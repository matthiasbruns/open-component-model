package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

func Test_GetResourceCredentialConsumerIdentity(t *testing.T) {
	repo := NewResourceRepository()

	spec := &v1.S3{URL: "https://example.com/artifact"}
	spec.Type = accessspec.V1VersionedType

	resource := &descriptor.Resource{}
	resource.Access = spec

	identity, err := repo.GetResourceCredentialConsumerIdentity(context.Background(), resource)
	require.NoError(t, err)
	require.NotNil(t, identity)

	// nil resource must be rejected.
	_, err = repo.GetResourceCredentialConsumerIdentity(context.Background(), nil)
	require.Error(t, err)

	// TODO: add cases covering DownloadResource and ProcessResourceDigest once implemented.
}
