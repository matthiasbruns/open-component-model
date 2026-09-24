package builtin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/verify"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

type registrationBackend struct {
	repository.ResourceRepository
	scheme *runtime.Scheme
}

func (b *registrationBackend) GetResourceRepositoryScheme() *runtime.Scheme { return b.scheme }

func (*registrationBackend) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	return inmemory.New(strings.NewReader("tampered")), nil
}

type registrationProvider func(context.Context, *descriptor.Resource) (verify.ResourceVerifier, error)

func (f registrationProvider) GetResourceVerifier(ctx context.Context, res *descriptor.Resource) (verify.ResourceVerifier, error) {
	return f(ctx, res)
}

func TestRegisterEnablesResourceVerificationAtLookup(t *testing.T) {
	for _, specialized := range []bool{false, true} {
		name := "generic fallback"
		if specialized {
			name = "repository provider"
		}
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			pm := manager.NewPluginManager(ctx)
			typ := runtime.NewVersionedType("registration-test", "v1")
			scheme := runtime.NewScheme()
			scheme.MustRegisterWithAlias(&wgetv1.Wget{}, typ)
			var backend resource.BuiltinResourceRepository = &registrationBackend{scheme: scheme}
			providerErr := errors.New("repository verifier selected")
			providerCalls := 0
			res := &descriptor.Resource{
				Access: &wgetv1.Wget{Type: typ},
				Digest: &descriptor.Digest{
					HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1",
					Value: godigest.FromString("expected").Encoded(),
				},
			}
			if specialized {
				backend = &struct {
					resource.BuiltinResourceRepository
					verify.ResourceVerifierProvider
				}{backend, registrationProvider(func(gotCtx context.Context, got *descriptor.Resource) (verify.ResourceVerifier, error) {
					r.Equal(ctx, gotCtx)
					r.Same(res, got)
					providerCalls++
					return nil, providerErr
				})}
			}
			r.NoError(pm.ResourcePluginRegistry.RegisterInternalResourcePlugin(backend))
			raw, err := pm.ResourcePluginRegistry.GetResourcePlugin(ctx, res.Access)
			r.NoError(err)
			r.Same(backend, raw)
			content, err := raw.DownloadResource(ctx, res, nil)
			r.NoError(err)
			r.NoError(blob.Copy(io.Discard, content))
			r.Zero(providerCalls)

			r.NoError(Register(pm, &filesystemv1alpha1.Config{}, &httpv1alpha1.Config{}, slog.Default()))
			protected, err := pm.ResourcePluginRegistry.GetResourcePlugin(ctx, res.Access)
			r.NoError(err)
			content, err = protected.DownloadResource(ctx, res, nil)
			if specialized {
				r.ErrorIs(err, providerErr)
				r.Nil(content)
				r.Equal(1, providerCalls)
			} else {
				r.NoError(err)
				r.ErrorContains(blob.Copy(io.Discard, content), "digest mismatch")
			}
		})
	}
}
