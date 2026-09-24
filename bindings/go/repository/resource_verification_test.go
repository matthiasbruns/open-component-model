package repository

import (
	"bytes"

	"errors"
	"log/slog"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func downloadDigest(content string) *descriptor.Digest {
	return &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  godigest.FromString(content).Encoded(),
	}
}

func TestResourceVerifierVerification(t *testing.T) {
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
				verifier, err := NewGenericResourceVerifierProvider(policy).GetResourceVerifier(ctx, res)
				r.NoError(err)
				content, err := verifier.Verify(ctx, inmemory.New(strings.NewReader(tt.content)))
				r.NoError(err, "verification is deferred until content is consumed")
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

func TestResourceVerifierProviderRejectsInvalidExpectations(t *testing.T) {
	value := godigest.FromString(verifyContent).Encoded()
	for _, tt := range []struct {
		name    string
		res     *descriptor.Resource
		policy  DownloadVerificationPolicy
		message string
	}{
		{name: "nil resource", message: "resource is required"},
		{name: "unknown policy", res: resourceWithDigest(downloadDigest(verifyContent)), policy: DownloadVerificationPolicy(255), message: "unsupported download verification policy"},
		{name: "unknown policy without digest", res: resourceWithDigest(nil), policy: DownloadVerificationPolicy(255), message: "unsupported download verification policy"},
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
			verifier, err := NewGenericResourceVerifierProvider(tt.policy).GetResourceVerifier(t.Context(), tt.res)
			r.ErrorContains(err, tt.message)
			r.Nil(verifier, "invalid expectations must be rejected before content is supplied")
		})
	}
}

func TestResourceVerifierProviderPermissiveMissingDigest(t *testing.T) {
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
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			original := inmemory.New(strings.NewReader(verifyContent))
			res := resourceWithDigest(tt.digest)
			verifier, err := NewGenericResourceVerifierProvider(VerifyIfPresent).GetResourceVerifier(t.Context(), res)
			r.NoError(err)
			r.Contains(logs.String(), "level=WARN")
			r.Contains(logs.String(), "resource has no digest")
			logs.Reset()
			res.Digest = downloadDigest("later expectation")
			content, err := verifier.Verify(t.Context(), original)
			r.NoError(err)
			r.Same(original, content, "unverified content must pass through unchanged")
			r.Empty(logs.String(), "the warning belongs to provider selection, not verification")
			var dst bytes.Buffer
			r.NoError(blob.Copy(&dst, content))
			r.Equal(verifyContent, dst.String())
		})
	}
}

func TestResourceVerifierProviderSnapshotsExpectedDigest(t *testing.T) {
	for _, mutation := range []string{"modify value", "replace digest", "remove digest"} {
		for _, matches := range []bool{true, false} {
			name := mutation + " mismatch"
			if matches {
				name = mutation + " matching"
			}
			t.Run(name, func(t *testing.T) {
				r := require.New(t)
				res := resourceWithDigest(downloadDigest(verifyContent))
				verifier, err := NewGenericResourceVerifierProvider(VerifyIfPresent).GetResourceVerifier(t.Context(), res)
				r.NoError(err)
				switch mutation {
				case "modify value":
					res.Digest.Value = godigest.FromString("backend content").Encoded()
				case "replace digest":
					res.Digest = downloadDigest("backend content")
				case "remove digest":
					res.Digest = nil
				}
				data := "backend content"
				if matches {
					data = verifyContent
				}
				content, err := verifier.Verify(t.Context(), inmemory.New(strings.NewReader(data)))
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

type failingCloseBlob struct {
	*closableBlob
	closeErr error
}

func (b *failingCloseBlob) Close() error {
	b.closed = true
	return b.closeErr
}

func TestResourceVerifierWrapFailureCleanup(t *testing.T) {
	for _, fails := range []bool{false, true} {
		name := "successful cleanup"
		if fails {
			name = "failed cleanup"
		}
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			original := &failingCloseBlob{closableBlob: &closableBlob{Blob: inmemory.New(strings.NewReader(verifyContent))}}
			if fails {
				original.closeErr = errors.New("close failed")
			}
			// Provider validation prevents this, but wrapping failures must still release content.
			verifier := &genericResourceVerifier{expected: "sha256:invalid"}
			content, err := verifier.Verify(t.Context(), original)
			r.ErrorContains(err, "invalid expected digest")
			r.Nil(content)
			r.True(original.closed)
			if fails {
				r.ErrorIs(err, original.closeErr)
			}
		})
	}
}
