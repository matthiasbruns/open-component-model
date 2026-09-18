package oci_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
)

// TestPR3635_NonSemverVersionSurvivesRoundTrip covers the write/read
// asymmetry this PR introduces.
//
// Write side: the constructor/descriptor JSON schema no longer pins versions to
// a semver regex, VersionToOCITag accepts "2024.03.15.2" (it is a valid OCI
// tag), and a configured regex scheme makes it a valid version. So
// AddComponentVersion stores it.
//
// Read side: ListComponentVersions is unchanged. It still hardcodes
// lister.SortPolicyLooseSemverDescending, whose sort loop skips every candidate
// semver.NewVersion cannot parse. lister.Options.Comparator was added in this PR
// to plumb the versioning registry down here, but nothing ever assigns it.
//
// Result: the component version is written without error and then silently
// disappears from the listing. On main this state was unreachable, because the
// schema rejected "2024.03.15.2" at parse time.
func TestPR3635_NonSemverVersionSurvivesRoundTrip(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	const component = "acme.org/svc"
	// Every version below is lowercase and matches the OCI tag grammar
	// ^[\w][\w.-]{0,127}$ that VersionToOCITag now enforces.
	versions := []string{"1.0.0", "2024.03.15", "2024.03.15.2", "2024.03.15.build42"}

	for _, v := range versions {
		err := repo.AddComponentVersion(ctx, &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider: descriptor.Provider{Name: "acme"},
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: component, Version: v},
				},
			},
		})
		r.NoError(err, "AddComponentVersion(%q) must not fail", v)
	}

	listed, err := repo.ListComponentVersions(ctx, component)
	r.NoError(err)

	// Actual: ["2024.03.15" "1.0.0"] - the two four-segment versions are gone,
	// with no error and no warning.
	r.ElementsMatch(versions, listed, "every stored version must be listable")
}
