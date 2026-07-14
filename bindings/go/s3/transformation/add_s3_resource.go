// Package transformation implements the S3 transformation steps executed by the transfer graph.
package transformation

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	v1alpha1 "ocm.software/open-component-model/bindings/go/s3/spec/transformation/v1alpha1"
)

// AddS3Resource is a transformer that uploads a resource's content to S3. The upload is dispatched
// by the resource's access type through the provided Repository (a resource plugin registry), so
// the S3 access produced from the coordinate at graph-build time routes to the S3 resource plugin.
type AddS3Resource struct {
	Scheme             *runtime.Scheme
	Repository         repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *AddS3Resource) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.AddS3Resource
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to add s3 resource transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for add s3 resource transformation")
	}
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.AddS3ResourceOutput{}
	}
	if transformation.Spec.Resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if transformation.Spec.File.URI == "" {
		return nil, fmt.Errorf("file is required")
	}

	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)

	var creds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerID, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, targetResource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerID); err != nil {
				if !errors.Is(err, credentials.ErrNotFound) {
					return nil, fmt.Errorf("failed resolving credentials: %w", err)
				}
			}
		}
	}

	blobContent, err := filesystem.GetBlobFromSpec(ctx, &transformation.Spec.File)
	if err != nil {
		return nil, fmt.Errorf("failed reading blob from file %s: %w", transformation.Spec.File.URI, err)
	}

	updatedResource, err := t.Repository.UploadResource(ctx, targetResource, blobContent, creds)
	if err != nil {
		return nil, fmt.Errorf("failed uploading s3 resource %v: %w", targetResource.ToIdentity(), err)
	}

	v2UpdatedResource, err := descriptor.ConvertToV2Resource(t.Scheme, updatedResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}
	transformation.Output.Resource = v2UpdatedResource

	return &transformation, nil
}
