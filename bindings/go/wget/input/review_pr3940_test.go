package input_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/wget/input"
)

func reviewPR3940Gzip(t *testing.T, content []byte) []byte {
	t.Helper()
	r := require.New(t)
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	_, err := writer.Write(content)
	r.NoError(err)
	// A flushed block permits the last content byte to arrive before gzip EOF.
	r.NoError(writer.Flush())
	r.NoError(writer.Close())
	return encoded.Bytes()
}

func TestReviewPR3940_F1_KnownSizeCopyVerifiesContentDigest(t *testing.T) {
	for _, consume := range []string{"read to EOF", "blob.Copy"} {
		t.Run(consume, func(t *testing.T) {
			r := require.New(t)
			content := bytes.Repeat([]byte("artifact content\n"), 100)
			encoded := reviewPR3940Gzip(t, content)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Range") != "" {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", len(content)))
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(content[:1])
					return
				}
				w.Header().Set("Content-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(make([]byte, 32))+":")
				if req.Header.Get("Accept-Encoding") == "gzip" {
					w.Header().Set("Content-Encoding", "gzip")
					_, _ = w.Write(encoded)
					return
				}
				_, _ = w.Write(content)
			}))
			t.Cleanup(server.Close)
			method := &input.InputMethod{ChecksumConfig: requireMode()}
			result, err := method.ProcessResource(t.Context(), wgetInputResource(t, map[string]any{"url": server.URL}), nil)
			if err != nil {
				r.ErrorContains(err, "checksum mismatch")
				return
			}
			r.Equal(int64(len(content)), result.ProcessedBlobData.(blob.SizeAware).Size())
			if consume == "blob.Copy" {
				err = blob.Copy(io.Discard, result.ProcessedBlobData)
			} else {
				reader, openErr := result.ProcessedBlobData.ReadCloser()
				r.NoError(openErr)
				_, err = io.Copy(io.Discard, reader)
				_ = reader.Close()
			}
			r.ErrorContains(err, "checksum mismatch", "the advertised digest must be verified by every supported consumer")
		})
	}
}

func TestReviewPR3940_F2_FailedProbePreservesStoredArchive(t *testing.T) {
	r := require.New(t)
	archive := reviewPR3940Gzip(t, []byte("stored archive payload"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)
	// Establish the reference bytes without allowing Go's transparent decoding.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	r.NoError(err)
	req.Header.Set("Accept-Encoding", "identity")
	response, err := server.Client().Do(req)
	r.NoError(err)
	identity, err := io.ReadAll(response.Body)
	r.NoError(err)
	r.NoError(response.Body.Close())
	r.Equal(archive, identity)
	result, err := (&input.InputMethod{}).ProcessResource(t.Context(), wgetInputResource(t, map[string]any{"url": server.URL}), nil)
	r.NoError(err)
	reader, err := result.ProcessedBlobData.ReadCloser()
	r.NoError(err)
	actual, err := io.ReadAll(reader)
	r.NoError(err)
	r.NoError(reader.Close())
	r.Equal(identity, actual, "an unavailable range probe must not silently unpack stored archives")
}
