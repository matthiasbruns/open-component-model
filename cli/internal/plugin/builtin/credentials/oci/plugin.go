package oci

import (
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ociidentity "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtype"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(repoRegistry *credentialrepository.RepositoryRegistry, credTypeRegistry *credentialtype.Registry) error {
	credTypeRegistry.Register(ocicredentialsspec.CredentialTypeScheme)
	return repoRegistry.RegisterInternalCredentialRepositoryPlugin(
		&ocicredentials.OCICredentialRepository{},
		[]runtime.Type{ociidentity.Type},
	)
}
