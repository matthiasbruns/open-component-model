package pack_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	resourceblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/pack"
	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type reviewPR3998WrongSizeBlob struct {
	data []byte
	dig  digest.Digest
}

func (b *reviewPR3998WrongSizeBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func (b *reviewPR3998WrongSizeBlob) Size() int64 { return int64(len(b.data) + 1) }

func (b *reviewPR3998WrongSizeBlob) Digest() (string, bool) { return b.dig.String(), true }

func (b *reviewPR3998WrongSizeBlob) MediaType() (string, bool) {
	return "application/octet-stream", true
}

// ORAS remote blob existence checks use the digest and ignore descriptor size.
type reviewPR3998DigestOnlyStore struct {
	content.Storage
	actualSize int64
}

type reviewPR3998HeadDeniedStore struct{ content.Storage }

func (s *reviewPR3998HeadDeniedStore) Exists(context.Context, ocispec.Descriptor) (bool, error) {
	return false, errors.New("HEAD forbidden")
}

func (s *reviewPR3998DigestOnlyStore) Exists(ctx context.Context, desc ocispec.Descriptor) (bool, error) {
	desc.Size = s.actualSize
	return s.Storage.Exists(ctx, desc)
}

func TestReviewPR3998_SizeMetadata_RejectsExistingBlobMismatch(t *testing.T) {
	r := require.New(t)
	data := []byte("already in the target")
	dig := digest.FromBytes(data)
	store := &reviewPR3998DigestOnlyStore{Storage: memory.New(), actualSize: int64(len(data))}
	r.NoError(store.Storage.Push(t.Context(), ocispec.Descriptor{
		MediaType: "application/octet-stream", Digest: dig, Size: int64(len(data)),
	}, bytes.NewReader(data)))

	resource := &descriptor.Resource{}
	b, err := resourceblob.NewArtifactBlob(resource, &reviewPR3998WrongSizeBlob{data: data, dig: dig})
	r.NoError(err)
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	oci.MustAddToScheme(scheme)

	_, err = pack.ResourceLocalBlobOCILayer(t.Context(), store, b,
		&v2.LocalBlob{MediaType: "application/octet-stream"},
		pack.Options{AccessScheme: scheme, BaseReference: "test-ref"})
	r.Error(err, "a digest-only existence check must not accept incorrect source size metadata")
}

func TestReviewPR3998_PushOnlyStore_AllowsMissingBlobUpload(t *testing.T) {
	r := require.New(t)
	data := []byte("new blob for push-only target")
	dig := digest.FromBytes(data)
	store := &reviewPR3998HeadDeniedStore{Storage: memory.New()}
	resource := &descriptor.Resource{}
	b, err := resourceblob.NewArtifactBlob(resource, &testBlob{
		content: data, mediaType: "application/octet-stream", digest: dig,
	})
	r.NoError(err)
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)
	oci.MustAddToScheme(scheme)

	_, err = pack.ResourceLocalBlobOCILayer(t.Context(), store, b,
		&v2.LocalBlob{MediaType: "application/octet-stream"},
		pack.Options{AccessScheme: scheme, BaseReference: "test-ref"})
	r.NoError(err, "a missing blob should still be pushable when HEAD is forbidden")
}
