package download_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/wget/internal/download"
)

func TestReviewPR3939_ChecksumBytesMustNotBeTransparentlyDecoded(t *testing.T) {
	r := require.New(t)
	var encoded bytes.Buffer
	zw := gzip.NewWriter(&encoded)
	_, err := zw.Write([]byte("artifact content"))
	r.NoError(err)
	r.NoError(zw.Close())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(encoded.Bytes())
	}))
	t.Cleanup(srv.Close)
	b, err := download.Download(t.Context(), download.Request{URL: srv.URL},
		download.WithClient(srv.Client()),
		download.WithDigestAlgorithms(download.DigestAlgorithm{Name: "SHA-256", New: sha256.New}))
	r.NoError(err)
	t.Cleanup(func() { _ = b.Close() })
	rc, err := b.ReadCloser()
	r.NoError(err)
	got, readErr := io.ReadAll(rc)
	r.NoError(readErr)
	r.NoError(rc.Close())
	r.Equal(encoded.Bytes(), got, "HTTP content digests cover encoded bytes, including with an injected standard client")
}
