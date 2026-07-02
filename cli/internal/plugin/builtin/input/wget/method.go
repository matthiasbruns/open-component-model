package wget

import (
	"fmt"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	wgetinput "ocm.software/open-component-model/bindings/go/wget/input"
	wgetcreds "ocm.software/open-component-model/bindings/go/wget/spec/credentials"
)

// Register wires the wget input method and its credential scheme into the CLI plugin registries.
func Register(inputRegistry *input.RepositoryRegistry,
	repositoryRegistry *credentialrepository.RepositoryRegistry,
	httpConfig *httpv1alpha1.Config,
) error {
	method := &wgetinput.InputMethod{
		HTTPConfig: httpConfig,
	}

	repositoryRegistry.Register(wgetcreds.Scheme)

	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register wget resource input method: %w", err)
	}

	return nil
}
