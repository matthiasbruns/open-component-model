package oci_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const reviewPR3595Qcow2MediaType = "application/vnd.ocm.software.vm.disk.qcow2.v1"

// reviewPR3595Setup publishes a qcow2 disk the way ADR Flow C describes ("published with ORAS"):
// one OCI manifest with the disk as its only layer, tagged ghcr.io/acme/vm-images/ubuntu:24.04.
func reviewPR3595Setup(t *testing.T) (*oci.Repository, []byte, ociImageSpecV1.Descriptor) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))

	imgStore, err := store.StoreForReference(ctx, "ghcr.io/acme/vm-images/ubuntu:24.04")
	r.NoError(err)

	disk := []byte("QFI\xfb fake qcow2 disk bytes")
	layer := content.NewDescriptorFromBytes(reviewPR3595Qcow2MediaType, disk)
	r.NoError(imgStore.Push(ctx, layer, bytes.NewReader(disk)))
	manifest, err := oras.PackManifest(ctx, imgStore, oras.PackManifestVersion1_1, reviewPR3595Qcow2MediaType, oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)
	r.NoError(imgStore.Tag(ctx, manifest, "24.04"))

	return Repository(t, ocictf.WithCTF(store)), disk, layer
}

func reviewPR3595ReadAll(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer rc.Close()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

// PR 3595 F3: ADR decision 8 says `ocm download resource` streams the disk blob for every flow.
// For Flow C (OCIImage access) the repository instead returns a gzipped OCI layout of the
// whole artifact, built via Materialize(), which decision 6 calls the anti-pattern.
func TestReviewPR3595_F3_OCIImageDownloadYieldsDiskBytes(t *testing.T) {
	r := require.New(t)
	repo, disk, _ := reviewPR3595Setup(t)

	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "disk", Version: "24.04.1"}},
		Type:        "VirtualMachineImage",
		Access: &v1.OCIImage{
			Type:           runtime.NewVersionedType(v1.OCIImageType, v1.Version),
			ImageReference: "ghcr.io/acme/vm-images/ubuntu:24.04",
		},
	}

	b, err := repo.DownloadResource(t.Context(), res)
	r.NoError(err)
	mediaTypeAware, ok := b.(blob.MediaTypeAware)
	r.True(ok)
	got, _ := mediaTypeAware.MediaType()
	r.Equal(reviewPR3595Qcow2MediaType, got, "consumer receives an OCI layout, not the disk")
	r.Equal(disk, reviewPR3595ReadAll(t, b), "downloaded bytes must be the qcow2 disk")
}

// PR 3595 F3 (alternative): pointing at the disk layer directly via OCIImageLayer access would
// yield the disk bytes, but the OCI repository does not support downloading that access type.
func TestReviewPR3595_F3_OCIImageLayerDownloadYieldsDiskBytes(t *testing.T) {
	r := require.New(t)
	repo, disk, layer := reviewPR3595Setup(t)

	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "disk", Version: "24.04.1"}},
		Type:        "VirtualMachineImage",
		Access: &v1.OCIImageLayer{
			Type:      runtime.NewVersionedType(v1.OCIImageLayerType, v1.Version),
			Reference: "ghcr.io/acme/vm-images/ubuntu",
			MediaType: reviewPR3595Qcow2MediaType,
			Digest:    layer.Digest,
			Size:      layer.Size,
		},
	}

	b, err := repo.DownloadResource(t.Context(), res)
	r.NoError(err)
	r.Equal(disk, reviewPR3595ReadAll(t, b))
}
