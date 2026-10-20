package repository_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/wget/repository"
)

func TestReviewPR3939_ValidHTTPChecksumWithStandardClient(t *testing.T) {
	for _, tt := range []struct {
		name     string
		custom   bool
		identity bool
	}{
		{name: "default client"},
		{name: "standard client", custom: true},
		{name: "standard client with explicit identity", custom: true, identity: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			plain := []byte("artifact content")
			var compressed bytes.Buffer
			zw := gzip.NewWriter(&compressed)
			_, err := zw.Write(plain)
			r.NoError(err)
			r.NoError(zw.Close())
			seen := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				encoding := req.Header.Get("Accept-Encoding")
				seen <- encoding
				payload := plain
				if strings.Contains(encoding, "gzip") {
					payload = compressed.Bytes()
					w.Header().Set("Content-Encoding", "gzip")
				}
				sum := sha256.Sum256(payload)
				w.Header().Set("X-Checksum-Sha256", hex.EncodeToString(sum[:]))
				_, _ = w.Write(payload)
			}))
			t.Cleanup(srv.Close)
			opts := []repository.Option{
				repository.WithChecksumConfig(&checksumhttpv1alpha1.Config{
					Mode: checksumhttpv1alpha1.ChecksumModeRequire,
				}),
			}
			if tt.custom {
				opts = append(opts, repository.WithHTTPClient(srv.Client()))
			}
			repo := repository.NewResourceRepository(opts...)
			spec := map[string]any{}
			if tt.identity {
				spec["header"] = map[string][]string{"Accept-Encoding": {"identity"}}
			}
			b, err := repo.DownloadResource(t.Context(), wgetResource(t, srv.URL, spec), nil)
			r.NoError(err)
			if closer, ok := b.(io.Closer); ok {
				t.Cleanup(func() { _ = closer.Close() })
			}
			rc, err := b.ReadCloser()
			r.NoError(err)
			got, readErr := io.ReadAll(rc)
			closeErr := rc.Close()
			t.Logf("Accept-Encoding=%q bytes=%d read=%v close=%v", <-seen, len(got), readErr, closeErr)
			r.NoError(readErr, "a correctly advertised checksum must verify")
			r.NoError(closeErr)
		})
	}
}
