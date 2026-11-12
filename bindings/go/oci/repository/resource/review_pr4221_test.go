package resource_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	remotecredentials "github.com/oras-project/oras-go/v3/registry/remote/credentials"
	"github.com/stretchr/testify/require"
)

// TestReviewPR4221_F1b_StaticCredentialFuncIsCaseSensitive mirrors the call in
// resource_repository.go createRepository: StaticCredentialFunc(url.Host, ...).
func TestReviewPR4221_F1b_StaticCredentialFuncIsCaseSensitive(t *testing.T) {
	for _, host := range []string{"registry.example.com", "REGISTRY.EXAMPLE.COM", "Registry.Example.Com"} {
		t.Run(host, func(t *testing.T) {
			r := require.New(t)
			var sawCredential bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if _, _, ok := req.BasicAuth(); ok {
					sawCredential = true
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(srv.Close)

			client := &auth.Client{
				Client:         srv.Client(),
				Cache:          auth.NewCache(),
				CredentialFunc: remotecredentials.StaticCredentialFunc(host, remotecredentials.Credential{Username: "u", Password: "p"}),
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/v2/some/repo/manifests/latest", nil)
			r.NoError(err)
			req.Host = host
			resp, err := client.Do(req)
			r.NoError(err)
			r.NoError(resp.Body.Close())
			r.True(sawCredential, "request went out unauthenticated")
		})
	}
}
