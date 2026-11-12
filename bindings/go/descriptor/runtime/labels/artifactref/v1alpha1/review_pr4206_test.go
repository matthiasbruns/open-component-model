package v1alpha1_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"

	artifactref "ocm.software/open-component-model/bindings/go/descriptor/runtime/labels/artifactref/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestReviewPR4206_F4c_NilIdentityEncodesAsNull covers the nil-map wire shape on content that
// can be signed, which is the gap left by runtime.JSONOptions having no direct test.
//
// Reference.Identity is a runtime.Identity (map[string]string) tagged `json:"identity"` with no
// omit option, so the member is always emitted. encoding/json v1 wrote a nil map as null;
// encoding/json/v2 writes {} unless FormatNilMapAsNull is set. These bytes end up inside the
// value of the ocm.software/artifact-references label, which an element can mark
// signing: true.
//
// Dropping FormatNilMapAsNull from runtime.JSONOptions currently fails exactly one test in the
// module, and that test is about a pointer marshaler encoding a zero time as null - the nil-map
// shape is only caught incidentally by its JSON comparison helper. artifactref_test.go has no
// case with an unset Identity: every existing case supplies a populated one.
func TestReviewPR4206_F4c_NilIdentityEncodesAsNull(t *testing.T) {
	for _, tt := range []struct {
		name string
		refs artifactref.References
		want string
	}{
		{
			name: "unset identity",
			refs: artifactref.References{{}},
			want: `[{"identity":null}]`,
		},
		{
			// An empty but non-nil map encodes as {} under both v1 and v2; only the nil
			// case is affected by FormatNilMapAsNull.
			name: "explicitly empty identity",
			refs: artifactref.References{{Identity: runtime.Identity{}}},
			want: `[{"identity":{}}]`,
		},
		{
			name: "populated identity",
			refs: artifactref.References{{Identity: runtime.Identity{"name": "r1"}}},
			want: `[{"identity":{"name":"r1"}}]`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			data, err := json.Marshal(tt.refs, runtime.JSONOptions)
			r.NoError(err)
			r.Equal(tt.want, string(data),
				"label value bytes changed shape; this content can be signing relevant")
		})
	}
}
