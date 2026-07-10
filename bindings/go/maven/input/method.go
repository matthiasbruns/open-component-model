package input

import (
	"context"
	"fmt"
	"net/url"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the [constructor.ResourceInputMethod] interface for Maven inputs.
type InputMethod struct{}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for a
// Maven input from its URL.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	spec := v1.Maven{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if spec.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	if _, err := url.Parse(spec.URL); err != nil {
		return nil, fmt.Errorf("maven url is not a valid url: %w", err)
	}

	identity, err := runtime.ParseURLToIdentity(spec.URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing maven URL to identity: %w", err)
	}

	identity.SetType(runtime.NewUnversionedType(input.MavenConsumerType))

	return identity, nil
}

// ProcessResource processes the Maven input and returns it as local blob data.
//
// TODO: implement the actual input processing.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	spec := v1.Maven{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if spec.URL == "" {
		return nil, fmt.Errorf("url is required in maven input spec")
	}

	return nil, fmt.Errorf("ProcessResource is not implemented for the Maven input method")
}
