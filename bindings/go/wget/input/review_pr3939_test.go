package input_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/wget/input"
)

// TestReviewPR3939_F1_PartialReadMustNotBypassChecksumPolicy: under checksum mode Require, content whose
// advertised checksum does not match must never be accepted silently. A consumer that stops before the
// end of the body (a tar reader stops at the end-of-archive marker, before the record padding) must see
// the mismatch from ProcessResource, Read or Close.
func TestReviewPR3939_F1_PartialReadMustNotBypassChecksumPolicy(t *testing.T) {
	t.Parallel()

	// A tar padded to a full 10240-byte record, as GNU tar writes it.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	payload := bytes.Repeat([]byte("chart"), 200)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "chart.tgz", Mode: 0o644, Size: int64(len(payload)), Typeflag: tar.TypeReg}))
	_, err := tw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	archive := append(buf.Bytes(), make([]byte, 10240-buf.Len()%10240)...)

	tests := []struct {
		name    string
		consume func(r io.Reader) error
	}{
		{
			name: "consumer reads the first KiB only",
			consume: func(r io.Reader) error {
				_, err := io.ReadFull(r, make([]byte, 1024))
				return err
			},
		},
		{
			name: "tar reader stops at the end-of-archive marker",
			consume: func(r io.Reader) error {
				tr := tar.NewReader(r)
				for {
					if _, err := tr.Next(); errors.Is(err, io.EOF) {
						return nil
					} else if err != nil {
						return err
					}
					if _, err := io.Copy(io.Discard, tr); err != nil {
						return err
					}
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Checksum-Sha256", strings.Repeat("0", 64))
				_, _ = w.Write(archive)
			}))
			defer server.Close()

			method := &input.InputMethod{ChecksumConfig: requireMode()}
			result, err := method.ProcessResource(t.Context(), wgetInputResource(t, map[string]any{"url": server.URL + "/chart.tar"}), nil)
			if err != nil {
				r.ErrorContains(err, "checksum")
				return
			}
			rc, err := result.ProcessedBlobData.ReadCloser()
			r.NoError(err)
			consumeErr := tc.consume(rc)
			closeErr := rc.Close()
			r.Error(errors.Join(consumeErr, closeErr),
				"content with a mismatching advertised checksum was accepted: the consumer stopped before the end of the body, so the checksum policy never ran")
		})
	}
}
