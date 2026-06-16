// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package credentialtype

import (
	"errors"
	"fmt"
	"sync"

	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Registry is the single source of truth for all known credential payload types.
// It is shared across every plugin registry in the PluginManager so that types
// declared by signing handlers, input plugins, credential repository plugins, and
// credential plugins all end up in one place.
type Registry struct {
	mu     sync.Mutex
	scheme *runtime.Scheme
}

// NewRegistry creates an empty credential type registry.
func NewRegistry() *Registry {
	return &Registry{scheme: runtime.NewScheme()}
}

// Register merges all types from the given scheme into the registry.
// Use this for pre-built schemes that belong to a single builtin plugin.
func (r *Registry) Register(scheme *runtime.Scheme) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scheme.MustRegisterScheme(scheme)
}

// RegisterCustomTypes registers credential types declared by an external plugin.
// Each entry in types is checked for conflicts against already-registered types;
// conflicting entries are skipped and their errors are joined into the return value.
// Non-conflicting entries are always registered, even when other entries conflict.
func (r *Registry) RegisterCustomTypes(types []mtypes.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs []error
	for _, t := range types {
		allTypes := append([]runtime.Type{t.Type}, t.Aliases...)
		conflict := false
		for _, alias := range allTypes {
			if r.scheme.IsRegistered(alias) {
				errs = append(errs, fmt.Errorf("credential type %s already registered", alias))
				conflict = true
			}
		}
		if conflict {
			continue
		}
		typed := &runtime.Raw{}
		typed.SetType(t.Type)
		if err := r.scheme.RegisterWithAlias(typed, allTypes...); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Scheme returns the combined runtime.Scheme containing all registered credential types.
func (r *Registry) Scheme() *runtime.Scheme {
	return r.scheme
}

// GetCredentialTypeScheme implements credentials.CredentialTypeSchemeProvider so
// that the Registry can be passed directly wherever that interface is expected
// (e.g. credentials.Options.CredentialTypeSchemeProvider).
func (r *Registry) GetCredentialTypeScheme() *runtime.Scheme {
	return r.scheme
}
