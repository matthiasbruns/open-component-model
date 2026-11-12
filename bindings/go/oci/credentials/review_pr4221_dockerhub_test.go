package credentials

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	"github.com/stretchr/testify/require"

	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

// TestReviewPR4221_DockerHubIdentity records which Authorization header reaches
// a registry dialled at registry-1.docker.io, for each spelling an OCM user can
// put in Repository.BaseUrl.
//
// v3 derives the credential key from the canonical registry name
// (registry-1.docker.io -> docker.io, see registry/remote/auth/resource.go
// canonicalRegistry), while v2 passed the request host as written. CredentialFunc
// compares identity.Hostname against resource.Registry, so the two spellings swap
// places across the migration:
//
//	BaseUrl                 v2 (c1f262431a)     v3 (27c88f861c)
//	docker.io               credential dropped  credential sent
//	registry-1.docker.io    credential sent     credential dropped
//
// The spelling that authenticates today is therefore the one that stops working.
// Run the v2 side with .review/pr-4221/dockerhub-baseline.sh.
func TestReviewPR4221_DockerHubIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		// baseURLHost is what OCIRegistryIdentity.Hostname holds, taken
		// verbatim from Repository.BaseUrl by OCIRegistryIdentityFromOCIRepository.
		baseURLHost  string
		wantAuthSent bool
	}{
		{name: "BaseUrl docker.io", baseURLHost: "docker.io", wantAuthSent: true},
		{name: "BaseUrl registry-1.docker.io", baseURLHost: "registry-1.docker.io", wantAuthSent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if a := req.Header.Get("Authorization"); a != "" {
					gotAuth = a
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer srv.Close()

			client := &auth.Client{
				Client: srv.Client(),
				Cache:  auth.NewCache(),
				CredentialFunc: CredentialFunc(
					&identityv1.OCIRegistryIdentity{Hostname: tc.baseURLHost},
					&credentialsv1.OCICredentials{Username: "u", Password: "p"},
				),
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/v2/", nil)
			r.NoError(err)
			// Dial the test server but present the Docker Hub endpoint as the
			// request host, exactly as remote.Repository does for docker.io.
			req.Host = "registry-1.docker.io"

			resp, err := client.Do(req)
			r.NoError(err)
			defer func() { _ = resp.Body.Close() }()

			if tc.wantAuthSent {
				r.Equal(http.StatusOK, resp.StatusCode, "expected the credential to be sent")
				r.NotEmpty(gotAuth)
			} else {
				r.Equal(http.StatusUnauthorized, resp.StatusCode)
				r.Empty(gotAuth)
			}
		})
	}
}
