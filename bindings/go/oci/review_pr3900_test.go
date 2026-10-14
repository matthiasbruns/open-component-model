package oci_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
)

// reviewPR3900Resolver hands out stores emulating a write-once registry:
// re-tagging an existing reference and re-pushing existing content are both
// rejected, even when the target is identical.
type reviewPR3900Resolver struct {
	oci.Resolver
	rejectTag  bool
	rejectPush bool
}

func (r reviewPR3900Resolver) StoreForReference(ctx context.Context, reference string) (spec.Store, error) {
	s, err := r.Resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, err
	}
	return &reviewPR3900Store{Store: s, rejectTag: r.rejectTag, rejectPush: r.rejectPush}, nil
}

type reviewPR3900Store struct {
	spec.Store
	rejectTag  bool
	rejectPush bool
}

func (s *reviewPR3900Store) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	if _, err := s.Resolve(ctx, reference); s.rejectTag && err == nil {
		return fmt.Errorf("redeploy disabled: %q cannot be updated as asset already exists", reference)
	}
	return s.Store.Tag(ctx, desc, reference)
}

func (s *reviewPR3900Store) Push(ctx context.Context, desc ociImageSpecV1.Descriptor, r io.Reader) error {
	if exists, err := s.Exists(ctx, desc); s.rejectPush && err == nil && exists {
		return fmt.Errorf("redeploy disabled: asset %s already exists", desc.Digest)
	}
	return s.Store.Push(ctx, desc, r)
}

// F1: transfer of an ociArtifact resource goes through UploadResourceStream
// (transformer/transfer_oci_artifact.go), whose tag write is not idempotent.
func TestReviewPR3900_F1_UploadResourceStreamRetagIdempotent(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, oci.WithResolver(reviewPR3900Resolver{Resolver: store, rejectTag: true}), oci.WithScheme(testScheme))

	memStore, manifestDesc := buildTestManifestStream(t)
	upload := func() error {
		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "stream-res", Version: "1.0.0"}},
			Type:        "ociImage",
			Access:      &v1.OCIImage{ImageReference: "test-repo:v1.0.0"},
		}
		_, err := repo.UploadResourceStream(ctx, resource, &ocistream.OCIResourceStream{
			ReadOnlyGraphStorage: memStore,
			Descriptor:           manifestDesc,
			ExtendedCopyOpts:     oras.DefaultExtendedCopyGraphOptions,
		})
		return err
	}

	r.NoError(upload())
	r.NoError(upload(), "re-streaming identical OCI artifact must succeed on a redeploy-disabled registry")
}

// F2: under the PR's own push-once model (used for the ownership referrer),
// AddComponentVersion still re-pushes the descriptor manifest unconditionally.
func TestReviewPR3900_F2_AddComponentVersionRepushIdempotent(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, oci.WithResolver(reviewPR3900Resolver{Resolver: store, rejectTag: true, rejectPush: true}))

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "test-provider"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/test-component", Version: "1.0.0"},
			},
		},
	}

	r.NoError(repo.AddComponentVersion(ctx, desc))
	r.NoError(repo.AddComponentVersion(ctx, desc),
		"re-adding identical content must succeed when the registry rejects content re-pushes")
}
