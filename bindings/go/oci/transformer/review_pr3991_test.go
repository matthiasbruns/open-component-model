package transformer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type review3991Reader struct {
	io.Reader
	closed bool
}

func (r *review3991Reader) Close() error { r.closed = true; return nil }

type review3991Repository struct {
	repository.ComponentVersionRepository
	content  blob.ReadOnlyBlob
	resource *descriptor.Resource
}

func (r *review3991Repository) GetLocalResource(context.Context, string, string, runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return r.content, r.resource, nil
}

func (r *review3991Repository) AddLocalResource(context.Context, string, string, *descriptor.Resource, blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, errors.New("target unavailable before reading source")
}

type review3991Provider struct {
	repository.ComponentVersionRepositoryProvider
	repo *review3991Repository
}

func (p *review3991Provider) GetComponentVersionRepository(context.Context, runtime.Typed, runtime.Typed) (repository.ComponentVersionRepository, error) {
	return p.repo, nil
}

func TestReviewPR3991_SourceClosedOnTargetFailure(t *testing.T) {
	for _, fused := range []bool{false, true} {
		name := "previous_get_add"
		if fused {
			name = "fused"
		}
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			data := []byte("source body")
			source := &review3991Reader{Reader: bytes.NewReader(data)}
			d := digest.FromBytes(data)
			res := &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "payload", Version: "1.0.0"}},
				Type:        "plainText", Relation: descriptor.LocalRelation,
				Access: &v2.LocalBlob{Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion), LocalReference: d.String(), MediaType: "text/plain"},
			}
			p := &review3991Provider{repo: &review3991Repository{resource: res, content: ociblob.NewDescriptorBlob(source, ocispec.Descriptor{Digest: d, Size: int64(len(data)), MediaType: "text/plain"})}}
			scheme := runtime.NewScheme()
			scheme.MustRegisterScheme(v1alpha1.Scheme)
			scheme.MustRegisterScheme(v2.Scheme)
			spec := ctfspec.Repository{Type: runtime.NewVersionedType(ctfspec.Type, ctfspec.Version), FilePath: t.TempDir()}
			var err error
			if fused {
				_, err = (&TransferLocalBlob{Scheme: scheme, RepoProvider: p}).Transform(t.Context(), &v1alpha1.TransferCTFLocalBlobToCTFLocalBlob{
					Type: v1alpha1.TransferCTFLocalBlobToCTFLocalBlobV1alpha1,
					Spec: &v1alpha1.TransferCTFLocalBlobToCTFLocalBlobSpec{Source: v1alpha1.CTFLocalBlobSource{Repository: spec, Component: "example.org/c", Version: "1.0.0", ResourceIdentity: runtime.Identity{"name": "payload"}}, Repository: spec},
				})
			} else {
				out, getErr := (&GetLocalResource{Scheme: scheme, RepoProvider: p}).Transform(t.Context(), &v1alpha1.CTFGetLocalResource{
					Type: v1alpha1.CTFGetLocalResourceV1alpha1,
					Spec: &v1alpha1.CTFGetLocalResourceSpec{Repository: spec, Component: "example.org/c", Version: "1.0.0", ResourceIdentity: runtime.Identity{"name": "payload"}, OutputPath: t.TempDir()},
				})
				r.NoError(getErr)
				got := out.(*v1alpha1.CTFGetLocalResource).Output
				_, err = (&AddLocalResource{Scheme: scheme, RepoProvider: p}).Transform(t.Context(), &v1alpha1.CTFAddLocalResource{
					Type: v1alpha1.CTFAddLocalResourceV1alpha1,
					Spec: &v1alpha1.CTFAddLocalResourceSpec{Repository: spec, Component: "example.org/c", Version: "1.0.0", Resource: got.Resource, File: got.File},
				})
			}
			r.ErrorContains(err, "target unavailable")
			r.True(source.closed, "source reader must be closed when target fails before consuming it")
		})
	}
}
