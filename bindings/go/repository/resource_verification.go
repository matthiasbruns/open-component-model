package repository

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/verification"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// ResourceVerifier binds downloaded content to an independently supplied expectation.
// Verification may be streaming: consumers must read to EOF and check errors before
// publishing or trusting the content.
type ResourceVerifier interface {
	// Verify takes ownership of content. On success the returned blob owns it;
	// on failure the verifier must release it if it implements io.Closer.
	Verify(context.Context, blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error)
}

// ResourceVerifierProvider validates and snapshots a resource's verification
// expectation. Resource repositories can optionally implement this interface to
// supply technology-specific verification; the plugin facade prefers it over its
// generic fallback. Selection errors must not trigger fallback verification.
// Call GetResourceVerifier before downloading the resource.
type ResourceVerifierProvider interface {
	GetResourceVerifier(context.Context, *descriptor.Resource) (ResourceVerifier, error)
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

type genericResourceVerifierProvider struct {
	policy DownloadVerificationPolicy
}

// NewGenericResourceVerifierProvider creates a provider for generic blob digests.
// Unsupported policies and normalization algorithms fail closed in GetResourceVerifier.
func NewGenericResourceVerifierProvider(policy DownloadVerificationPolicy) ResourceVerifierProvider {
	return &genericResourceVerifierProvider{policy: policy}
}

func (p *genericResourceVerifierProvider) GetResourceVerifier(ctx context.Context, res *descriptor.Resource) (ResourceVerifier, error) {
	if res == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if p.policy != VerifyIfPresent && p.policy != RequireDigest {
		return nil, fmt.Errorf("unsupported download verification policy %d", p.policy)
	}
	expected, err := parseDigest(res.Digest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse digest for resource %q: %w", res.Name, err)
	}
	if expected == "" {
		if p.policy == RequireDigest {
			return nil, fmt.Errorf("resource %q requires a digest", res.Name)
		}
		slog.WarnContext(ctx, "resource has no digest, no verification can be performed", slog.Any("resource", res.ToIdentity()))
	}
	return &genericResourceVerifier{expected: expected}, nil
}

type genericResourceVerifier struct {
	expected digest.Digest
}

func (v *genericResourceVerifier) Verify(_ context.Context, content blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	if v.expected == "" {
		return content, nil
	}
	wrapped, err := verification.Wrap(content, v.expected)
	if err != nil {
		return nil, handlerError(content, err)
	}
	return wrapped, nil
}
