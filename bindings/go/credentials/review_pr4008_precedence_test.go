package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const twoGlobConsumersYAML = `
type: credentials.config.ocm.software
consumers:
  - identity:
      type: OCIRegistry
      hostname: "*.example.com"
    credentials:
      - type: Credentials/v1
        properties:
          username: host-wide
  - identity:
      type: OCIRegistry
      hostname: "*.example.com"
      path: "team-a/*"
    credentials:
      - type: Credentials/v1
        properties:
          username: team-a
`

// PR body claims "Exact path beats glob path ... beats absent path" and that
// matchAnyNode scores all matches. With two glob-hostname consumers, the
// winner is whichever vertex map iteration hits first.
func TestReviewGlobHostnamePathPrecedenceIsDeterministic(t *testing.T) {
	seen := map[string]int{}
	for range 200 {
		graph, err := GetGraph(t, twoGlobConsumersYAML)
		require.NoError(t, err)
		typed, err := graph.Resolve(t.Context(), runtime.Identity{"type": "OCIRegistry", "hostname": "repo.example.com", "path": "team-a/app"})
		require.NoError(t, err)
		seen[typed.(*v1.DirectCredentials).Properties["username"]]++
	}
	t.Logf("winners over 200 fresh graphs: %v", seen)
	require.Len(t, seen, 1, "credential selection is non-deterministic")
}
