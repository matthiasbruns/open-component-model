package oci_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func TestReviewPR3635_DirectWriteWithoutVersionConfiguration(t *testing.T) {
	for _, version := range []string{"1.0.0", "not-a-version"} {
		t.Run(version, func(t *testing.T) {
			r := require.New(t)
			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)

			repo := Repository(t, ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
			desc := &descriptor.Descriptor{
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "acme.org/review", Version: version}},
					Provider:      descriptor.Provider{Name: "acme"},
					Resources: []descriptor.Resource{{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "artifact", Version: "not a version"}},
						Type:        "test", Relation: descriptor.ExternalRelation,
						Access: &runtime.Raw{Type: runtime.Type{Name: "review"}, Data: []byte(`{"type":"review"}`)},
					}},
				},
			}
			r.False(versioning.Default().Valid(desc.Component.Resources[0].Version))
			r.NoError(repo.AddComponentVersion(t.Context(), desc))
			got, err := repo.GetComponentVersion(t.Context(), desc.Component.Name, version)
			r.NoError(err)
			r.Equal(version, got.Component.Version)
			r.Equal("not a version", got.Component.Resources[0].Version)
			t.Logf("default repository persisted component version %q with invalid resource version %q", version, got.Component.Resources[0].Version)
		})
	}
}
