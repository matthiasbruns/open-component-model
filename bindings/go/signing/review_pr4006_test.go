package signing_test

import (
	"crypto"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/signing"
)

// Expected digests are the output of legacy ocm.software/ocm v0.53.0
// compdesc.Normalize(cd, compdesc.JsonNormalisationV3) + SHA-256 on the same
// YAML (independent oracle, not recomputed with this module).
func TestReviewPR4006_F1_LegacyV3SignaturesVerify(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		v3Digest string
	}{
		{
			// sanity: no label.version anywhere, v3 == v4alpha1 here
			name: "no versioned labels",
			input: `
meta: {schemaVersion: v2}
component:
  name: acme.org/delta-free
  version: 1.0.0
  provider: acme
  repositoryContexts: []
  componentReferences: []
  sources: []
  labels:
    - {name: comp-sig, value: "cv", signing: true}
    - {name: comp-nosig, value: "nv"}
  resources:
    - name: res
      version: 1.0.0
      type: blob
      relation: local
      access: {type: localBlob, localReference: "sha256:aaaa", mediaType: application/octet-stream}
      digest: {hashAlgorithm: SHA-256, normalisationAlgorithm: genericBlobDigest/v1, value: aaaa}
`,
			v3Digest: "2a3511daae5bc631a09c866f85cf86ac848972f0b20131edc8e54e0c115c2c53",
		},
		{
			name: "signing label with version",
			input: `
meta: {schemaVersion: v2}
component:
  name: acme.org/version-labels
  version: 1.0.0
  provider: acme
  repositoryContexts: []
  componentReferences: []
  labels:
    - {name: comp-sig, value: "cv", signing: true, version: v1}
    - {name: comp-nosig, value: "nv"}
  resources:
    - name: res
      version: 1.0.0
      type: blob
      relation: local
      access: {type: localBlob, localReference: "sha256:aaaa", mediaType: application/octet-stream}
      digest: {hashAlgorithm: SHA-256, normalisationAlgorithm: genericBlobDigest/v1, value: aaaa}
      labels:
        - {name: res-sig, value: "rv", signing: true, version: v3}
        - {name: res-nosig, value: "nv"}
  sources:
    - name: src
      version: 1.0.0
      type: git
      access: {type: git, repoUrl: github.com/acme/x}
      labels:
        - {name: src-sig, value: "sv", signing: true, version: v4}
`,
			v3Digest: "97d6edffa6a0bc85795b4a5bfda0f4f36aae1dad86b34d80aa1cd12727f41561",
		},
		{
			name: "reference label with version",
			input: `
meta: {schemaVersion: v2}
component:
  name: acme.org/reference-labels
  version: 1.0.0
  provider: acme
  repositoryContexts: []
  resources: []
  sources: []
  componentReferences:
    - name: ref
      componentName: acme.org/child
      version: 1.0.0
      digest: {hashAlgorithm: SHA-256, normalisationAlgorithm: jsonNormalisation/v4alpha1, value: deadbeef}
      labels:
        - {name: ref-sig, value: "rsv", signing: true, version: v5}
        - {name: ref-nosig, value: "rnv"}
`,
			v3Digest: "7d783015ce912c74cfe541f943adec9c0b9651fce9a79ec8e49d0288d09d9a87",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var v2desc descriptorv2.Descriptor
			r.NoError(yaml.Unmarshal([]byte(tt.input), &v2desc))
			desc, err := descruntime.ConvertFromV2(&v2desc)
			r.NoError(err)

			got, err := signing.GenerateDigest(t.Context(), desc, slog.New(slog.DiscardHandler),
				signing.LegacyNormalisationAlgo, crypto.SHA256.String())
			r.NoError(err)
			r.Equal(tt.v3Digest, got.Value,
				"a jsonNormalisation/v3 signature minted by OCM v1 must verify in OCM v2")
		})
	}
}

// Expected digests are the output of legacy ocm.software/ocm v0.53.0
// compdesc.Normalize(cd, compdesc.JsonNormalisationV3) + SHA-256 on the same YAML.
// Legacy v3 emits componentReferences unfiltered, so a reference label's merge
// spec is part of the signed bytes; OCM v2's Label type has no Merge field.
func TestReviewPR4006_F4_LegacyV3ReferenceLabelMergeVerifies(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		v3Digest string
	}{
		{
			// sanity: merge on a signing component label is dropped by v3 too
			name: "component signing label with merge",
			input: `
meta: {schemaVersion: v2}
component:
  name: acme.org/comp-merge
  version: 1.0.0
  provider: acme
  repositoryContexts: []
  resources: []
  sources: []
  componentReferences: []
  labels:
    - name: comp-sig
      value: {a: 1}
      signing: true
      merge: {algorithm: default}
`,
			v3Digest: "c658000dd6025f55fd831b323af4493e77db00e51d21dc0c3b5c8e8cd3453735",
		},
		{
			name: "reference label with merge",
			input: `
meta: {schemaVersion: v2}
component:
  name: acme.org/ref-merge
  version: 1.0.0
  provider: acme
  repositoryContexts: []
  resources: []
  sources: []
  componentReferences:
    - name: ref
      componentName: acme.org/child
      version: 1.0.0
      digest: {hashAlgorithm: SHA-256, normalisationAlgorithm: jsonNormalisation/v3, value: deadbeef}
      labels:
        - name: ref-merge
          value: {a: 1}
          merge: {algorithm: default}
`,
			v3Digest: "7f46d5ea7c3f58ec2c8275319962094460c041dfdccf84d3086722df5be259d4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var v2desc descriptorv2.Descriptor
			r.NoError(yaml.Unmarshal([]byte(tt.input), &v2desc))
			desc, err := descruntime.ConvertFromV2(&v2desc)
			r.NoError(err)

			got, err := signing.GenerateDigest(t.Context(), desc, slog.New(slog.DiscardHandler),
				signing.LegacyNormalisationAlgo, crypto.SHA256.String())
			r.NoError(err)
			r.Equal(tt.v3Digest, got.Value,
				"a jsonNormalisation/v3 signature minted by OCM v1 must verify in OCM v2")
		})
	}
}
