package repositoryupload

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/uploadtest"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

type reviewRedirectBackend struct{ store *reviewRedirectStore }

func (b reviewRedirectBackend) Name() string { return "review" }
func (b reviewRedirectBackend) CredentialURLs(spec *uploadv1alpha1.RepositoryUploadSpec) (string, string, error) {
	return "", spec.URL, nil
}
func (b reviewRedirectBackend) Store(context.Context, *Client, *uploadv1alpha1.RepositoryUploadSpec, *descriptor.Resource, time.Duration) (Store, error) {
	return b.store, nil
}

type reviewRedirectStore struct {
	url      string
	calls    []string
	digest   string
	complete bool
	sendErr  error
}

func (s *reviewRedirectStore) Chart() bool { return false }
func (s *reviewRedirectStore) URL() string { return s.url }
func (s *reviewRedirectStore) Stored(context.Context, *Client, string) (bool, error) {
	s.calls = append(s.calls, "Stored")
	return false, nil
}
func (s *reviewRedirectStore) Put(ctx context.Context, c *Client, content blob.ReadOnlyBlob, mediaType, _ string) (string, error) {
	s.calls = append(s.calls, "Put")
	s.digest, s.complete, s.sendErr = UploadBlob(ctx, c, content, s.url, http.Header{"Content-Type": {mediaType}}, nil)
	if s.sendErr != nil {
		return "", s.sendErr
	}
	if !s.complete {
		return "", fmt.Errorf("upload did not consume the complete body")
	}
	return s.digest, nil
}
func (s *reviewRedirectStore) Discard(context.Context, *Client, string) error {
	s.calls = append(s.calls, "Discard")
	return nil
}
func (s *reviewRedirectStore) Publish(context.Context, *Client, string, string) (runtime.Typed, error) {
	s.calls = append(s.calls, "Publish")
	return &wgetaccessv1.Wget{Type: runtime.NewVersionedType("Wget", "v1"), URL: s.url}, nil
}

func TestReviewUploadRejectsLoginRedirect(t *testing.T) {
	r := require.New(t)
	content := []byte("review redirect payload")
	expectedDigest := fmt.Sprintf("%x", sha256.Sum256(content))
	var mu sync.Mutex
	var requests []string
	var consumed []byte
	var readErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		mu.Lock()
		requests = append(requests, fmt.Sprintf("%s %s body=%q", req.Method, req.URL.Path, body))
		if req.Method == http.MethodPut && req.URL.Path == "/upload" {
			consumed, readErr = body, err
		}
		mu.Unlock()
		switch {
		case req.Method == http.MethodPut && req.URL.Path == "/upload":
			// Consume the entire body before redirecting, so EOF/digest checks cannot detect failure to store it.
			http.Redirect(w, req, "/login", http.StatusFound)
		case req.Method == http.MethodGet && req.URL.Path == "/login":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "<html>Login required</html>")
		default:
			http.NotFound(w, req)
		}
	}))
	defer srv.Close()

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(uploadv1alpha1.Scheme)
	scheme.MustRegisterScheme(wgetaccess.Scheme)
	u := &Uploader{Scheme: scheme, ResourceRepository: &uploadtest.ResourceRepo{Content: content, MediaType: "application/octet-stream"}}
	st := &reviewRedirectStore{url: srv.URL + "/upload"}
	spec := &uploadv1alpha1.RepositoryUploadSpec{
		Resource: &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "payload", Version: "1.0.0"},
			Type:        "blob",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://example.com/content"}`)},
			Digest:      &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: expectedDigest},
		},
		ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/review", Version: "1.0.0"},
		URL:              srv.URL,
		Repository:       "review",
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	out, err := u.Upload(ctx, spec, reviewRedirectBackend{store: st})
	mu.Lock()
	observedRequests := append([]string(nil), requests...)
	observedBody, observedReadErr := append([]byte(nil), consumed...), readErr
	mu.Unlock()
	t.Logf("HTTP calls: %v", observedRequests)
	t.Logf("store calls: %v", st.calls)
	t.Logf("UploadBlob / Client.Send: err=%v complete=%t digest=%s expected=%s", st.sendErr, st.complete, st.digest, expectedDigest)
	t.Logf("Uploader.Upload: err=%v output_present=%t", err, out != nil)
	if out != nil {
		t.Logf("published digest: %+v", out.Resource.Digest)
	}
	r.NoError(observedReadErr)
	r.Equal(content, observedBody)
	r.Error(err, "a PUT redirected to a 200 login page must be rejected, not published as an uploaded resource")
	r.Nil(out)
	r.NotContains(st.calls, "Publish")
}
