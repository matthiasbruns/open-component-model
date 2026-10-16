package v1_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
)

// TestReviewPR3887_R2_ResourceTypeAliases asserts the resource type that
// cc-utils hashes. cc-utils loads resource types through the ArtefactType enum,
// whose alias table maps legacy spellings to one canonical value, and sign.py
// serialises that canonical value:
// https://github.com/gardener/cc-utils/blob/01d0cffaea759127c915eed481b5fc6339fb1e06/ocm/__init__.py#L236-L265
// The expected values were confirmed by running ocm/sign.py unchanged
// (.review/pr-3887/ccutils-oracle/cases/c16-resource-type-aliases).
func TestReviewPR3887_R2_ResourceTypeAliases(t *testing.T) {
	tests := []struct {
		stored string
		signed string
	}{
		{"blob", "blob/v1"},
		{"helmChart", "helmChart/v1"},
		{"ociArtifact", "ociArtifact/v1"},
		{"filesystem", "directoryTree"},
		{"ociImage/v1", "ociImage"},
		{"git/v1", "git"},
		{"npmPackage", "npmPackage"}, // control: already canonical
	}
	for _, tt := range tests {
		t.Run(tt.stored, func(t *testing.T) {
			r := require.New(t)
			desc := fromYAML(t, fmt.Sprintf(`
meta:
  schemaVersion: v2
component:
  name: acme.org/case
  version: 1.0.0
  provider: ACME
  repositoryContexts: []
  sources: []
  componentReferences: []
  resources:
  - name: r
    version: 1.0.0
    type: %s
    relation: external
    access: {type: ociRegistry, imageReference: "registry.example/img:1"}
    digest: {hashAlgorithm: SHA-256, normalisationAlgorithm: genericBlobDigest/v1, value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
`, tt.stored))

			got, err := normalisation.Normalise(desc, v1.Algorithm)
			r.NoError(err)
			r.Contains(string(got), fmt.Sprintf(`{"type":%q}`, tt.signed), "resource type differs from the value cc-utils signs")
		})
	}
}
