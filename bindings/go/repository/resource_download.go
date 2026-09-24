package repository

import (
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/verification"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceFetcher implements transport only. It deliberately does not satisfy
// ResourceRepository: consumer-facing downloads belong to the verification facade.
type ResourceFetcher interface {
	FetchResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error)
}

// ResourceBackend is the transport implementation behind a resource repository.
type ResourceBackend interface {
	ResourceFetcher
	GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error)
	UploadResource(context.Context, *descriptor.Resource, blob.ReadOnlyBlob, runtime.Typed) (*descriptor.Resource, error)
}

// DownloadVerificationPolicy controls whether resources without usable digests
// (including explicit signature exclusions) may be downloaded.
type DownloadVerificationPolicy uint8

const (
	// VerifyIfPresent preserves support for unsigned resources. It never labels them verified.
	VerifyIfPresent DownloadVerificationPolicy = iota
	// RequireDigest rejects absent digests and explicit exclusions before fetching.
	RequireDigest
)

type verifiedResourceRepository struct {
	backend ResourceBackend
	policy  DownloadVerificationPolicy
}

// NewVerifiedResourceRepository exposes streaming-verified downloads over a raw
// backend. Missing digests are allowed for compatibility; use
// NewResourceRepositoryWithVerification to require a digest. A successful return
// from DownloadResource does not mean verification has completed: consumers must
// read to EOF and check errors before publishing or trusting the content.
func NewVerifiedResourceRepository(backend ResourceBackend) ResourceRepository {
	return NewResourceRepositoryWithVerification(backend, VerifyIfPresent)
}

// NewResourceRepositoryWithVerification fixes verification policy at construction.
// Unsupported policies fail closed when downloading.
func NewResourceRepositoryWithVerification(backend ResourceBackend, policy DownloadVerificationPolicy) ResourceRepository {
	return &verifiedResourceRepository{backend: backend, policy: policy}
}

func (r *verifiedResourceRepository) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	if res == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if r.policy != VerifyIfPresent && r.policy != RequireDigest {
		return nil, fmt.Errorf("unsupported download verification policy %d", r.policy)
	}
	// Snapshot the expectation before handing the resource to transport code.
	expected, err := parseDigest(res.Digest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse digest for resource %q: %w", res.Name, err)
	}
	if expected == "" && r.policy == RequireDigest {
		return nil, fmt.Errorf("resource %q requires a digest", res.Name)
	}
	content, err := r.backend.FetchResource(ctx, res, credentials)
	if err != nil {
		return nil, err
	}
	if expected == "" {
		slog.WarnContext(ctx, "resource has no digest, no verification can be performed", slog.Any("resource", res.ToIdentity()))
		return content, nil
	}
	wrapped, err := verification.Wrap(content, expected)
	if err != nil {
		return nil, handlerError(content, err)
	}
	return wrapped, nil
}

func (r *verifiedResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	return r.backend.GetResourceCredentialConsumerIdentity(ctx, res)
}

func (r *verifiedResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return r.backend.UploadResource(ctx, res, content, credentials)
}
