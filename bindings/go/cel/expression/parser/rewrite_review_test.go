package parser_test

import (
	"testing"

	"cel.dev/cel-go/cel"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cel/expression/parser"
)

func TestReviewRewriteIdentifierPreservesCELSemantics(t *testing.T) {
	for _, tt := range []struct {
		name string
		expr string
		want string
	}{
		{
			name: "unshadowed_alias_control",
			expr: `resource.labels.map(label, label.name)`,
			want: `environment.source.labels.map(label, label.name)`,
		},
		{
			name: "macro_variable_shadows_alias",
			expr: `resource.labels.map(resource, resource.name)`,
			want: `environment.source.labels.map(resource, resource.name)`,
		},
		{
			name: "alias_outside_shadowed_scope",
			expr: `["x"].map(resource, resource)[0] + resource.name`,
			want: `["x"].map(resource, resource)[0] + environment.source.name`,
		},
		{
			name: "member_access_with_whitespace",
			expr: `resource.access. resource`,
			want: `environment.source.access. resource`,
		},
		{
			name: "triple_double_quoted_literal",
			expr: `"""a " resource.name " b""" + resource.name`,
			want: `"""a " resource.name " b""" + environment.source.name`,
		},
		{
			name: "triple_single_quoted_literal",
			expr: `'''a ' resource.name ' b''' + resource.name`,
			want: `'''a ' resource.name ' b''' + environment.source.name`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			env, err := cel.NewEnv(cel.Variable("resource", cel.DynType), cel.Variable("environment", cel.DynType))
			r.NoError(err)
			_, issues := env.Compile(tt.expr)
			r.NoError(issues.Err(), "the original expression must be valid CEL")
			_, issues = env.Compile(tt.want)
			r.NoError(issues.Err(), "the expected rewrite must be valid CEL")
			r.Equal(tt.want, parser.RewriteIdentifier(tt.expr, "resource", "environment.source"))
		})
	}
}
