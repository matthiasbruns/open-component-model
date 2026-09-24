package internal

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

func TestReviewUploaderExtraIdentityBuildAndCheck(t *testing.T) {
	plain := wgetResource("plain", "1.0.0", "https://source.example/plain")
	selected := wgetResource("blob", "1.0.0", "https://source.example/public")
	selected.ExtraIdentity = runtime.Identity{"tier": "public"}
	other := wgetResource("blob", "1.0.0", "https://source.example/private")
	other.ExtraIdentity = runtime.Identity{"tier": "private"}

	for _, tt := range []struct {
		name      string
		resources []descriptor.Resource
		extra     runtime.Identity
	}{
		{name: "no_extra_identity_control", resources: []descriptor.Resource{plain}},
		{name: "single_extra_identity", resources: []descriptor.Resource{selected}, extra: selected.ExtraIdentity},
		{name: "same_name_and_version", resources: []descriptor.Resource{other, selected}, extra: selected.ExtraIdentity},
		{name: "missing_extra_identity_first", resources: []descriptor.Resource{plain, selected}, extra: selected.ExtraIdentity},
		{name: "missing_extra_identity_last", resources: []descriptor.Resource{selected, plain}, extra: selected.ExtraIdentity},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			desc := testDescriptor("ocm.software/test", "1.0.0", tt.resources, nil)
			resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
			roots := testTransferRoots("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/target"), resolver)
			uploader := wgetUploader(t, `${"https://target.example/" + resource.name}`)
			uploader.MatchSpec.ExtraIdentity = tt.extra
			tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{
				CopyMode: transferv1alpha1.CopyModeLocalBlobResources,
			}, []transferv1alpha1.UploaderConfig{uploader})
			r.NoError(err)
			streamCount := 0
			for _, tr := range tgd.Transformations {
				if tr.Type == wgetv1alpha1.HTTPStreamingV1alpha1 {
					streamCount++
				}
			}
			r.Equal(1, streamCount, "the uploader must match exactly the intended resource")

			// Use the descriptor environment and schema inference from the real builder,
			// not a hand-written CEL environment that might mask identity field types.
			_, err = NewDefaultBuilder(nil, nil, nil, nil).BuildAndCheck(tgd)
			r.NoError(err, "a resource selected by its full identity must pass graph checking")
		})
	}
}

func TestReviewUploaderPreservesNestedLiteralExpression(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "literal_only_control",
			input: `${"${resource.name}"}`,
			want:  `${"${resource.name}"}`,
		},
		{
			name:  "real_expression_before_literal",
			input: `${resource.name}/${"${resource.name}"}`,
			want:  `${environment.source.name}/${"${resource.name}"}`,
		},
		{
			name:  "real_expression_after_literal",
			input: `${"${resource.name}"}/${resource.name}`,
			want:  `${"${resource.name}"}/${environment.source.name}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			data, err := json.Marshal(map[string]any{"url": tt.input})
			r.NoError(err)
			raw := &runtime.Raw{Data: data}
			r.NoError(templateExpressions(raw, "environment.source"))
			var got map[string]any
			r.NoError(json.Unmarshal(raw.Data, &got))
			r.Equal(tt.want, got["url"], "nested expression text inside a CEL literal must remain literal")
		})
	}
}
