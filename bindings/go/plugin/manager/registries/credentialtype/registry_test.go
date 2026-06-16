package credentialtype_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtype"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestRegister_MergesSchemes(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry()

	schemeA := runtime.NewScheme()
	schemeA.MustRegisterWithAlias(&runtime.Raw{}, runtime.NewVersionedType("CredA", "v1"))
	schemeB := runtime.NewScheme()
	schemeB.MustRegisterWithAlias(&runtime.Raw{}, runtime.NewVersionedType("CredB", "v1"))

	reg.Register(schemeA)
	reg.Register(schemeB)

	s := reg.Scheme()
	r.True(s.IsRegistered(runtime.NewVersionedType("CredA", "v1")))
	r.True(s.IsRegistered(runtime.NewVersionedType("CredB", "v1")))
}

func TestRegisterCustomTypes_MultipleTypes(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry()

	typeA := runtime.NewVersionedType("CredA", "v1")
	typeB := runtime.NewVersionedType("CredB", "v2")
	r.NoError(reg.RegisterCustomTypes([]mtypes.Type{
		{Type: typeA},
		{Type: typeB},
	}))

	s := reg.Scheme()
	r.True(s.IsRegistered(typeA))
	r.True(s.IsRegistered(typeB))
}

func TestRegisterCustomTypes_ConflictsBetweenCalls(t *testing.T) {
	typeA := runtime.NewVersionedType("CredA", "v1")
	aliasA := runtime.NewUnversionedType("CredA")
	typeB := runtime.NewVersionedType("CredB", "v1")

	tests := []struct {
		name   string
		first  []mtypes.Type
		second []mtypes.Type
	}{
		{
			name:   "same canonical type registered twice",
			first:  []mtypes.Type{{Type: typeA}},
			second: []mtypes.Type{{Type: typeA}},
		},
		{
			name:   "second canonical conflicts with first alias",
			first:  []mtypes.Type{{Type: typeA, Aliases: []runtime.Type{aliasA}}},
			second: []mtypes.Type{{Type: aliasA}},
		},
		{
			name:   "second alias conflicts with first canonical",
			first:  []mtypes.Type{{Type: typeA}},
			second: []mtypes.Type{{Type: typeB, Aliases: []runtime.Type{typeA}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			reg := credentialtype.NewRegistry()
			r.NoError(reg.RegisterCustomTypes(tc.first))
			r.Error(reg.RegisterCustomTypes(tc.second))
		})
	}
}

func TestRegisterCustomTypes_NonConflictingTypesStillRegistered(t *testing.T) {
	r := require.New(t)
	reg := credentialtype.NewRegistry()

	typeA := runtime.NewVersionedType("CredA", "v1")
	typeB := runtime.NewVersionedType("CredB", "v1")

	r.NoError(reg.RegisterCustomTypes([]mtypes.Type{{Type: typeA}}))
	// typeA conflicts, typeB does not — typeB should still be registered
	err := reg.RegisterCustomTypes([]mtypes.Type{{Type: typeA}, {Type: typeB}})
	r.Error(err)
	r.True(reg.Scheme().IsRegistered(typeB), "non-conflicting type must still be registered")
}

func TestRegisterCustomTypes_TypesDoNotAliasEachOther(t *testing.T) {
	typeA := runtime.NewVersionedType("PluginCredA", "v1")
	typeB := runtime.NewVersionedType("PluginCredB", "v1")
	typeC := runtime.NewVersionedType("PluginCredC", "v2")

	reg := credentialtype.NewRegistry()
	require.NoError(t, reg.RegisterCustomTypes([]mtypes.Type{
		{Type: typeA},
		{Type: typeB},
		{Type: typeC},
	}))

	s := reg.Scheme()

	for _, typ := range []runtime.Type{typeA, typeB, typeC} {
		obj, err := s.NewObject(typ)
		require.NoError(t, err)
		raw, ok := obj.(*runtime.Raw)
		require.True(t, ok)
		require.Equal(t, typ, raw.GetType(), "NewObject(%s) must not bleed into another type", typ)
	}
}

func TestGetCredentialTypeScheme_ImplementsProvider(t *testing.T) {
	// Verify Registry satisfies credentials.CredentialTypeSchemeProvider via
	// compile-time duck typing (GetCredentialTypeScheme returns the same scheme).
	reg := credentialtype.NewRegistry()
	require.Equal(t, reg.Scheme(), reg.GetCredentialTypeScheme())
}
