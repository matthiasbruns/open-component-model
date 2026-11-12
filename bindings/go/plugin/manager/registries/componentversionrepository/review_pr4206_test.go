package componentversionrepository

import (
	"context"
	"encoding/json/jsontext"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestReviewPR4206_F5_MarshalFailureDoesNotCorruptTheResponse pins what a plugin endpoint sends
// when marshaling its response fails part way through.
//
// encoding/json v1 marshaled into an internal buffer, so a failure wrote nothing and the
// handler's error branch produced a clean status plus an error-only body. encoding/json/v2
// streams: json.MarshalWrite flushes to the http.ResponseWriter as it goes, so by the time the
// error is returned the first bytes of the JSON object are already on the wire - and the first
// write has implicitly committed HTTP 200.
//
// Every handler in this package kept its v1-era error branch after the write
// (handlers.go:50, 80, 149, 196, 215, 257, 299 and the sibling registries), so the error text
// is appended to a half-written object. The client decodes that with
// json.UnmarshalRead (plugins/call.go) and reports a JSON syntax error, so the plugin's real
// failure is lost.
//
// Fix direction: marshal into a buffer and issue a single Write wherever an error branch writes
// to the same writer, or stop writing a second payload once the stream has failed.
func TestReviewPR4206_F5_MarshalFailureDoesNotCorruptTheResponse(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)

	handler := GetComponentVersionHandlerFunc(
		func(_ context.Context, _ repov1.GetComponentVersionRequest[*dummyv1.Repository], _ runtime.Typed) (*descriptor.Descriptor, error) {
			return &descriptor.Descriptor{
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					Provider: descriptor.Provider{Name: "ocm.software"},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "ocm.software/review",
							Version: "1.0.0",
							// An empty, non-nil label value fails to marshal once the
							// enclosing object has already been written out.
							Labels: []descriptor.Label{{
								Name:  "broken",
								Value: jsontext.Value{},
							}},
						},
					},
				},
			}, nil
		}, scheme, &dummyv1.Repository{})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	r.NoError(err)
	target.RawQuery = url.Values{"name": {"ocm.software/review"}, "version": {"1.0.0"}}.Encode()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target.String(), nil)
	r.NoError(err)
	request.Header.Set("Authorization", `{"type":"DummyRepository/v1"}`)

	resp, err := http.DefaultClient.Do(request)
	r.NoError(err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	r.NoError(err)

	// Whatever the handler decides to do, a failed response must not be a 200 carrying a
	// truncated object with an error message concatenated onto it.
	r.NotEqual(http.StatusOK, resp.StatusCode,
		"marshal failed but the response committed 200; body=%q", string(body))
	r.NotContains(string(body), `"schemaVersion"`,
		"the half-marshaled descriptor was already flushed to the client before the error branch ran, so the error text is appended to a truncated object; body=%q", string(body))
}
