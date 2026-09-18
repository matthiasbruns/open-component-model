package versioning_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

// TestPR3635_FilterDropsVersionsNoSchemeClaims pins the pre-PR contract of
// cli/internal/repository/ocm.filterBySemver: a semver constraint must drop
// versions that no registered scheme claims, such as floating tags.
//
// Registry.Filter retains them instead. schemeFor returns a nil Scheme for an
// unclaimed version, and nil also fails the *looseSemverScheme type assertion,
// so unclaimed versions take the "retain non-semver scheme" branch that was
// meant only for calver-style histories. SortDescending then places them ahead
// of every real release via the lexical Compare fallback.
//
// Reach: not observable through OCI or CTF, whose lister already drops
// non-semver tags upstream. It is observable through any plugin-provided
// ComponentVersionRepository and through direct use of this package.
//
// This test passes on main and fails on 4ac17e67e.
func TestPR3635_FilterDropsVersionsNoSchemeClaims(t *testing.T) {
	r := require.New(t)

	// versioning.Default() == no versioning configuration at all.
	reg := versioning.Default()
	r.Len(reg.Schemes(), 1)
	r.False(reg.Valid("latest"), "no scheme in the default registry claims %q", "latest")
	r.False(reg.Valid("main"), "no scheme in the default registry claims %q", "main")

	out, err := reg.Filter([]string{"1.0.0", "latest", "2.0.0", "0.1.0", "main"}, ">=1.0.0")
	r.NoError(err)
	r.Equal([]string{"1.0.0", "2.0.0"}, out)

	reg.SortDescending(out)
	r.Equal("2.0.0", out[0], "--latest must resolve to the newest release")
}

// TestPR3635_FilterKeepsGenuineNonSemverScheme is the behaviour a fix must
// preserve: a version claimed by a configured non-semver scheme is retained, so
// a semver constraint never discards a calver history.
//
// This test passes on 4ac17e67e and must keep passing.
func TestPR3635_FilterKeepsGenuineNonSemverScheme(t *testing.T) {
	r := require.New(t)

	reg := versioning.NewRegistry(calverFull(), versioning.Default().Schemes()[0])
	r.True(reg.Valid("2024.03.15"), "calver-full claims %q", "2024.03.15")

	out, err := reg.Filter([]string{"1.0.0", "2024.03.15", "0.1.0"}, ">=1.0.0")
	r.NoError(err)
	r.Equal([]string{"1.0.0", "2024.03.15"}, out)
}
