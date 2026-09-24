package repository

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
	"ocm.software/open-component-model/bindings/go/runtime"
)

type resourceBackendStub struct {
	fetch    func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error)
	upload   func(context.Context, *descriptor.Resource, blob.ReadOnlyBlob, runtime.Typed) (*descriptor.Resource, error)
	identity func(context.Context, *descriptor.Resource) (runtime.Identity, error)
}

func (b *resourceBackendStub) FetchResource(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	return b.fetch(ctx, res, credentials)
}

func (b *resourceBackendStub) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return b.upload(ctx, res, content, credentials)
}

func (b *resourceBackendStub) GetResourceCredentialConsumerIdentity(ctx context.Context, res *descriptor.Resource) (runtime.Identity, error) {
	return b.identity(ctx, res)
}

func downloadDigest(content string) *descriptor.Digest {
	return &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  godigest.FromString(content).Encoded(),
	}
}

func TestResourceRepositoryDownloadVerification(t *testing.T) {
	for _, policy := range []DownloadVerificationPolicy{VerifyIfPresent, RequireDigest} {
		for _, tt := range []struct {
			name          string
			content       string
			normalization string
			mismatch      bool
		}{
			{name: "matching", content: verifyContent, normalization: "genericBlobDigest/v1"},
			{name: "legacy empty normalization", content: verifyContent},
			{name: "mismatch", content: "different content", normalization: "genericBlobDigest/v1", mismatch: true},
		} {
			name := tt.name
			if policy == RequireDigest {
				name += " strict"
			}
			t.Run(name, func(t *testing.T) {
				r := require.New(t)
				ctx := t.Context()
				dig := downloadDigest(verifyContent)
				dig.NormalisationAlgorithm = tt.normalization
				res := resourceWithDigest(dig)
				credentials := &runtime.Raw{}
				calls := 0
				backend := &resourceBackendStub{fetch: func(gotCtx context.Context, gotRes *descriptor.Resource, gotCredentials runtime.Typed) (blob.ReadOnlyBlob, error) {
					calls++
					r.Equal(ctx, gotCtx)
					r.Same(res, gotRes)
					r.Same(credentials, gotCredentials)
					return inmemory.New(strings.NewReader(tt.content)), nil
				}}
				repo := NewVerifiedResourceRepository(backend)
				if policy == RequireDigest {
					repo = NewResourceRepositoryWithVerification(backend, policy)
				}
				content, err := repo.DownloadResource(ctx, res, credentials)
				r.NoError(err, "verification is deferred until content is consumed")
				r.Equal(1, calls)
				var dst bytes.Buffer
				err = blob.Copy(&dst, content)
				if tt.mismatch {
					r.ErrorContains(err, "digest mismatch")
				} else {
					r.NoError(err)
					r.Equal(tt.content, dst.String())
				}
			})
		}
	}
}

func TestResourceRepositoryRejectsBeforeFetch(t *testing.T) {
	value := godigest.FromString(verifyContent).Encoded()
	for _, tt := range []struct {
		name    string
		res     *descriptor.Resource
		policy  DownloadVerificationPolicy
		message string
	}{
		{name: "nil resource", message: "resource is required"},
		{name: "unknown policy", res: resourceWithDigest(downloadDigest(verifyContent)), policy: DownloadVerificationPolicy(255), message: "unsupported download verification policy"},
		{name: "invalid hex", res: resourceWithDigest(&descriptor.Digest{HashAlgorithm: "SHA-256", Value: strings.Repeat("z", 64)}), message: "invalid digest"},
		{name: "wrong length", res: resourceWithDigest(&descriptor.Digest{HashAlgorithm: "SHA-256", Value: "abcd"}), message: "invalid digest"},
		{name: "missing value", res: resourceWithDigest(&descriptor.Digest{HashAlgorithm: "SHA-256"}), message: "incomplete digest"},
		{name: "missing algorithm", res: resourceWithDigest(&descriptor.Digest{Value: value}), message: "incomplete digest"},
		{name: "unsupported hash", res: resourceWithDigest(&descriptor.Digest{HashAlgorithm: "MD5", Value: value}), message: "unsupported hash algorithm"},
		{name: "unsupported normalization", res: resourceWithDigest(&descriptor.Digest{HashAlgorithm: "SHA-256", Value: value, NormalisationAlgorithm: "ociArtifactDigest/v1"}), message: "unsupported normalisation algorithm"},
		{name: "normalization without hash or value", res: resourceWithDigest(&descriptor.Digest{NormalisationAlgorithm: "unknown/v1"}), message: "unsupported normalisation algorithm"},
		{name: "strict nil digest", res: resourceWithDigest(nil), policy: RequireDigest, message: "requires a digest"},
		{name: "strict empty digest", res: resourceWithDigest(&descriptor.Digest{}), policy: RequireDigest, message: "requires a digest"},
		{name: "strict exclusion", res: resourceWithDigest(&descriptor.Digest{HashAlgorithm: descriptor.NoDigest, NormalisationAlgorithm: descriptor.ExcludeFromSignature, Value: descriptor.NoDigest}), policy: RequireDigest, message: "requires a digest"},
		{name: "strict normalization exclusion", res: resourceWithDigest(&descriptor.Digest{HashAlgorithm: "SHA-256", Value: value, NormalisationAlgorithm: descriptor.ExcludeFromSignature}), policy: RequireDigest, message: "requires a digest"},
		{name: "strict no digest algorithm", res: resourceWithDigest(&descriptor.Digest{HashAlgorithm: descriptor.NoDigest}), policy: RequireDigest, message: "requires a digest"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			calls := 0
			backend := &resourceBackendStub{fetch: func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
				calls++
				return inmemory.New(strings.NewReader(verifyContent)), nil
			}}
			content, err := NewResourceRepositoryWithVerification(backend, tt.policy).DownloadResource(t.Context(), tt.res, nil)
			r.ErrorContains(err, tt.message)
			r.Nil(content)
			r.Zero(calls, "invalid expectations must be rejected before transport is invoked")
		})
	}
}

func TestResourceRepositoryPermissiveMissingDigest(t *testing.T) {
	for _, tt := range []struct {
		name   string
		digest *descriptor.Digest
	}{
		{name: "nil"},
		{name: "empty", digest: &descriptor.Digest{}},
		{name: "excluded", digest: &descriptor.Digest{HashAlgorithm: descriptor.NoDigest, NormalisationAlgorithm: descriptor.ExcludeFromSignature, Value: descriptor.NoDigest}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			original := inmemory.New(strings.NewReader(verifyContent))
			calls := 0
			backend := &resourceBackendStub{fetch: func(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
				calls++
				return original, nil
			}}
			content, err := NewVerifiedResourceRepository(backend).DownloadResource(t.Context(), resourceWithDigest(tt.digest), nil)
			r.NoError(err)
			r.Equal(1, calls)
			r.Same(original, content, "unverified content must pass through unchanged")
			var dst bytes.Buffer
			r.NoError(blob.Copy(&dst, content))
			r.Equal(verifyContent, dst.String())
		})
	}
}

func TestResourceRepositorySnapshotsExpectedDigest(t *testing.T) {
	for _, mutation := range []string{"modify value", "replace digest", "remove digest"} {
		for _, matches := range []bool{true, false} {
			name := mutation + " mismatch"
			if matches {
				name = mutation + " matching"
			}
			t.Run(name, func(t *testing.T) {
				r := require.New(t)
				res := resourceWithDigest(downloadDigest(verifyContent))
				backend := &resourceBackendStub{fetch: func(_ context.Context, gotRes *descriptor.Resource, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
					switch mutation {
					case "modify value":
						gotRes.Digest.Value = godigest.FromString("backend content").Encoded()
					case "replace digest":
						gotRes.Digest = downloadDigest("backend content")
					case "remove digest":
						gotRes.Digest = nil
					}
					data := "backend content"
					if matches {
						data = verifyContent
					}
					return inmemory.New(strings.NewReader(data)), nil
				}}
				content, err := NewVerifiedResourceRepository(backend).DownloadResource(t.Context(), res, nil)
				r.NoError(err)
				var dst bytes.Buffer
				err = blob.Copy(&dst, content)
				if matches {
					r.NoError(err)
					r.Equal(verifyContent, dst.String())
				} else {
					r.ErrorContains(err, "digest mismatch")
				}
			})
		}
	}
}

func TestResourceRepositoryBackendErrorsAndForwarding(t *testing.T) {
	for _, fails := range []bool{false, true} {
		name := "success"
		if fails {
			name = "backend error"
		}
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			res := resourceWithDigest(downloadDigest(verifyContent))
			credentials := &runtime.Raw{}
			original := inmemory.New(strings.NewReader(verifyContent))
			updated := resourceWithDigest(nil)
			identity := runtime.Identity{"hostname": "example.org"}
			var backendErr error
			if fails {
				backendErr = errors.New("backend failed")
			}
			fetchCalls, uploadCalls, identityCalls := 0, 0, 0
			backend := &resourceBackendStub{
				fetch: func(gotCtx context.Context, gotRes *descriptor.Resource, gotCredentials runtime.Typed) (blob.ReadOnlyBlob, error) {
					fetchCalls++
					r.Equal(ctx, gotCtx)
					r.Same(res, gotRes)
					r.Same(credentials, gotCredentials)
					if backendErr != nil {
						return nil, backendErr
					}
					return original, nil
				},
				upload: func(gotCtx context.Context, gotRes *descriptor.Resource, gotContent blob.ReadOnlyBlob, gotCredentials runtime.Typed) (*descriptor.Resource, error) {
					uploadCalls++
					r.Equal(ctx, gotCtx)
					r.Same(res, gotRes)
					r.Same(original, gotContent)
					r.Same(credentials, gotCredentials)
					return updated, backendErr
				},
				identity: func(gotCtx context.Context, gotRes *descriptor.Resource) (runtime.Identity, error) {
					identityCalls++
					r.Equal(ctx, gotCtx)
					r.Same(res, gotRes)
					return identity, backendErr
				},
			}
			repo := NewVerifiedResourceRepository(backend)
			content, err := repo.DownloadResource(ctx, res, credentials)
			if fails {
				r.ErrorIs(err, backendErr)
				r.Nil(content)
			} else {
				r.NoError(err)
				var dst bytes.Buffer
				r.NoError(blob.Copy(&dst, content))
			}
			gotRes, err := repo.UploadResource(ctx, res, original, credentials)
			r.Same(updated, gotRes)
			if fails {
				r.ErrorIs(err, backendErr)
			} else {
				r.NoError(err)
			}
			gotIdentity, err := repo.GetResourceCredentialConsumerIdentity(ctx, res)
			r.Equal(identity, gotIdentity)
			if fails {
				r.ErrorIs(err, backendErr)
			} else {
				r.NoError(err)
			}
			r.Equal(1, fetchCalls)
			r.Equal(1, uploadCalls)
			r.Equal(1, identityCalls)
		})
	}
}
