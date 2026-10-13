package v1_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
)

// Expected values were produced by two independent oracles and agree byte for
// byte unless stated otherwise:
//   - cc-utils ocm/sign.py (gardener-ocm 1.2847.0, cc-utils d7fe8934ec890ee875540019716b4850f3b13fd8)
//   - OCM v1 compdesc.Normalize(cd, "jsonNormalisation/v1") (ocm.software/ocm v0.52.0)
// See .review/pr-3887/oracle-compare.sh to regenerate them.

// The duplicate-identity check must compare the full extraIdentity, including
// an explicit "version" key. A peer that already carries a different
// extraIdentity.version is a distinct identity, so nothing is injected.
const (
	inputReviewF1 = `{"meta": {"schemaVersion": "v2"}, "component": {"name": "acme.org/case", "version": "1.0.0", "repositoryContexts": [], "provider": "ACME", "sources": [], "componentReferences": [], "resources": [{"name": "img", "version": "1.0.0", "type": "ociImage", "access": {"type": "ociRegistry", "imageReference": "registry.example/img@sha256:aa"}, "digest": {"hashAlgorithm": "SHA-256", "normalisationAlgorithm": "ociArtifactDigest/v1", "value": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "extraIdentity": {}, "relation": "external", "labels": [], "srcRefs": []}, {"name": "img", "version": "1.0.0", "type": "ociImage", "access": {"type": "ociRegistry", "imageReference": "registry.example/img@sha256:aa"}, "digest": {"hashAlgorithm": "SHA-256", "normalisationAlgorithm": "ociArtifactDigest/v1", "value": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, "extraIdentity": {"version": "2.0.0"}, "relation": "external", "labels": [], "srcRefs": []}], "labels": [], "creationTime": null}, "signatures": [], "nestedDigests": []}`
	normReviewF1  = `[{"component":[{"componentReferences":[]},{"name":"acme.org/case"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},{"extraIdentity":null},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}],[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},{"extraIdentity":[{"version":"2.0.0"}]},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
)

// Two resources whose extraIdentity values contain the ";" and "=" delimiters
// are distinct identities and must not be treated as duplicates.
const (
	inputReviewF2 = `{"meta": {"schemaVersion": "v2"}, "component": {"name": "acme.org/case", "version": "1.0.0", "repositoryContexts": [], "provider": "ACME", "sources": [], "componentReferences": [], "resources": [{"name": "res", "version": "1.0.0", "type": "ociImage", "access": {"type": "ociRegistry", "imageReference": "registry.example/img@sha256:aa"}, "digest": {"hashAlgorithm": "SHA-256", "normalisationAlgorithm": "ociArtifactDigest/v1", "value": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "extraIdentity": {"aa": "b;cc=dd"}, "relation": "external", "labels": [], "srcRefs": []}, {"name": "res", "version": "1.0.0", "type": "ociImage", "access": {"type": "ociRegistry", "imageReference": "registry.example/img@sha256:aa"}, "digest": {"hashAlgorithm": "SHA-256", "normalisationAlgorithm": "ociArtifactDigest/v1", "value": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, "extraIdentity": {"aa": "b", "cc": "dd"}, "relation": "external", "labels": [], "srcRefs": []}], "labels": [], "creationTime": null}, "signatures": [], "nestedDigests": []}`
	normReviewF2  = `[{"component":[{"componentReferences":[]},{"name":"acme.org/case"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},{"extraIdentity":[{"aa":"b;cc=dd"}]},{"name":"res"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}],[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},{"extraIdentity":[{"aa":"b"},{"cc":"dd"}]},{"name":"res"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
)

// OCM v1 marshals every entry with json.Marshal, which HTML-escapes &, < and >.
// cc-utils does not escape them, so this expectation is OCM v1 only.
const (
	inputReviewF3   = `{"meta": {"schemaVersion": "v2"}, "component": {"name": "acme.org/case", "version": "1.0.0", "repositoryContexts": [], "provider": "ACME", "sources": [], "componentReferences": [], "resources": [{"name": "img", "version": "1.0.0", "type": "ociImage", "access": {"type": "ociRegistry", "imageReference": "registry.example/img@sha256:aa"}, "digest": {"hashAlgorithm": "SHA-256", "normalisationAlgorithm": "ociArtifactDigest/v1", "value": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, "extraIdentity": {}, "relation": "external", "labels": [], "srcRefs": []}], "labels": [{"name": "url", "value": "https://x.example/?a=1&b=<2>", "version": null, "signing": true}], "creationTime": null}, "signatures": [], "nestedDigests": []}`
	normReviewF3OCM = `[{"component":[{"componentReferences":[]},{"labels":[[{"name":"url"},{"signing":true},{"value":"https://x.example/?a=1\u0026b=\u003c2\u003e"}]]},{"name":"acme.org/case"},{"provider":"ACME"},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},{"extraIdentity":null},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
)

func TestReviewPR3887_DuplicateIdentity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"F1-peer-with-explicit-version-is-distinct", inputReviewF1, normReviewF1},
		{"F2-delimiters-in-extra-identity-do-not-collide", inputReviewF2, normReviewF2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := normalisation.Normalise(fromJSON(t, tt.input), v1.Algorithm)
			r.NoError(err)
			r.Equal(tt.expected, string(got))
		})
	}
}

func TestReviewPR3887_F3_HTMLEscapingMatchesOCMv1(t *testing.T) {
	r := require.New(t)
	got, err := normalisation.Normalise(fromJSON(t, inputReviewF3), v1.Algorithm)
	r.NoError(err)
	r.Equal(normReviewF3OCM, string(got))
}

// OCM v1 applies the label rule to provider labels (CDExcludes "provider" ->
// "labels": LabelExcludes), so a non-signing provider label is not signed.
const (
	inputReviewF4   = `{"meta": {"schemaVersion": "v2"}, "component": {"name": "acme.org/c", "version": "1.0.0", "provider": "{\"name\": \"ACME\", \"labels\": [{\"name\": \"team\", \"value\": \"x\"}]}", "repositoryContexts": [], "sources": [], "resources": [{"name": "img", "version": "1.0.0", "type": "ociImage", "relation": "external", "access": {"type": "ociRegistry", "imageReference": "r.example/img:1"}, "digest": {"hashAlgorithm": "SHA-256", "normalisationAlgorithm": "ociArtifactDigest/v1", "value": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}], "componentReferences": []}}`
	normReviewF4OCM = `[{"component":[{"componentReferences":[]},{"name":"acme.org/c"},{"provider":[{"name":"ACME"}]},{"resources":[[{"digest":[{"hashAlgorithm":"SHA-256"},{"normalisationAlgorithm":"ociArtifactDigest/v1"},{"value":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},{"extraIdentity":null},{"name":"img"},{"relation":"external"},{"type":"ociImage"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
)

func TestReviewPR3887_F4_ProviderNonSigningLabelsDropped(t *testing.T) {
	r := require.New(t)
	got, err := normalisation.Normalise(fromJSON(t, inputReviewF4), v1.Algorithm)
	r.NoError(err)
	r.Equal(normReviewF4OCM, string(got))
}
