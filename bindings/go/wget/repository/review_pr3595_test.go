package repository_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/wget/repository"
)

// PR 3595 F9: ADR Flow B says the consumption path "rejects any mismatch before returning the blob,
// so a changed upstream response cannot reach the importer". The wget repository returns the blob
// without an error; any verification only happens while the caller reads the stream.
func TestReviewPR3595_F9_WgetMismatchRejectedBeforeBlobIsReturned(t *testing.T) {
	r := require.New(t)

	packaged := []byte("disk bytes at packaging time")
	changed := []byte("disk bytes served later")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(changed)
	}))
	defer server.Close()

	res := wgetResource(t, server.URL, map[string]any{"mediaType": "application/vnd.ocm.software.vm.disk.qcow2.v1"})
	res.Digest = &descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  godigest.FromBytes(packaged).Encoded(),
	}

	repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
	b, err := repo.DownloadResource(t.Context(), res, nil)
	if err == nil {
		rc, openErr := b.ReadCloser()
		r.NoError(openErr)
		got, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		t.Logf("blob returned; read %d bytes of the changed content, read error: %v, close error: %v", len(got), readErr, closeErr)
	}
	r.Error(err, "a digest mismatch must be rejected before the blob is handed to the consumer")
}
