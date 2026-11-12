package credentials_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	"github.com/stretchr/testify/require"

	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

// TestReviewPR4221_F1_CredentialFuncIsCaseSensitive asserts that the credential
// function built from an OCM consumer identity still matches when the identity's
// hostname is not lower-case. Registry host names are case-insensitive and
// runtime.ParseURLToIdentity preserves the case the user wrote in BaseUrl.
//
// Place in bindings/go/oci/credentials/. Fails on PR head 462312f42b, passes on
// its parent c1f262431a (with the v2 spelling: auth.Credential / Credential:).
func TestReviewPR4221_F1_CredentialFuncIsCaseSensitive(t *testing.T) {
	for _, tc := range []struct {
		name string
		host string
	}{
		{"lower-case", "registry.example.com"},
		{"upper-case", "REGISTRY.EXAMPLE.COM"},
		{"mixed-case", "Registry.Example.Com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var gotUser, gotPass string
			var gotOK bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if u, p, ok := req.BasicAuth(); ok {
					gotUser, gotPass, gotOK = u, p, true
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(srv.Close)

			client := &auth.Client{
				Client: srv.Client(),
				Cache:  auth.NewCache(),
				CredentialFunc: ocicredentials.CredentialFunc(
					&identityv1.OCIRegistryIdentity{Hostname: tc.host},
					&credentialsv1.OCICredentials{Username: "user", Password: "pass"},
				),
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/v2/some/repo/manifests/latest", nil)
			r.NoError(err)
			// The credential lookup keys off the request's Host, not the dialled address.
			req.Host = tc.host

			resp, err := client.Do(req)
			r.NoError(err)
			r.NoError(resp.Body.Close())

			r.True(gotOK, "server never saw a basic credential: the request went out unauthenticated")
			r.Equal("user", gotUser)
			r.Equal("pass", gotPass)
			r.Equal(http.StatusOK, resp.StatusCode)
		})
	}
}
