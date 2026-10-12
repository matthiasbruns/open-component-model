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

// TestReviewPR3778_F1_PartWithoutContentRangeIsNotTheObject covers a store that honours
// partNumber=1 but leaves out Content-Range, while it still reports that the object has
// three parts. Download must not return part 1 as if it were the whole object: it should
// either fetch the rest or fail.
func TestReviewPR3778_F1_PartWithoutContentRangeIsNotTheObject(t *testing.T) {
	parts := threeParts()
	content := bytes.Join(parts, nil)

	tests := []struct {
		name   string
		status int
	}{
		{name: "206 without Content-Range", status: http.StatusPartialContent},
		{name: "200 with only part 1", status: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header()["Content-Type"] = nil
				w.Header().Set("ETag", fmt.Sprintf(`"0123456789abcdef-%d"`, len(parts)))
				if req.URL.Query().Get("partNumber") == "1" {
					w.Header().Set("x-amz-mp-parts-count", strconv.Itoa(len(parts)))
					w.Header().Set("Content-Length", strconv.Itoa(len(parts[0])))
					w.WriteHeader(tt.status)
					_, _ = w.Write(parts[0])
					return
				}
				w.Header().Set("Content-Length", strconv.Itoa(len(content)))
				_, _ = w.Write(content)
			}))
			t.Cleanup(s.Close)

			withoutAWSEnvironment(t)
			out, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k", Endpoint: s.URL, UsePathStyle: true},
				anonymous(), WithTempDir(t.TempDir()))
			if err != nil {
				return
			}
			r.Equal(string(content), string(readBlob(t, out.Blob)), "download returned a truncated object")
		})
	}
}
