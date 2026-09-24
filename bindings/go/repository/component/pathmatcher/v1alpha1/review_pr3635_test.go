package v1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func TestReviewPR3635_CustomRouting(t *testing.T) {
	for _, tc := range []struct{ constraint, version, want string }{
		{">=1000 <2000", "1200", "constrained"},
		{">=1000 <2000", "2200", "fallback"},
		{">=1000 <2000", "unknown", "fallback"},
		{">=1000 <", "2200", "constrained"},
		{">=", "1", "constrained"},
	} {
		t.Run(tc.constraint+"/"+tc.version, func(t *testing.T) {
			r := require.New(t)
			scheme, ok := versioning.BuiltinScheme(versioning.BuiltinBuildNumber)
			r.True(ok)
			constrained := &runtime.Raw{Type: runtime.Type{Name: "constrained"}}
			fallback := &runtime.Raw{Type: runtime.Type{Name: "fallback"}}
			p, err := pathmatcher.NewSpecProvider(t.Context(), []*resolverspec.Resolver{
				{Repository: constrained, ComponentNamePattern: "acme.org/*", VersionConstraint: tc.constraint},
				{Repository: fallback, ComponentNamePattern: "*"},
			}, pathmatcher.WithVersioningRegistry(versioning.NewRegistry(scheme)))
			r.NoError(err)
			got, err := p.GetRepositorySpec(t.Context(), runtime.Identity{
				descriptor.IdentityAttributeName:    "acme.org/service",
				descriptor.IdentityAttributeVersion: tc.version,
			})
			r.NoError(err)
			r.Equal(tc.want, got.GetType().Name)
			t.Logf("constraint %q, version %q routed to %q", tc.constraint, tc.version, got.GetType().Name)
		})
	}
}
