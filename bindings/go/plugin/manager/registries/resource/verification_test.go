package resource

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	resourcev1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type verificationBackend struct {
	download func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error)
	upload   func(context.Context, *descriptor.Resource, blob.ReadOnlyBlob, runtime.Typed) (*descriptor.Resource, error)
	identity func(context.Context, *descriptor.Resource) (runtime.Identity, error)
}

func (*verificationBackend) GetResourceRepositoryScheme() *runtime.Scheme { return dummytype.Scheme }
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
		Access: &runtime.Raw{Type: dummyType, Data: []byte(`{"type":"dummy/v1"}`)},
	}
}
func verificationLookup(t *testing.T, backend BuiltinResourceRepository, opts ...Option) Repository {
	t.Helper()
	r := require.New(t)
	registry := NewResourceRegistry(t.Context(), opts...)
	r.NoError(registry.RegisterInternalResourcePlugin(backend))
	plugin, err := registry.GetResourcePlugin(t.Context(), &runtime.Raw{Type: dummyType})
	r.NoError(err)
	return plugin
}

func TestRegistryVerificationDownload(t *testing.T) {
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
				opts = append(opts, WithResourceVerifierProvider(repository.NewGenericResourceVerifierProvider(repository.RequireDigest)))
			}
			plugin := verificationLookup(t, backend, opts...)
			content, err := plugin.DownloadResource(t.Context(), res, credentials)
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

type verificationProviderFunc func(context.Context, *descriptor.Resource) (repository.ResourceVerifier, error)

func (f verificationProviderFunc) GetResourceVerifier(ctx context.Context, res *descriptor.Resource) (repository.ResourceVerifier, error) {
	return f(ctx, res)
}

func TestRegistryVerificationRejectsBeforeTransport(t *testing.T) {
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
		{name: "strict missing", opts: []Option{WithResourceVerifierProvider(repository.NewGenericResourceVerifierProvider(repository.RequireDigest))}, message: "requires a digest"},
		{name: "strict excluded", digest: &descriptor.Digest{HashAlgorithm: descriptor.NoDigest, NormalisationAlgorithm: descriptor.ExcludeFromSignature}, opts: []Option{WithResourceVerifierProvider(repository.NewGenericResourceVerifierProvider(repository.RequireDigest))}, message: "requires a digest"},
		{name: "nil provider", opts: []Option{WithResourceVerifierProvider(nil)}, message: "provider is required"},
		{name: "nil verifier", opts: []Option{WithResourceVerifierProvider(verificationProviderFunc(func(context.Context, *descriptor.Resource) (repository.ResourceVerifier, error) { return nil, nil }))}, message: "no verifier returned"},
		{name: "provider error", opts: []Option{WithResourceVerifierProvider(verificationProviderFunc(func(context.Context, *descriptor.Resource) (repository.ResourceVerifier, error) {
			return nil, providerErr
		}))}, message: "provider failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			calls := 0
			plugin := verificationLookup(t, &verificationBackend{download: func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
				calls++
				return nil, errors.New("unexpected transport")
			}}, tt.opts...)
			content, err := plugin.DownloadResource(t.Context(), verificationResource(tt.digest), nil)
			r.ErrorContains(err, tt.message)
			if tt.name == "provider error" {
				r.ErrorIs(err, providerErr)
			}
			r.Nil(content)
			r.Zero(calls)
		})
	}
}

func TestRegistryVerificationSnapshot(t *testing.T) {
	for _, mutation := range []string{"modify", "replace", "remove"} {
		for _, payload := range []string{"expected", "tampered"} {
			t.Run(mutation+"/"+payload, func(t *testing.T) {
				r := require.New(t)
				res := verificationResource(verificationDigest("expected"))
				plugin := verificationLookup(t, &verificationBackend{download: func(_ context.Context, got *descriptor.Resource, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
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
				content, err := plugin.DownloadResource(t.Context(), res, nil)
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

func TestRegistryVerificationForwarding(t *testing.T) {
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
	plugin := verificationLookup(t, backend, WithResourceVerifierProvider(repository.NewGenericResourceVerifierProvider(repository.RequireDigest)))
	gotIdentity, err := plugin.GetResourceCredentialConsumerIdentity(ctx, res)
	r.Equal(identity, gotIdentity)
	r.ErrorIs(err, backendErr)
	gotResource, err := plugin.UploadResource(ctx, res, payload, credentials)
	r.Same(updated, gotResource)
	r.ErrorIs(err, backendErr)
	res.Digest = verificationDigest("expected")
	content, err := plugin.DownloadResource(ctx, res, credentials)
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

func TestRegistryVerificationOptionalCapabilities(t *testing.T) {
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
			var backend BuiltinResourceRepository = &verificationBackend{download: func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
				return inmemory.New(strings.NewReader("tampered")), nil
			}}
			switch {
			case tt.ownership && tt.sbom:
				backend = &struct {
					BuiltinResourceRepository
					repository.OwnershipAwareRepository
					repository.SBOMDiscoverer
				}{backend, ownership, sbom}
			case tt.ownership:
				backend = &struct {
					BuiltinResourceRepository
					repository.OwnershipAwareRepository
				}{backend, ownership}
			case tt.sbom:
				backend = &struct {
					BuiltinResourceRepository
					repository.SBOMDiscoverer
				}{backend, sbom}
			}
			plugin := verificationLookup(t, backend)
			owner, hasOwnership := plugin.(repository.OwnershipAwareRepository)
			discoverer, hasSBOM := plugin.(repository.SBOMDiscoverer)
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
			content, err := plugin.DownloadResource(ctx, res, credentials)
			r.NoError(err)
			var dst bytes.Buffer
			r.Error(blob.Copy(&dst, content), "capability preservation must not bypass verification")
		})
	}
}

type verificationExternalPlugin struct {
	resourcev1.ReadWriteResourcePluginContract
	download func(context.Context, *resourcev1.GetGlobalResourceRequest, runtime.Typed) (*resourcev1.GetGlobalResourceResponse, error)
}

func (p *verificationExternalPlugin) GetGlobalResource(ctx context.Context, request *resourcev1.GetGlobalResourceRequest, credentials runtime.Typed) (*resourcev1.GetGlobalResourceResponse, error) {
	return p.download(ctx, request, credentials)
}

func TestRegistryVerificationExternal(t *testing.T) {
	for _, tt := range []struct {
		name, payload string
		invalid       bool
	}{
		{name: "matching", payload: "expected"}, {name: "host rejects mismatch", payload: "tampered"}, {name: "invalid before transport", payload: "expected", invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "resource")
			r.NoError(os.WriteFile(path, []byte(tt.payload), 0o600))
			res := verificationResource(verificationDigest("expected"))
			if tt.invalid {
				res.Digest.NormalisationAlgorithm = "unknown/v1"
			}
			credentials := &runtime.Raw{Type: runtime.NewUnversionedType("credentials")}
			calls := 0
			external := &verificationExternalPlugin{download: func(gotCtx context.Context, request *resourcev1.GetGlobalResourceRequest, creds runtime.Typed) (*resourcev1.GetGlobalResourceResponse, error) {
				calls++
				r.Equal(ctx, gotCtx)
				r.Same(credentials, creds)
				r.Equal(res.Name, request.Resource.Name)
				r.Equal(res.Digest.Value, request.Resource.Digest.Value)
				// The external contract deliberately does no verification: the host must enforce the descriptor digest.
				return &resourcev1.GetGlobalResourceResponse{Location: types.Location{LocationType: types.LocationTypeLocalFile, Value: path}}, nil
			}}
			registry := NewResourceRegistry(ctx)
			capability := dummyCapability([]byte(`{}`))
			r.NoError(registry.AddPlugin(types.Plugin{ID: "verification-external"}, &capability))
			registry.constructedPlugins["verification-external"] = &constructedPlugin{Plugin: external}
			plugin, err := registry.GetResourcePlugin(ctx, res.Access)
			r.NoError(err)
			content, err := plugin.DownloadResource(ctx, res, credentials)
			if tt.invalid {
				r.ErrorContains(err, "unsupported normalisation")
				r.Nil(content)
				r.Zero(calls)
				return
			}
			r.NoError(err)
			r.Equal(1, calls)
			var dst bytes.Buffer
			err = blob.Copy(&dst, content)
			if tt.payload == "expected" {
				r.NoError(err)
				r.Equal(tt.payload, dst.String())
			} else {
				r.Error(err)
			}
		})
	}
}
