package oci_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
)

func TestReviewPR3635RoundTrip(t *testing.T) {
	for _, policy := range []oci.ReferrerTrackingPolicy{oci.ReferrerTrackingPolicyNone, oci.ReferrerTrackingPolicyByIndexAndSubject} {
		t.Run(fmt.Sprint(policy), func(t *testing.T) {
			r := require.New(t)
			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			repo := Repository(t, ocictf.WithCTF(store), oci.WithReferrerTrackingPolicy(policy))
			versions := []string{"1.0.0+build.5", "2024.03.15", "build-1837"}
			for _, version := range versions {
				desc := &descriptor.Descriptor{
					Meta: descriptor.Meta{Version: "v2"},
					Component: descriptor.Component{
						Provider: descriptor.Provider{Name: "test"},
						ComponentMeta: descriptor.ComponentMeta{
							ObjectMeta: descriptor.ObjectMeta{Name: "acme.org/service", Version: version},
						},
					},
				}
				r.NoError(repo.AddComponentVersion(t.Context(), desc))
				got, err := repo.GetComponentVersion(t.Context(), "acme.org/service", version)
				r.NoError(err)
				r.Equal(version, got.Component.Version)
			}
			got, err := repo.ListComponentVersions(t.Context(), "acme.org/service")
			r.NoError(err)
			r.ElementsMatch(versions, got)
			t.Logf("listed: %q", got)
		})
	}
}
