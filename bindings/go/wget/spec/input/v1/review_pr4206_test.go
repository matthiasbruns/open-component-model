package v1_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/runtime"
	accessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	inputv1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

// TestReviewPR4206_F6_AccessAndInputAgreeOnURLCasing pins that the wget access and input types
// accept the same spelling of their URL member.
//
// This PR adopts case-sensitive member matching and adds `case:ignore` to the URL member of
// wget/spec/access/v1 so legacy specs keep decoding - covered by
// wget/spec/access/v1/wget_test.go, which feeds "URL". wget/spec/input/v1 declares the same
// member as plain `json:"url"` and did not get the tag, so within one resource the identical
// key is now accepted under access: and silently dropped under input:.
//
// Both types are decoded here through runtime.Scheme, which is the path production uses for a
// nested spec, so the two columns differ only in the type being decoded. Before this PR both
// went through encoding/json v1 and both accepted "URL".
//
// Validate() then reports "url is required" for a document that does supply it, which sends the
// user looking at their own input rather than at the casing.
func TestReviewPR4206_F6_AccessAndInputAgreeOnURLCasing(t *testing.T) {
	const legacyURL = "https://example.com/file.tar"

	for _, tt := range []struct {
		name string
		doc  string
	}{
		{
			name: "current lower-case url",
			doc:  "type: Wget/v1\nurl: " + legacyURL + "\n",
		},
		{
			name: "legacy upper-case URL",
			doc:  "type: Wget/v1\nURL: " + legacyURL + "\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// The nested spec reaches the scheme as a Raw, because the enclosing document is
			// decoded by sigs.k8s.io/yaml.
			var raw runtime.Raw
			r.NoError(yaml.Unmarshal([]byte(tt.doc), &raw))

			accessScheme := runtime.NewScheme()
			accessScheme.MustRegisterWithAlias(&accessv1.Wget{},
				runtime.NewVersionedType("Wget", accessv1.Version))
			var access accessv1.Wget
			r.NoError(accessScheme.Convert(&raw, &access))

			inputScheme := runtime.NewScheme()
			inputScheme.MustRegisterWithAlias(&inputv1.Wget{},
				runtime.NewVersionedType("Wget", inputv1.Version))
			var input inputv1.Wget
			r.NoError(inputScheme.Convert(&raw, &input))

			r.Equal(access.URL, input.URL,
				"wget access and input disagree on this spelling of the URL member")
			r.Equal(legacyURL, input.URL, "input dropped the URL member")
			r.NoError(input.Validate())
		})
	}
}
