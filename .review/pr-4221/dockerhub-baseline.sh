#!/usr/bin/env bash
# Counter-proof for TestReviewPR4221_DockerHubIdentity: runs the same scenario
# against oras-go v2 on the PR's merge base, where the result is inverted.
#
# Expected:
#   base c1f262431a   BaseUrl docker.io            FAIL (basic credential not found)
#                     BaseUrl registry-1.docker.io PASS
#   head 27c88f861c   BaseUrl docker.io            PASS
#                     BaseUrl registry-1.docker.io FAIL (basic credential not found)
#
# Usage: .review/pr-4221/dockerhub-baseline.sh [base-commit]
set -euo pipefail

BASE="${1:-c1f262431a}"
CHECKOUT="$(git rev-parse --path-format=absolute --git-common-dir)/.."
CHECKOUT="$(cd "$CHECKOUT" && pwd)"
WT="$CHECKOUT/.claude/worktrees/pr4221-base"

if [ ! -d "$WT" ]; then
  git -C "$CHECKOUT" worktree add --detach "$WT" "$BASE"
fi

PROBE="$WT/bindings/go/oci/credentials/review_pr4221_dockerhub_test.go"
cat > "$PROBE" <<'EOF'
package credentials

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/auth"

	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

// v2 counter-proof. Same scenario as the head-side test; v2's auth.Client keys
// credentials on the request host as written, so registry-1.docker.io matches
// and docker.io does not.
func TestReviewPR4221_DockerHubIdentity(t *testing.T) {
	for _, tc := range []struct {
		name         string
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
				Credential: CredentialFunc(
					&identityv1.OCIRegistryIdentity{Hostname: tc.baseURLHost},
					&credentialsv1.OCICredentials{Username: "u", Password: "p"},
				),
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/v2/", nil)
			r.NoError(err)
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
EOF

echo "### v2 baseline ($BASE)"
go -C "$WT/bindings/go" test ./oci/credentials/ -run TestReviewPR4221_DockerHubIdentity -count=1 -v || true
rm -f "$PROBE"
