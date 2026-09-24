package transformation

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetrepository "ocm.software/open-component-model/bindings/go/wget/repository"
	"ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

func transportReviewStep(sourceURL, targetURL string) *v1alpha1.HTTPStreaming {
	return &v1alpha1.HTTPStreaming{
		Type: v1alpha1.HTTPStreamingV1alpha1,
		ID:   "transport-review",
		Spec: &v1alpha1.HTTPStreamingSpec{
			Resource:       wgetResourceV2("blob", sourceURL, "", "", nil, nil),
			TargetResource: wgetResourceV2("blob", targetURL, http.MethodPut, "", nil, nil),
		},
	}
}

func TestHTTPStreamingTransportReview_RejectsRedirectToGET(t *testing.T) {
	r := require.New(t)
	payload := []byte("discarded upload, not a stored resource")
	type request struct {
		method string
		bytes  int64
		err    error
	}
	requests := make(chan request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		n, err := io.Copy(io.Discard, req.Body)
		requests <- request{req.Method, n, err}
		if req.URL.Path == "/upload" {
			http.Redirect(w, req, "/page", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "ordinary GET page; nothing was stored")
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tr := &HTTPStreamingTransformer{Scheme: newTransformerScheme(), ResourceRepository: &stubResourceRepository{payload: payload}}
	out, err := tr.Transform(ctx, transportReviewStep(server.URL+"/unused-source", server.URL+"/upload"))
	first := <-requests
	r.NoError(first.err)
	r.Equal(http.MethodPut, first.method)
	r.Equal(int64(len(payload)), first.bytes, "the redirect must follow a fully consumed/discarded PUT")
	select {
	case second := <-requests:
		r.NoError(second.err)
		r.Equal(http.MethodGet, second.method)
		r.Zero(second.bytes)
		t.Log("302 was followed as a bodyless GET returning 200")
	default:
		// Rejecting the redirect without following it is also correct.
	}
	r.Error(err, "a GET page after a discarded PUT must not count as an upload")
	r.Nil(out)
}

// The gate models a large source stalled after its first byte. Closing the reader
// interrupts the stall; no sleeps or unbounded allocation are needed.
type transportReviewSlowReader struct {
	ctx     context.Context
	blocked chan struct{}
	closed  chan struct{}
	done    chan struct{}
	once    sync.Once
	first   bool
}

func (s *transportReviewSlowReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if !s.first {
		s.first = true
		p[0] = 'x'
		return 1, nil
	}
	close(s.blocked)
	defer close(s.done)
	select {
	case <-s.closed:
		return 0, io.ErrClosedPipe
	case <-s.ctx.Done():
		return 0, s.ctx.Err()
	}
}

func (s *transportReviewSlowReader) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

type transportReviewSlowBlob struct {
	ctx     context.Context
	readers chan *transportReviewSlowReader
}

func (b *transportReviewSlowBlob) Size() int64 { return 64 << 20 }

func (b *transportReviewSlowBlob) ReadCloser() (io.ReadCloser, error) {
	rc := &transportReviewSlowReader{ctx: b.ctx, blocked: make(chan struct{}), closed: make(chan struct{}), done: make(chan struct{})}
	b.readers <- rc
	return rc, nil
}

type transportReviewBlobRepository struct {
	repository.ResourceRepository
	source blob.ReadOnlyBlob
}

func (r *transportReviewBlobRepository) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	return r.source, nil
}

func TestHTTPStreamingTransportReview_Early2xxRequiresCompletion(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	source := &transportReviewSlowBlob{ctx: ctx, readers: make(chan *transportReviewSlowReader, 1)}
	observed := make(chan *transportReviewSlowReader, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rc := <-source.readers
		observed <- rc
		prefix := make([]byte, 1)
		if _, err := io.ReadFull(req.Body, prefix); err != nil {
			return
		}
		select {
		case <-rc.blocked:
		case <-ctx.Done():
			return
		}
		// Prevent net/http's automatic request-body draining before sending 200.
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
	}))
	t.Cleanup(server.Close)
	tr := &HTTPStreamingTransformer{Scheme: newTransformerScheme(), ResourceRepository: &transportReviewBlobRepository{source: source}}
	out, err := tr.Transform(ctx, transportReviewStep(server.URL+"/unused-source", server.URL+"/upload"))
	r.NoError(ctx.Err(), "the early response must complete before the safety deadline")
	rc := <-observed
	defer rc.Close()
	select {
	case <-rc.done:
	case <-ctx.Done():
		t.Fatal("source read did not terminate after Transform returned")
	}
	if out != nil {
		result := out.(*v1alpha1.HTTPStreaming)
		r.NotNil(result.Output.Resource.Digest)
		prefixDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("x")))
		t.Logf("returned digest=%s; one-byte prefix digest=%s; advertised source size=%d", result.Output.Resource.Digest.Value, prefixDigest, source.Size())
	}
	r.Error(err, "an early 200 must not publish the digest of a one-byte prefix as a completed 64 MiB upload")
	r.Nil(out)
}

type transportReviewRetainingRepository struct {
	repository.ResourceRepository
	downloaded blob.ReadOnlyBlob
	tempFolder string
	paths      []string
}

func (r *transportReviewRetainingRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	b, err := r.ResourceRepository.DownloadResource(ctx, resource, credentials)
	if err != nil {
		return nil, err
	}
	r.downloaded = b
	entries, err := os.ReadDir(r.tempFolder)
	for _, entry := range entries {
		r.paths = append(r.paths, filepath.Join(r.tempFolder, entry.Name()))
	}
	return b, err
}

func TestHTTPStreamingTransportReview_RemovesDownloadedTempBlob(t *testing.T) {
	r := require.New(t)
	const payload = "real wget download backed by a temporary file"
	uploaded := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			_, _ = io.WriteString(w, payload)
			return
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		uploaded <- string(body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)
	tempFolder := t.TempDir()
	repo := &transportReviewRetainingRepository{
		ResourceRepository: wgetrepository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempFolder}, wgetrepository.WithHTTPClient(server.Client())),
		tempFolder:         tempFolder,
	}
	// Retain the owning blob so GC cleanup cannot hide a missing explicit Close.
	t.Cleanup(func() {
		if closer, ok := repo.downloaded.(io.Closer); ok {
			r.NoError(closer.Close())
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tr := &HTTPStreamingTransformer{Scheme: newTransformerScheme(), ResourceRepository: repo}
	_, err := tr.Transform(ctx, transportReviewStep(server.URL+"/source", server.URL+"/upload"))
	r.NoError(err)
	r.Equal(payload, <-uploaded)
	r.Len(repo.paths, 1, "verify the actual wget repository created a file")
	_, err = os.Stat(repo.paths[0])
	r.ErrorIs(err, os.ErrNotExist, "Transform must remove the owning blob's file, not merely close its reader")
}

func TestHTTPStreamingTransportReview_RedactsQuerySecretFromError(t *testing.T) {
	r := require.New(t)
	const secret = "synthetic-review-query-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		// A malformed response causes http.Client.Do to wrap the request URL.
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, "not an HTTP response\r\n\r\n")
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tr := &HTTPStreamingTransformer{Scheme: newTransformerScheme(), ResourceRepository: &stubResourceRepository{payload: []byte("upload")}}
	_, err := tr.Transform(ctx, transportReviewStep(server.URL+"/unused-source", server.URL+"/upload?token="+secret))
	r.Error(err)
	r.Contains(err.Error(), "failed uploading")
	r.False(strings.Contains(err.Error(), secret), "upload errors must redact synthetic query credentials: %v", err)
}
