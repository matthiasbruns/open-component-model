package s3

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	s3repository "ocm.software/open-component-model/bindings/go/s3/repository"
	s3creds "ocm.software/open-component-model/bindings/go/s3/spec/credentials"
)

// Register wires the S3 access type and its credential scheme into the CLI plugin registries.
func Register(
	resourcePluginRegistry *resource.ResourceRegistry,
	digestProcessorRegistry *digestprocessor.RepositoryRegistry,
	credentialRepository *credentialrepository.RepositoryRegistry,
) error {
	credentialRepository.Register(s3creds.Scheme)

	s3ResourceRepository := s3repository.NewResourceRepository()
	if err := resourcePluginRegistry.RegisterInternalResourcePlugin(s3ResourceRepository); err != nil {
		return fmt.Errorf("could not register s3 resource repository plugin: %w", err)
	}
	if err := digestProcessorRegistry.RegisterInternalDigestProcessorPlugin(s3ResourceRepository); err != nil {
		return fmt.Errorf("could not register s3 digest processor plugin: %w", err)
	}

	return nil
}
