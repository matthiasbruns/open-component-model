package transformation

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go"
	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetrepository "ocm.software/open-component-model/bindings/go/wget/repository"
	"ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

func TestHTTPStreamingReview_OutputReadback(t *testing.T) {
	r := require.New(t)
	payload := []byte("original resource bytes")
	r.NotEmpty(payload)

	for _, verb := range []string{"", http.MethodPut} {
		name := "default_upload_verb"
		if verb != "" {
			name = "explicit_PUT"
		}
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			var mu sync.Mutex
			var stored []byte
			var methods []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				methods = append(methods, req.Method)
				switch req.Method {
				case http.MethodPut:
					body, err := io.ReadAll(req.Body)
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					stored = body
					w.WriteHeader(http.StatusOK)
				case http.MethodGet:
					_, _ = w.Write(stored)
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			t.Cleanup(server.Close)

			tr := &HTTPStreamingTransformer{
				Scheme:             newTransformerScheme(),
				ResourceRepository: &stubResourceRepository{payload: payload},
			}
			out, err := tr.Transform(t.Context(), &v1alpha1.HTTPStreaming{
				Type: v1alpha1.HTTPStreamingV1alpha1,
				Spec: &v1alpha1.HTTPStreamingSpec{
					Resource:       wgetResourceV2("blob", "https://source.example/blob", "", "", nil, nil),
					TargetResource: wgetResourceV2("blob", server.URL, verb, "", nil, nil),
				},
			})
			r.NoError(err)
			mu.Lock()
			uploaded := append([]byte(nil), stored...)
			mu.Unlock()
			r.Equal(payload, uploaded)

			result := out.(*v1alpha1.HTTPStreaming)
			tempFolder := t.TempDir()
			repo := wgetrepository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})
			b, err := repo.DownloadResource(t.Context(), descriptor.ConvertFromV2Resource(result.Output.Resource), nil)
			r.NoError(err)
			data := reviewReadBlob(t, b)

			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, []string{http.MethodPut, http.MethodGet}, methods, "reading transformed access must not repeat the upload")
			assert.Equal(t, payload, data, "transformed access must retrieve the original bytes")
			assert.Equal(t, payload, stored, "read-back must not overwrite the uploaded object")
		})
	}
}

func TestHTTPStreamingReview_OCIManifestDigest(t *testing.T) {
	r := require.New(t)
	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	configDigest := godigest.FromBytes(config)
	manifest, err := json.Marshal(ociv1.Manifest{
		Versioned: ocispec.Versioned{SchemaVersion: 2},
		MediaType: ociv1.MediaTypeImageManifest,
		Config:    ociv1.Descriptor{MediaType: ociv1.MediaTypeImageConfig, Digest: configDigest, Size: int64(len(config))},
		Layers:    []ociv1.Descriptor{},
	})
	r.NoError(err)
	manifestDigest := godigest.FromBytes(manifest)

	// This read-only registry fixture implements only the endpoints needed for this
	// single image. Digest processing and archive materialization use the real OCI repository.
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body []byte
		switch req.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
			return
		case "/v2/review/image/manifests/latest", "/v2/review/image/manifests/" + manifestDigest.String():
			body = manifest
			w.Header().Set("Content-Type", ociv1.MediaTypeImageManifest)
		case "/v2/review/image/blobs/" + configDigest.String():
			body = config
			w.Header().Set("Content-Type", ociv1.MediaTypeImageConfig)
		default:
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Docker-Content-Digest", godigest.FromBytes(body).String())
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if req.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(registry.Close)

	tempFolder := t.TempDir()
	repo := ocirepository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder})
	source, err := repo.ProcessResourceDigest(t.Context(), &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "image", Version: "1.0.0"}},
		Type:        "ociArtifact",
		Access: &ociaccessv1.OCIImage{
			Type:           runtime.NewVersionedType(ociaccessv1.OCIImageType, ociaccessv1.Version),
			ImageReference: registry.URL + "/review/image:latest",
		},
	}, nil)
	r.NoError(err)
	r.NotNil(source.Digest)
	r.Equal(manifestDigest.Encoded(), source.Digest.Value, "OCI resource digest identifies the manifest")
	b, err := repo.DownloadResource(t.Context(), source, nil)
	r.NoError(err)
	archive := reviewReadBlob(t, b)
	r.NotEmpty(archive)
	r.NotEqual(source.Digest.Value, godigest.FromBytes(archive).Encoded(), "materialized archive has different bytes than the manifest")

	var uploaded []byte
	var mu sync.Mutex
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		uploaded = body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	scheme := newTransformerScheme()
	scheme.MustRegisterScheme(ociaccess.Scheme)
	sourceV2, err := descriptor.ConvertToV2Resource(scheme, source)
	r.NoError(err)
	tr := &HTTPStreamingTransformer{Scheme: scheme, ResourceRepository: repo}
	out, err := tr.Transform(t.Context(), &v1alpha1.HTTPStreaming{
		Type: v1alpha1.HTTPStreamingV1alpha1,
		Spec: &v1alpha1.HTTPStreamingSpec{
			Resource:       sourceV2,
			TargetResource: wgetResourceV2("image", target.URL, "", "", nil, nil),
		},
	})
	r.NoError(err, "a valid OCI manifest digest must not be compared to the materialized archive's byte digest")
	result := out.(*v1alpha1.HTTPStreaming)
	r.NotNil(result.Output.Resource.Digest)
	mu.Lock()
	defer mu.Unlock()
	r.NotEmpty(uploaded)
	r.Equal(godigest.FromBytes(uploaded).Encoded(), result.Output.Resource.Digest.Value, "the wget target digest must describe the uploaded archive")
}

func reviewReadBlob(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	r := require.New(t)
	if closer, ok := b.(io.Closer); ok {
		defer func() { r.NoError(closer.Close()) }()
	}
	rc, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	data, err := io.ReadAll(rc)
	r.NoError(err)
	return data
}
