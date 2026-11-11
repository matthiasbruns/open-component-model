// Package digest provides the digest processor for the GitHub access type.
// It downloads the repository tree at the pinned commit (via the github
// resource repository) and computes a generic blob digest over the resulting
// tar archive, so that by-reference github resources carry the same digest
// they would have as an embedded local blob.
package digest

import (
	"context"
	"crypto"
	"fmt"
	"log/slog"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmdigest "ocm.software/open-component-model/bindings/go/digest"
	githubinternal "ocm.software/open-component-model/bindings/go/github/internal"
	"ocm.software/open-component-model/bindings/go/github/repository/resource"
	"ocm.software/open-component-model/bindings/go/github/spec/access"
	ghcreds "ocm.software/open-component-model/bindings/go/github/spec/credentials"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ digestprocessor.BuiltinDigestProcessorPlugin = (*DigestProcessor)(nil)

// DigestProcessor resolves digests for GitHub access types by downloading the
// pinned commit's tar archive and hashing it.
type DigestProcessor struct {
	resourceRepository *resource.ResourceRepository
}

// NewDigestProcessor creates a new GitHub digest processor. opts are forwarded
// to the underlying resource repository, whose HTTP client performs both the
// ref resolution and the archive download; the archive's digest is computed
// while it is buffered in memory, not by a second read.
func NewDigestProcessor(opts ...resource.Option) *DigestProcessor {
	return &DigestProcessor{
		resourceRepository: resource.NewResourceRepository(opts...),
	}
}

func (p *DigestProcessor) GetResourceRepositoryScheme() *runtime.Scheme {
	return access.Scheme
}

// GetResourceDigestProcessorCredentialConsumerIdentity resolves the
// credential consumer identity (type GitHubRepository) for digest processing.
func (p *DigestProcessor) GetResourceDigestProcessorCredentialConsumerIdentity(
	ctx context.Context, resource *descriptor.Resource,
) (runtime.Identity, error) {
	return p.resourceRepository.GetResourceCredentialConsumerIdentity(ctx, resource)
}

// ProcessResourceDigest pins a ref-only github access to the commit the ref
// currently resolves to, then downloads the archive at the pinned commit and
// applies or verifies its generic blob digest. The returned resource carries
// both the pinned access and the digest; the input resource is never mutated.
func (p *DigestProcessor) ProcessResourceDigest(
	ctx context.Context, res *descriptor.Resource, credentials runtime.Typed,
) (*descriptor.Resource, error) {
	gitHub, err := githubinternal.AccessFrom(res.Access)
	if err != nil {
		return nil, err
	}
	if err := gitHub.Validate(); err != nil {
		return nil, fmt.Errorf("invalid github access: %w", err)
	}
	if ocmdigest.IsExcluded(res.Digest) {
		return res.DeepCopy(), nil
	}

	// Everything below works on a copy; the caller's resource is never touched.
	res = res.DeepCopy()

	// A set Commit is authoritative and Ref is informational, so only an
	// unpinned access resolves its ref — mirroring OCI tag->digest pinning.
	// Re-resolving an already pinned commit would make the digest depend on
	// where the ref points today, and every branch that moves past the pinned
	// commit (or is deleted after a merge) would break verification of a
	// component version that has not changed.
	gitHubCredentials, err := credsv1.ConvertToGitHubCredentials(credentials)
	if err != nil {
		return nil, fmt.Errorf("error resolving github credentials: %w", err)
	}
	if gitHub.Commit == "" {
		resolved, err := p.resourceRepository.ResolveCommit(ctx, gitHub, gitHubCredentials)
		if err != nil {
			return nil, fmt.Errorf("error resolving github ref to commit: %w", err)
		}
		gitHub.Commit = resolved

		// Hand the pinned access back in the raw form a constructor parsed.
		pinned := &runtime.Raw{}
		if err := access.Scheme.Convert(gitHub, pinned); err != nil {
			return nil, fmt.Errorf("error encoding pinned github access: %w", err)
		}
		res.Access = pinned
	}

	// An algorithm the author pinned can never match what is computed here, so the archive is
	// not fetched to find that out. The value is settled after the download.
	if err := ocmdigest.CheckPinned(res.Digest, crypto.SHA256, ocmdigest.BlobRule); err != nil {
		return nil, err
	}

	// The full archive is fetched only to be hashed and thrown away. That cost
	// is easy to miss from the outside, so it is called out loudly rather than
	// hidden in debug logs.
	slog.WarnContext(ctx, "computing the digest of a github resource downloads the full commit archive and discards it after hashing",
		"repoUrl", gitHub.RepoURL, "commit", gitHub.Commit)

	// The archive is downloaded without a resource digest to check against: this is
	// where that digest is computed, and it is compared below.
	downloaded, err := p.resourceRepository.DownloadArchive(ctx, gitHub, gitHubCredentials)
	if err != nil {
		return nil, fmt.Errorf("error downloading github resource for digest processing: %w", err)
	}
	var raw string
	if aware, ok := downloaded.(blob.DigestAware); ok {
		raw, _ = aware.Digest()
	}
	resolvedDigest, err := godigest.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("downloaded github archive blob does not carry a valid digest: %w", err)
	}
	// The digest is still author input here, so the shared rules complete what a constructor
	// entry left out and report only a genuine conflict.
	if res.Digest, err = ocmdigest.Complete(res.Digest,
		ocmdigest.Computed{Hash: crypto.SHA256, Value: resolvedDigest.Encoded()}, ocmdigest.BlobRule); err != nil {
		return nil, err
	}

	return res, nil
}

func (p *DigestProcessor) GetCredentialTypeScheme() *runtime.Scheme {
	return ghcreds.Scheme
}
