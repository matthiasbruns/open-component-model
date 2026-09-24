package verify

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type verificationBackend struct {
	download func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error)
	upload   func(context.Context, *descriptor.Resource, blob.ReadOnlyBlob, runtime.Typed) (*descriptor.Resource, error)
	identity func(context.Context, *descriptor.Resource) (runtime.Identity, error)
}

func (b *verificationBackend) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	return b.download(ctx, res, credentials)
}
func (b *verificationBackend) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return b.upload(ctx, res, content, credentials)
}
func (b *verificationBackend) GetResourceCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	return b.identity(ctx, res)
}

func verificationDigest(content string) *descriptor.Digest {
	return &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: godigest.FromString(content).Encoded()}
}
func verificationResource(digest *descriptor.Digest) *descriptor.Resource {
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "verified-resource", Version: "1.0.0"}},
		Type:        "blob", Relation: "external", Digest: digest,
	}
}
func TestResourceRepositoryDownload(t *testing.T) {
	for _, tt := range []struct {
		name, content    string
		digest           *descriptor.Digest
		strict, mismatch bool
	}{
		{name: "matching", content: "expected", digest: verificationDigest("expected")},
		{name: "mismatch", content: "tampered", digest: verificationDigest("expected"), mismatch: true},
		{name: "missing permissive", content: "unsigned"},
		{name: "empty permissive", content: "unsigned", digest: &descriptor.Digest{}},
		{name: "strict matching", content: "expected", digest: verificationDigest("expected"), strict: true},
		{name: "strict mismatch", content: "tampered", digest: verificationDigest("expected"), strict: true, mismatch: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			res := verificationResource(tt.digest)
			credentials := &runtime.Raw{Type: runtime.NewUnversionedType("credentials"), Data: []byte(`{"token":"secret"}`)}
			calls := 0
			backend := &verificationBackend{download: func(ctx context.Context, got *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, error) {
				calls++
				r.Equal(t.Context(), ctx)
				r.Same(res, got)
				r.Same(credentials, creds)
				return inmemory.New(strings.NewReader(tt.content)), nil
			}}
			var opts []Option
			if tt.strict {
				opts = append(opts, WithFallbackResourceVerifierProvider(NewGenericResourceVerifierProvider(RequireDigest)))
			}
			wrapped := NewResourceRepository(backend, opts...)
			content, err := wrapped.DownloadResource(t.Context(), res, credentials)
			r.NoError(err)
			var dst bytes.Buffer
			err = blob.Copy(&dst, content)
			if tt.mismatch {
				r.Error(err)
			} else {
				r.NoError(err)
				r.Equal(tt.content, dst.String())
			}
			r.Equal(1, calls)
		})
	}
}

type verificationProviderFunc func(context.Context, *descriptor.Resource) (ResourceVerifier, error)

func (f verificationProviderFunc) GetResourceVerifier(ctx context.Context, res *descriptor.Resource) (ResourceVerifier, error) {
	return f(ctx, res)
}

type verificationVerifierFunc func(context.Context, blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error)

func (f verificationVerifierFunc) Verify(ctx context.Context, content blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	return f(ctx, content)
}

func TestResourceRepositoryProviderPrecedence(t *testing.T) {
	providerErr := errors.New("repository provider failed")
	verifyErr := errors.New("repository verification failed")
	for _, tt := range []struct {
		name    string
		wantErr error
		steps   []string
	}{
		{name: "success", steps: []string{"prepare", "download", "verify"}},
		{name: "provider error", wantErr: providerErr, steps: []string{"prepare"}},
		{name: "nil verifier", steps: []string{"prepare"}},
		{name: "verify error", wantErr: verifyErr, steps: []string{"prepare", "download", "verify"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			res := verificationResource(verificationDigest("expected"))
			payload := inmemory.New(strings.NewReader("expected"))
			var steps []string
			backend := &struct {
				*verificationBackend
				ResourceVerifierProvider
			}{
				verificationBackend: &verificationBackend{download: func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
					steps = append(steps, "download")
					return payload, nil
				}},
				ResourceVerifierProvider: verificationProviderFunc(func(ctx context.Context, got *descriptor.Resource) (ResourceVerifier, error) {
					r.Equal(t.Context(), ctx)
					r.Same(res, got)
					steps = append(steps, "prepare")
					switch tt.name {
					case "provider error":
						return nil, providerErr
					case "nil verifier":
						return nil, nil
					default:
						return verificationVerifierFunc(func(ctx context.Context, content blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
							r.Equal(t.Context(), ctx)
							r.Same(payload, content)
							steps = append(steps, "verify")
							if tt.name == "success" {
								return content, nil
							}
							return nil, verifyErr
						}), nil
					}
				}),
			}
			fallbackCalls := 0
			fallback := verificationProviderFunc(func(context.Context, *descriptor.Resource) (ResourceVerifier, error) {
				fallbackCalls++
				return nil, errors.New("unexpected fallback")
			})
			wrapped := NewResourceRepository(backend, WithFallbackResourceVerifierProvider(fallback))
			content, err := wrapped.DownloadResource(t.Context(), res, nil)
			switch {
			case tt.name == "success":
				r.NoError(err)
				r.Same(payload, content)
			case tt.wantErr != nil:
				r.ErrorIs(err, tt.wantErr)
				r.Nil(content)
			default:
				r.ErrorContains(err, "no verifier returned")
				r.Nil(content)
			}
			r.Equal(tt.steps, steps)
			r.Zero(fallbackCalls)
		})
	}
}

func TestResourceRepositoryFallbackWithoutRepositoryProvider(t *testing.T) {
	r := require.New(t)
	res := verificationResource(verificationDigest("expected"))
	fallbackErr := errors.New("fallback selected")
	fallbackCalls, downloadCalls := 0, 0
	backend := &verificationBackend{download: func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
		downloadCalls++
		return nil, errors.New("unexpected transport")
	}}
	fallback := verificationProviderFunc(func(ctx context.Context, got *descriptor.Resource) (ResourceVerifier, error) {
		r.Equal(t.Context(), ctx)
		r.Same(res, got)
		fallbackCalls++
		return nil, fallbackErr
	})
	wrapped := NewResourceRepository(backend, WithFallbackResourceVerifierProvider(fallback))
	content, err := wrapped.DownloadResource(t.Context(), res, nil)
	r.ErrorIs(err, fallbackErr)
	r.Nil(content)
	r.Equal(1, fallbackCalls)
	r.Zero(downloadCalls)
}

func TestResourceRepositoryRejectsBeforeTransport(t *testing.T) {
	providerErr := errors.New("provider failed")
	for _, tt := range []struct {
		name    string
		digest  *descriptor.Digest
		opts    []Option
		message string
	}{
		{name: "malformed", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: "not-hex"}, message: "invalid digest"},
		{name: "incomplete", digest: &descriptor.Digest{HashAlgorithm: "SHA-256"}, message: "incomplete digest"},
		{name: "unknown normalization", digest: &descriptor.Digest{HashAlgorithm: "SHA-256", Value: verificationDigest("expected").Value, NormalisationAlgorithm: "unknown/v1"}, message: "unsupported normalisation"},
		{name: "strict missing", opts: []Option{WithFallbackResourceVerifierProvider(NewGenericResourceVerifierProvider(RequireDigest))}, message: "requires a digest"},
		{name: "strict excluded", digest: &descriptor.Digest{HashAlgorithm: descriptor.NoDigest, NormalisationAlgorithm: descriptor.ExcludeFromSignature}, opts: []Option{WithFallbackResourceVerifierProvider(NewGenericResourceVerifierProvider(RequireDigest))}, message: "requires a digest"},
		{name: "nil provider", opts: []Option{WithFallbackResourceVerifierProvider(nil)}, message: "provider is required"},
		{name: "nil verifier", opts: []Option{WithFallbackResourceVerifierProvider(verificationProviderFunc(func(context.Context, *descriptor.Resource) (ResourceVerifier, error) { return nil, nil }))}, message: "no verifier returned"},
		{name: "provider error", opts: []Option{WithFallbackResourceVerifierProvider(verificationProviderFunc(func(context.Context, *descriptor.Resource) (ResourceVerifier, error) {
			return nil, providerErr
		}))}, message: "provider failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			calls := 0
			wrapped := NewResourceRepository(&verificationBackend{download: func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
				calls++
				return nil, errors.New("unexpected transport")
			}}, tt.opts...)
			content, err := wrapped.DownloadResource(t.Context(), verificationResource(tt.digest), nil)
			r.ErrorContains(err, tt.message)
			if tt.name == "provider error" {
				r.ErrorIs(err, providerErr)
			}
			r.Nil(content)
			r.Zero(calls)
		})
	}
}

func TestResourceRepositorySnapshot(t *testing.T) {
	for _, mutation := range []string{"modify", "replace", "remove"} {
		for _, payload := range []string{"expected", "tampered"} {
			t.Run(mutation+"/"+payload, func(t *testing.T) {
				r := require.New(t)
				res := verificationResource(verificationDigest("expected"))
				wrapped := NewResourceRepository(&verificationBackend{download: func(_ context.Context, got *descriptor.Resource, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
					r.Same(res, got)
					switch mutation {
					case "modify":
						got.Digest.Value = verificationDigest("tampered").Value
					case "replace":
						got.Digest = verificationDigest("tampered")
					case "remove":
						got.Digest = nil
					}
					return inmemory.New(strings.NewReader(payload)), nil
				}})
				content, err := wrapped.DownloadResource(t.Context(), res, nil)
				r.NoError(err)
				var dst bytes.Buffer
				err = blob.Copy(&dst, content)
				if payload == "expected" {
					r.NoError(err)
					r.Equal(payload, dst.String())
				} else {
					r.Error(err)
				}
			})
		}
	}
}

func TestResourceRepositoryForwarding(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	res := verificationResource(nil)
	credentials := &runtime.Raw{Type: runtime.NewUnversionedType("credentials")}
	payload := inmemory.New(strings.NewReader("upload"))
	updated := verificationResource(verificationDigest("upload"))
	identity := runtime.Identity{"hostname": "registry.example", "path": "private"}
	backendErr := errors.New("backend unavailable")
	var identityCalls, uploadCalls, downloadCalls int
	backend := &verificationBackend{
		identity: func(gotCtx context.Context, got *descriptor.Resource) (runtime.Identity, error) {
			identityCalls++
			r.Equal(ctx, gotCtx)
			r.Same(res, got)
			return identity, backendErr
		},
		upload: func(gotCtx context.Context, got *descriptor.Resource, content blob.ReadOnlyBlob, creds runtime.Typed) (*descriptor.Resource, error) {
			uploadCalls++
			r.Equal(ctx, gotCtx)
			r.Same(res, got)
			r.Same(payload, content)
			r.Same(credentials, creds)
			return updated, backendErr
		},
		download: func(gotCtx context.Context, got *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, error) {
			downloadCalls++
			r.Equal(ctx, gotCtx)
			r.Same(res, got)
			r.Same(credentials, creds)
			return nil, backendErr
		},
	}
	// Strict download policy must not interfere with identity resolution or uploads.
	wrapped := NewResourceRepository(backend, WithFallbackResourceVerifierProvider(NewGenericResourceVerifierProvider(RequireDigest)))
	gotIdentity, err := wrapped.GetResourceCredentialConsumerIdentity(ctx, res)
	r.Equal(identity, gotIdentity)
	r.ErrorIs(err, backendErr)
	gotResource, err := wrapped.UploadResource(ctx, res, payload, credentials)
	r.Same(updated, gotResource)
	r.ErrorIs(err, backendErr)
	res.Digest = verificationDigest("expected")
	content, err := wrapped.DownloadResource(ctx, res, credentials)
	r.Nil(content)
	r.ErrorIs(err, backendErr)
	r.Equal(1, identityCalls)
	r.Equal(1, uploadCalls)
	r.Equal(1, downloadCalls)
}

type verificationOwnershipFunc func(context.Context, string, string, *descriptor.Resource, runtime.Typed) error

func (f verificationOwnershipFunc) AddOwnership(ctx context.Context, component, version string, res *descriptor.Resource, credentials runtime.Typed) error {
	return f(ctx, component, version, res, credentials)
}

type verificationSBOMFunc func(context.Context, *descriptor.Resource, runtime.Typed, ...repository.SBOMOption) ([]repository.SBOM, error)

func (f verificationSBOMFunc) DiscoverSBOM(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed, opts ...repository.SBOMOption) ([]repository.SBOM, error) {
	return f(ctx, res, credentials, opts...)
}

func TestResourceRepositoryOptionalCapabilities(t *testing.T) {
	for _, tt := range []struct {
		name            string
		ownership, sbom bool
	}{
		{name: "neither"}, {name: "ownership", ownership: true}, {name: "sbom", sbom: true}, {name: "both", ownership: true, sbom: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			res := verificationResource(verificationDigest("expected"))
			credentials := &runtime.Raw{Type: runtime.NewUnversionedType("credentials")}
			backendErr := errors.New("capability error")
			ownershipCalls, sbomCalls := 0, 0
			ownership := verificationOwnershipFunc(func(gotCtx context.Context, component, version string, got *descriptor.Resource, creds runtime.Typed) error {
				ownershipCalls++
				r.Equal(ctx, gotCtx)
				r.Equal("example/component", component)
				r.Equal("1.2.3", version)
				r.Same(res, got)
				r.Same(credentials, creds)
				return backendErr
			})
			sboms := []repository.SBOM{{ID: "document", Data: []byte(`{"bomFormat":"CycloneDX"}`)}}
			sbom := verificationSBOMFunc(func(gotCtx context.Context, got *descriptor.Resource, creds runtime.Typed, opts ...repository.SBOMOption) ([]repository.SBOM, error) {
				sbomCalls++
				r.Equal(ctx, gotCtx)
				r.Same(res, got)
				r.Same(credentials, creds)
				options := repository.NewSBOMOptions(opts...)
				r.True(options.AllPlatforms)
				r.Equal([]string{repository.PredicateTypeCycloneDX}, options.PredicateTypes)
				return sboms, backendErr
			})
			var backend repository.ResourceRepository = &verificationBackend{download: func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
				return inmemory.New(strings.NewReader("tampered")), nil
			}}
			switch {
			case tt.ownership && tt.sbom:
				backend = &struct {
					repository.ResourceRepository
					repository.OwnershipAwareRepository
					repository.SBOMDiscoverer
				}{backend, ownership, sbom}
			case tt.ownership:
				backend = &struct {
					repository.ResourceRepository
					repository.OwnershipAwareRepository
				}{backend, ownership}
			case tt.sbom:
				backend = &struct {
					repository.ResourceRepository
					repository.SBOMDiscoverer
				}{backend, sbom}
			}
			wrapped := NewResourceRepository(backend)
			owner, hasOwnership := wrapped.(repository.OwnershipAwareRepository)
			discoverer, hasSBOM := wrapped.(repository.SBOMDiscoverer)
			r.Equal(tt.ownership, hasOwnership)
			r.Equal(tt.sbom, hasSBOM)
			if hasOwnership {
				r.ErrorIs(owner.AddOwnership(ctx, "example/component", "1.2.3", res, credentials), backendErr)
				r.Equal(1, ownershipCalls)
			}
			if hasSBOM {
				got, err := discoverer.DiscoverSBOM(ctx, res, credentials, repository.WithAllSBOMPlatforms(), repository.WithSBOMPredicateTypes(repository.PredicateTypeCycloneDX))
				r.Equal(sboms, got)
				r.ErrorIs(err, backendErr)
				r.Equal(1, sbomCalls)
			}
			content, err := wrapped.DownloadResource(ctx, res, credentials)
			r.NoError(err)
			var dst bytes.Buffer
			r.Error(blob.Copy(&dst, content), "capability preservation must not bypass verification")
		})
	}
}
