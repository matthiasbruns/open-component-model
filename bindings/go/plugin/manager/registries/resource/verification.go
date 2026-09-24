package resource

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Option configures resource-plugin orchestration, independently of transport.
type Option func(*ResourceRegistry)

// WithFallbackResourceVerifierProvider configures verification for repositories
// that do not implement repository.ResourceVerifierProvider. The default fallback
// supports generic blobs and permits missing digests. It never overrides a
// repository-provided verifier or handles its errors. A nil fallback fails closed
// when the repository does not supply its own provider.
func WithFallbackResourceVerifierProvider(provider repository.ResourceVerifierProvider) Option {
	return func(r *ResourceRegistry) { r.fallbackVerifiers = provider }
}

type verifyingRepository struct {
	base      Repository
	verifiers repository.ResourceVerifierProvider
}

func newVerifyingRepository(base Repository, verifiers repository.ResourceVerifierProvider) Repository {
	if specialized, ok := base.(repository.ResourceVerifierProvider); ok {
		verifiers = specialized
	}
	verified := &verifyingRepository{base: base, verifiers: verifiers}
	ownership, hasOwnership := base.(repository.OwnershipAwareRepository)
	sbom, hasSBOM := base.(repository.SBOMDiscoverer)
	// Preserve optional capabilities without advertising ones the plugin lacks.
	switch {
	case hasOwnership && hasSBOM:
		return &struct {
			*verifyingRepository
			repository.OwnershipAwareRepository
			repository.SBOMDiscoverer
		}{verified, ownership, sbom}
	case hasOwnership:
		return &struct {
			*verifyingRepository
			repository.OwnershipAwareRepository
		}{verified, ownership}
	case hasSBOM:
		return &struct {
			*verifyingRepository
			repository.SBOMDiscoverer
		}{verified, sbom}
	default:
		return verified
	}
}

func (r *verifyingRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	return r.base.GetResourceCredentialConsumerIdentity(ctx, res)
}

func (r *verifyingRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return r.base.UploadResource(ctx, res, content, credentials)
}

func (r *verifyingRepository) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	if res == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if r.verifiers == nil {
		return nil, fmt.Errorf("resource verifier provider is required")
	}
	verifier, err := r.verifiers.GetResourceVerifier(ctx, res)
	if err != nil {
		return nil, fmt.Errorf("selecting verifier for resource %q: %w", res.Name, err)
	}
	if verifier == nil {
		return nil, fmt.Errorf("no verifier returned for resource %q", res.Name)
	}
	content, err := r.base.DownloadResource(ctx, res, credentials)
	if err != nil {
		return nil, err
	}
	return verifier.Verify(ctx, content)
}
