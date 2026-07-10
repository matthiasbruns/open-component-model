package repository

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// ResourceRepository implements the ResourceRepository interface for the S3 access type.
type ResourceRepository struct{}

// NewResourceRepository creates a new S3 resource repository.
func NewResourceRepository(opts ...Option) *ResourceRepository {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	// TODO: use options to configure the repository.
	return &ResourceRepository{}
}

// GetResourceRepositoryScheme returns the scheme used by the S3 resource repository.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return accessspec.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for the given resource.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	spec := v1.S3{}
	if err := r.GetResourceRepositoryScheme().Convert(resource.Access, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	if spec.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	identity, err := runtime.ParseURLToIdentity(spec.URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing s3 URL to identity: %w", err)
	}

	identity.SetType(runtime.NewUnversionedType(accessspec.S3ConsumerType))

	return identity, nil
}

// DownloadResource downloads a resource described by the S3 access spec.
//
// TODO: implement the actual download.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	spec := v1.S3{}
	if err := accessspec.Scheme.Convert(resource.Access, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	return nil, fmt.Errorf("DownloadResource is not implemented for the S3 access type")
}

// UploadResource is not supported for the S3 access type.
func (r *ResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("upload is not supported for the S3 access type")
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the credential consumer
// identity used when downloading the resource to compute its digest.
func (r *ResourceRepository) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return r.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// ProcessResourceDigest computes the digest of a S3 resource.
//
// TODO: implement digest processing.
func (r *ResourceRepository) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("ProcessResourceDigest is not implemented for the S3 access type")
}
