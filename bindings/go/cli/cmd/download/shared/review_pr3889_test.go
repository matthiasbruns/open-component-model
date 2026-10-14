package shared_test

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/cli/cmd/download/shared"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// A SHA-512 genericBlobDigest/v1 is a valid, signable resource digest
// (signing.ValidateDigestHashAlgorithms accepts SHA-256 and SHA-512), so a pinned
// download of matching content must succeed.
func TestReviewPR3889_F3_SHA512ResourceDigest(t *testing.T) {
	r := require.New(t)
	data := []byte("pinned content")
	sum := sha512.Sum512(data)
	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "blob", Version: "1.0.0"}},
		Digest: &descriptor.Digest{
			HashAlgorithm:          "SHA-512",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  hex.EncodeToString(sum[:]),
		},
	}

	verified, err := shared.VerifyResourceContent(t.Context(), res, inmemory.New(bytes.NewReader(data)))
	r.NoError(err, "content matching its SHA-512 resource digest must verify")
	rc, err := verified.ReadCloser()
	r.NoError(err)
	got, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())
	r.Equal(data, got)
}

// repository.VerifyDownload accepts a digest value spelled "sha256:<hex>"; the OCI
// layout branch of VerifyResourceContent must accept the same spelling.
func TestReviewPR3889_F4_PrefixedOCIArtifactDigest(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	store := memory.New()
	layer := []byte("layer")
	layerDesc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: digest.FromBytes(layer), Size: int64(len(layer))}
	r.NoError(store.Push(ctx, layerDesc, bytes.NewReader(layer)))
	manifest, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layerDesc},
	})
	r.NoError(err)

	layout, err := tar.CopyToOCILayoutInMemory(ctx, store, manifest, tar.CopyToOCILayoutOptions{Tags: []string{"v1"}})
	r.NoError(err)

	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "image", Version: "1.0.0"}},
		Digest: &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "ociArtifactDigest/v1",
			Value:                  manifest.Digest.String(),
		},
	}

	_, err = shared.VerifyResourceContent(ctx, res, layout)
	r.NoError(err, "a matching manifest digest spelled sha256:<hex> must verify")
}
