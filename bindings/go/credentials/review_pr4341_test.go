package credentials_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// reviewPR4341Runs is the number of freshly built graphs each case resolves against,
// so a result that depends on map iteration order shows up as a split.
const reviewPR4341Runs = 200

// reviewPR4341Resolve builds a new graph from config for every run and returns how often
// each username was resolved for the lookup identity.
func reviewPR4341Resolve(t *testing.T, config string, lookup runtime.Identity) map[string]int {
	t.Helper()
	r := require.New(t)

	var cfg v1.Config
	r.NoError(yaml.Unmarshal([]byte(config), &cfg))

	usernames := map[string]int{}
	for range reviewPR4341Runs {
		graph, err := credentials.ToGraph(t.Context(), credentialruntime.ConvertFromV1(&cfg), credentials.Options{})
		r.NoError(err)
		resolved, err := graph.Resolve(t.Context(), lookup)
		r.NoError(err)
		usernames[resolved.(*v1.DirectCredentials).Properties["username"]]++
	}
	return usernames
}

func reviewPR4341Consumer(attribute, value, username string) string {
	return fmt.Sprintf(`  - identity:
      type: OCIRegistry
      hostname: ghcr.io
      %s: %q
    credentials:
      - type: Credentials
        properties:
          username: %s
`, attribute, value, username)
}

// TestReviewPR4341_F1_NestedPathPrefixPrefersLongestPrefix resolves a repository below two
// nested OCM v1 pathprefix entries. OCM v1 selected "the element with the most matching path
// components" (ocm api/credentials/identity/hostpath/identity.go), so the team entry must win.
func TestReviewPR4341_F1_NestedPathPrefixPrefersLongestPrefix(t *testing.T) {
	config := "type: credentials.config.ocm.software\nconsumers:\n" +
		reviewPR4341Consumer("pathprefix", "my-org", "org-user") +
		reviewPR4341Consumer("pathprefix", "my-org/team", "team-user")

	lookup := func(path string) runtime.Identity {
		return runtime.Identity{"type": "OCIRegistry", "scheme": "https", "hostname": "ghcr.io", "path": path}
	}

	t.Run("control: only the org entry matches", func(t *testing.T) {
		got := reviewPR4341Resolve(t, config, lookup("my-org/other/repo"))
		require.Equal(t, map[string]int{"org-user": reviewPR4341Runs}, got)
	})

	t.Run("both entries match, the longer prefix wins", func(t *testing.T) {
		got := reviewPR4341Resolve(t, config, lookup("my-org/team/repo"))
		t.Logf("resolved usernames over %d graphs: %v", reviewPR4341Runs, got)
		require.Equal(t, map[string]int{"team-user": reviewPR4341Runs}, got)
	})
}

// TestReviewPR4341_F1_NestedGlobPrefersNarrowerPattern covers the same ranking for path
// globs. In every case the narrow pattern matches a strict subset of what the broad one
// matches, and both have the same attribute count and the same wildcard count.
func TestReviewPR4341_F1_NestedGlobPrefersNarrowerPattern(t *testing.T) {
	for _, tc := range []struct {
		name   string
		broad  string
		narrow string
		path   string
	}{
		{"control: fewer wildcards decides (docs Example B2)", "*/*", "my-org/*", "my-org/repo"},
		{"any depth below org and below team", "my-org/**", "my-org/team/**", "my-org/team/repo"},
		{"one segment and a name prefix", "my-org/*", "my-org/team-*", "my-org/team-a"},
		{"any depth and two levels below org", "**", "my-org/*/*", "my-org/team/repo"},
		{"any depth and two levels, same org", "my-org/**", "my-org/*/*", "my-org/team/repo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := "type: credentials.config.ocm.software\nconsumers:\n" +
				reviewPR4341Consumer("path", tc.broad, "broad-user") +
				reviewPR4341Consumer("path", tc.narrow, "narrow-user")
			lookup := runtime.Identity{"type": "OCIRegistry", "scheme": "https", "hostname": "ghcr.io", "path": tc.path}

			got := reviewPR4341Resolve(t, config, lookup)
			t.Logf("broad %q, narrow %q, lookup %q: %v", tc.broad, tc.narrow, tc.path, got)
			require.Equal(t, map[string]int{"narrow-user": reviewPR4341Runs}, got)
		})
	}
}
