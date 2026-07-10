package input

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	inputspec "ocm.software/open-component-model/bindings/go/maven/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/maven/spec/input/v1"
)

func Test_GetResourceCredentialConsumerIdentity(t *testing.T) {
	method := &InputMethod{}

	spec := &v1.Maven{URL: "https://example.com/artifact"}
	spec.Type = inputspec.V1VersionedType

	resource := &constructorruntime.Resource{}
	resource.Input = spec

	identity, err := method.GetResourceCredentialConsumerIdentity(context.Background(), resource)
	require.NoError(t, err)
	require.NotNil(t, identity)

	// TODO: add cases covering ProcessResource once implemented.
}
