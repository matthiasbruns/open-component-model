package provider_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	remotecredentials "github.com/oras-project/oras-go/v3/registry/remote/credentials"
	"github.com/stretchr/testify/require"
)

// TestReviewPR4221_TokenFlowPin proves what auth.NewCompositeTokenFetcher(..., true)
// buys: with the pin the realm sees only the distribution-spec GET; without it
// oras-go v3 tries the OAuth2 POST first. The second case is what every OCM
// production auth.Client would do if the TokenFetcher field were ever dropped.
func TestReviewPR4221_TokenFlowPin(t *testing.T) {
	for _, tc := range []struct {
		name        string
		pinned      bool
		wantMethods []string
	}{
		{name: "pinned (what the PR configures)", pinned: true, wantMethods: []string{http.MethodGet}},
		{name: "unpinned (v3 default)", pinned: false, wantMethods: []string{http.MethodPost, http.MethodGet}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var mu sync.Mutex
			var realmMethods []string
			realm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				realmMethods = append(realmMethods, req.Method)
				mu.Unlock()
				if req.Method == http.MethodPost {
					// Registries without an OAuth2 token endpoint answer like this
					// (GHCR 405, GCR/GitLab 404, ACR 400, Artifactory 401).
					w.WriteHeader(http.StatusMethodNotAllowed)
					_, _ = w.Write([]byte(`{"errors":[{"code":"UNSUPPORTED","message":"no oauth2"}]}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
			}))
			t.Cleanup(realm.Close)

			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Authorization") == "Bearer tok" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+realm.URL+`",service="reg"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(registry.Close)

			header := http.Header{"User-Agent": {"review-pr4221"}}

			c := &auth.Client{
				Client:               http.DefaultClient,
				Cache:                auth.NewCache(),
				Header:               header,
				BlockedRealmPrefixes: []netip.Prefix{},
				CredentialFunc: remotecredentials.StaticCredentialFunc(
					registryHost(registry.URL),
					remotecredentials.Credential{Username: "user", Password: "pass"},
				),
			}
			if tc.pinned {
				c.TokenFetcher = auth.NewCompositeTokenFetcher(http.DefaultClient, header, "", true)
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, registry.URL+"/v2/some/repo/manifests/latest", nil)
			r.NoError(err)
			resp, err := c.Do(req)
			r.NoError(err)
			r.NoError(resp.Body.Close())
			r.Equal(http.StatusOK, resp.StatusCode, "the request must end up authenticated either way")

			mu.Lock()
			defer mu.Unlock()
			r.Equal(tc.wantMethods, realmMethods)
		})
	}
}

func registryHost(url string) string {
	return url[len("http://"):]
}
