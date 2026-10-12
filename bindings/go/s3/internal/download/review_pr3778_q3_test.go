package download

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReviewPR3778_Q3_PartChecksumNeedsCompositeType characterises the part check: part
// 2 arrives with bytes that do not match the part checksum it carries. With
// x-amz-checksum-type: COMPOSITE the download fails. Without the type header, which is
// what the PR's [INFERENCE] note covers, the corrupt part is written unchecked: the SDK
// skips 206 responses, and spanChecksum skips partial spans that are not COMPOSITE. The
// test passes when that is the behaviour.
func TestReviewPR3778_Q3_PartChecksumNeedsCompositeType(t *testing.T) {
	parts := threeParts()
	corrupt := []byte("SECOND PART ")
	require.Len(t, corrupt, len(parts[1]))

	tests := []struct {
		name         string
		checksumType string
		wantErr      bool
	}{
		{name: "COMPOSITE type: corrupt part is rejected", checksumType: "COMPOSITE", wantErr: true},
		{name: "no type header: corrupt part is accepted", checksumType: "", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			total := len(bytes.Join(parts, nil))
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header()["Content-Type"] = nil
				w.Header().Set("ETag", fmt.Sprintf(`"0123456789abcdef-%d"`, len(parts)))
				n, _ := strconv.Atoi(req.URL.Query().Get("partNumber"))
				if n < 1 || n > len(parts) {
					http.Error(w, "only part GETs are expected", http.StatusBadRequest)
					return
				}
				start := len(bytes.Join(parts[:n-1], nil))
				body := parts[n-1]
				w.Header().Set("x-amz-mp-parts-count", strconv.Itoa(len(parts)))
				w.Header().Set("x-amz-checksum-crc32", checksumCRC32(body))
				if tt.checksumType != "" {
					w.Header().Set("x-amz-checksum-type", tt.checksumType)
				}
				if n == 2 {
					body = corrupt
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+len(body)-1, total))
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(body)
			}))
			t.Cleanup(s.Close)

			withoutAWSEnvironment(t)
			out, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k", Endpoint: s.URL, UsePathStyle: true},
				anonymous(), WithTempDir(t.TempDir()))
			if tt.wantErr {
				r.ErrorContains(err, "checksum")
				return
			}
			r.NoError(err)
			r.Contains(string(readBlob(t, out.Blob)), string(corrupt), "the corrupt part was written unchecked")
		})
	}
}
