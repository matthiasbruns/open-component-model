package transformation

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/wget/repository"
	"ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// TestReviewPR3939_F2_StreamingTransferWithSourceHostTimeout runs the production wget -> HTTP PUT
// transfer (HTTPStreamingTransformer + wget ResourceRepository) with one shared http.config. The source
// sends its body at once; the target reads the upload slowly (~0.8s for 64 MiB, larger than the socket buffers, so the upload applies backpressure). A timeout that the source download
// alone easily meets must not fail the transfer because the target is slow.
func TestReviewPR3939_F2_StreamingTransferWithSourceHostTimeout(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("x"), 64<<20)

	tests := []struct {
		name   string
		config func(sourceHost string) *httpv1alpha1.Config
	}{
		{
			// Control: a single global timeout also bounds the upload, so it must cover the slow upload anyway.
			name: "global timeout that covers the upload",
			config: func(string) *httpv1alpha1.Config {
				return &httpv1alpha1.Config{TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: httpv1alpha1.NewTimeout(3 * time.Second)}}
			},
		},
		{
			name: "per-host timeout on the source host only",
			config: func(sourceHost string) *httpv1alpha1.Config {
				return &httpv1alpha1.Config{Hosts: map[string]*httpv1alpha1.HostConfig{
					sourceHost: {TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: httpv1alpha1.NewTimeout(300 * time.Millisecond)}},
				}}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(payload)
			}))
			defer source.Close()
			var received int
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				chunk := make([]byte, 4<<20)
				for {
					n, err := io.ReadFull(req.Body, chunk)
					received += n
					if err != nil {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				w.WriteHeader(http.StatusCreated)
			}))
			defer target.Close()

			sourceURL, err := url.Parse(source.URL)
			r.NoError(err)
			cfg := tc.config(sourceURL.Host)

			tr := &HTTPStreamingTransformer{
				Scheme:             newTransformerScheme(),
				ResourceRepository: repository.NewResourceRepository(repository.WithHTTPClient(ocmhttp.New(ocmhttp.WithConfig(cfg)))),
				HTTPConfig:         cfg,
			}
			step := &v1alpha1.HTTPStreaming{
				Type: v1alpha1.HTTPStreamingV1alpha1,
				ID:   "upload",
				Spec: &v1alpha1.HTTPStreamingSpec{
					Resource:       wgetResourceV2("blob", source.URL+"/blob.bin", "", "application/octet-stream", nil, nil),
					Request:        wgetRequest(target.URL+"/blob.bin", http.MethodPut, "application/octet-stream", nil),
					TargetResource: wgetResourceV2("blob", target.URL+"/blob.bin", "", "application/octet-stream", nil, nil),
				},
			}
			start := time.Now()
			_, err = tr.Transform(t.Context(), step)
			t.Logf("transfer took %s, target received %d of %d bytes", time.Since(start).Round(time.Millisecond), received, len(payload))
			r.NoError(err, "the transfer failed although the source download alone takes milliseconds")
			r.Equal(len(payload), received)
		})
	}
}
