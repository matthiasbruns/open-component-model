package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/s3/repository"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

func Test_Integration_S3(t *testing.T) {
	repo := repository.NewResourceRepository()

	spec := &accessv1.S3{URL: "https://example.com/artifact"}
	spec.Type = accessspec.V1VersionedType

	resource := &descriptor.Resource{}
	resource.Access = spec

	identity, err := repo.GetResourceCredentialConsumerIdentity(context.Background(), resource)
	require.NoError(t, err)
	require.NotNil(t, identity)

	// TODO: replace with a real end-to-end integration test using httptest.
}
