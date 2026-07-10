package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/maven/repository"
	accessspec "ocm.software/open-component-model/bindings/go/maven/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
)

func Test_Integration_Maven(t *testing.T) {
	repo := repository.NewResourceRepository()

	spec := &accessv1.Maven{URL: "https://example.com/artifact"}
	spec.Type = accessspec.V1VersionedType

	resource := &descriptor.Resource{}
	resource.Access = spec

	identity, err := repo.GetResourceCredentialConsumerIdentity(context.Background(), resource)
	require.NoError(t, err)
	require.NotNil(t, identity)

	// TODO: replace with a real end-to-end integration test using httptest.
}
