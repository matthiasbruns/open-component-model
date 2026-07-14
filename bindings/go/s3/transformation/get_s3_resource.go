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

// GetS3Resource is a transformer that downloads a resource's content from S3 (dispatched by the
// resource's access type through the provided Repository) and buffers it to a file.
type GetS3Resource struct {
	Scheme             *runtime.Scheme
	Repository         repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *GetS3Resource) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.GetS3Resource
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get s3 resource transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for get s3 resource transformation")
	}
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.GetS3ResourceOutput{}
	}
	if transformation.Spec.Resource == nil {
		return nil, fmt.Errorf("resource is required")
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

	blobContent, err := t.Repository.DownloadResource(ctx, targetResource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed downloading s3 resource %v: %w", targetResource.ToIdentity(), err)
	}

	outputPath, err := DetermineOutputPath(transformation.Spec.OutputPath, "s3-resource")
	if err != nil {
		return nil, fmt.Errorf("failed determining output path: %w", err)
	}

	fileSpec, err := filesystem.BlobToSpec(blobContent, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering blob to file: %w", err)
	}

	v2Resource, err := descriptor.ConvertToV2Resource(t.Scheme, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	transformation.Output.File = *fileSpec
	transformation.Output.Resource = v2Resource

	return &transformation, nil
}
