package credentialrepository_test

// Tests for credential type auto-registration from builtin credential repository plugins.
// Custom credential type registration from external plugins (CustomCredentialTypes in capability
// specs) is handled by PluginManager and tested in manager_test.go.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestRegisterInternalPlugin_AutoRegistersCredentialTypes verifies that when a builtin
// CredentialRepositoryPlugin also implements credentials.CredentialTypeSchemeProvider,
// its credential types are automatically registered into PluginManager.CredentialTypeRegistry.
func TestRegisterInternalPlugin_AutoRegistersCredentialTypes(t *testing.T) {
	r := require.New(t)
	pm := manager.NewPluginManager(t.Context())

	credType := runtime.NewVersionedType("AutoRegisteredCred", "v1")
	plugin := &mockBuiltinPlugin{credType: credType}

	r.NoError(pm.RegisterInternalCredentialRepositoryPlugin(plugin, nil))

	r.True(pm.CredentialTypeRegistry.Scheme().IsRegistered(credType),
		"credential type should be auto-registered from the plugin via PluginManager")
}

// TestRegisterInternalPlugin_WithoutCredentialTypes verifies that plugins that do NOT
// implement CredentialTypeSchemeProvider still register without errors.
func TestRegisterInternalPlugin_WithoutCredentialTypes(t *testing.T) {
	r := require.New(t)
	pm := manager.NewPluginManager(t.Context())
	r.NoError(pm.RegisterInternalCredentialRepositoryPlugin(&mockBuiltinPluginNoCredTypes{}, nil))
}

// mockBuiltinPlugin implements BuiltinCredentialRepositoryPlugin and
// credentials.CredentialTypeSchemeProvider.
type mockBuiltinPlugin struct {
	credType runtime.Type
}

func (m *mockBuiltinPlugin) GetCredentialRepositoryScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&dummyv1.Repository{},
		runtime.NewVersionedType(dummyv1.Type, dummyv1.Version),
		runtime.NewUnversionedType(dummyv1.Type),
	)
	return s
}

func (m *mockBuiltinPlugin) GetCredentialTypeScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&runtime.Raw{}, m.credType)
	return s
}

func (m *mockBuiltinPlugin) Resolve(_ context.Context, _ runtime.Typed, _ runtime.Identity, _ runtime.Typed) (runtime.Typed, error) {
	return nil, nil
}

func (m *mockBuiltinPlugin) ConsumerIdentityForConfig(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

// mockBuiltinPluginNoCredTypes implements BuiltinCredentialRepositoryPlugin only,
// without CredentialTypeSchemeProvider.
type mockBuiltinPluginNoCredTypes struct{}

func (m *mockBuiltinPluginNoCredTypes) GetCredentialRepositoryScheme() *runtime.Scheme {
	return runtime.NewScheme()
}

func (m *mockBuiltinPluginNoCredTypes) Resolve(_ context.Context, _ runtime.Typed, _ runtime.Identity, _ runtime.Typed) (runtime.Typed, error) {
	return nil, nil
}

func (m *mockBuiltinPluginNoCredTypes) ConsumerIdentityForConfig(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}
