package internal

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

// Two parents may reference the same child with digests that are spelled differently but both
// valid for the child: another hash algorithm, or the legacy normalisation alias. Discovery must
// not reject them as conflicting; each is satisfiable and can be verified against the child.
func TestReviewPR3889_F2_EquivalentReferenceDigestsConflict(t *testing.T) {
	child := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/child", Version: "2.0.0"}},
			Provider:      descriptor.Provider{Name: "ocm.software"},
		},
	}
	sha256Digest, err := signing.GenerateDigest(t.Context(), child, slog.Default(), v4alpha1.Algorithm, "SHA-256")
	require.NoError(t, err)
	sha512Digest, err := signing.GenerateDigest(t.Context(), child, slog.Default(), v4alpha1.Algorithm, "SHA-512")
	require.NoError(t, err)
	legacyDigest := *sha256Digest
	legacyDigest.NormalisationAlgorithm = signing.LegacyNormalisationAlgo

	tests := []struct {
		name          string
		first, second descriptor.Digest
	}{
		{name: "SHA-256 and SHA-512", first: *sha256Digest, second: *sha512Digest},
		{name: "legacy normalisation alias", first: legacyDigest, second: *sha256Digest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			for _, d := range []descriptor.Digest{tc.first, tc.second} {
				r.NoError(signing.VerifyDigestMatchesDescriptor(t.Context(), child, descriptor.Signature{Digest: d}, slog.Default()),
					"precondition: %+v is a valid digest of the child", d)
			}

			d := &discoverer{recursive: -1, discoveredDigests: make(map[string]descriptor.Digest)}
			parent := func(name string, dig descriptor.Digest) *discoveryValue {
				return &discoveryValue{Descriptor: &descriptor.Descriptor{Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: "1.0.0"}},
					References: []descriptor.Reference{{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: "2.0.0"}},
						Component:   "ocm.software/child",
						Digest:      dig,
					}},
				}}}
			}
			_, err := d.Discover(t.Context(), parent("ocm.software/a", tc.first))
			r.NoError(err)
			_, err = d.Discover(t.Context(), parent("ocm.software/b", tc.second))
			r.NoError(err, "both parents' digests are valid for the child, so discovery must not fail")
		})
	}
}
