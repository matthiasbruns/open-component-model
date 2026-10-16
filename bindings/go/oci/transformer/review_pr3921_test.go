package transformer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	blobv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/ctf"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type reviewPR3921RepoProvider struct {
	repo repository.ComponentVersionRepository
}

func (p *reviewPR3921RepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(context.Context, runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (p *reviewPR3921RepoProvider) GetComponentVersionRepository(context.Context, runtime.Typed, runtime.Typed) (repository.ComponentVersionRepository, error) {
	return p.repo, nil
}

func (p *reviewPR3921RepoProvider) GetJSONSchemaForRepositorySpecification(runtime.Type) ([]byte, error) {
	return nil, nil
}

// TestReviewPR3921_F1_IncompleteSourceDigestIsCompletedNotRejected exercises the real CTF sink.
// The OCI sink completes an incomplete source digest after verifying the fields that are set
// agree with the content (#3681). reconcileSourceDigest compares the whole struct and turns that
// verified completion into a "digest mismatch" failure.
func TestReviewPR3921_F1_IncompleteSourceDigestIsCompletedNotRejected(t *testing.T) {
	data := []byte("review pr 3921")
	sum := sha256.Sum256(data)
	value := hex.EncodeToString(sum[:])

	tests := []struct {
		name   string
		digest *v2.Digest
	}{
		{name: "missing normalisation algorithm", digest: &v2.Digest{HashAlgorithm: "SHA-256", Value: value}},
		{name: "OCI hash spelling", digest: &v2.Digest{HashAlgorithm: "sha256", Value: value}},
		{name: "value only", digest: &v2.Digest{Value: value}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)
			repo, err := oci.NewRepository(oci.WithTempDir(t.TempDir()), ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
			r.NoError(err)

			file := filepath.Join(t.TempDir(), "res.bin")
			r.NoError(os.WriteFile(file, data, 0o644))

			scheme := runtime.NewScheme()
			v2.MustAddToScheme(scheme)
			scheme.MustRegisterWithAlias(&v1alpha1.CTFAddLocalResource{}, v1alpha1.CTFAddLocalResourceV1alpha1)
			tr := &AddLocalResource{Scheme: scheme, RepoProvider: &reviewPR3921RepoProvider{repo: repo}}

			spec := &v1alpha1.CTFAddLocalResource{
				Type: runtime.NewVersionedType(v1alpha1.CTFAddLocalResourceType, v1alpha1.Version),
				Spec: &v1alpha1.CTFAddLocalResourceSpec{
					Repository: ctfspec.Repository{Type: runtime.Type{Name: ctfspec.Type, Version: "v1"}, FilePath: t.TempDir()},
					Component:  "ocm.software/review",
					Version:    "1.0.0",
					Resource: &v2.Resource{
						ElementMeta: v2.ElementMeta{ObjectMeta: v2.ObjectMeta{Name: "res", Version: "1.0.0"}},
						Type:        "blob",
						Relation:    v2.LocalRelation,
						Digest:      tc.digest,
						Access: &runtime.Raw{
							Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
							Data: []byte(`{"type":"localBlob/v1","localReference":"sha256:` + value + `","mediaType":"application/octet-stream"}`),
						},
					},
					File: blobv1alpha1.File{Type: runtime.Type{Name: blobv1alpha1.FileType, Version: blobv1alpha1.Version}, URI: "file://" + file, MediaType: "application/octet-stream"},
				},
			}

			res, err := tr.Transform(ctx, spec)
			r.NoError(err, "the sink verified the set fields against the content; completing them is not a mismatch")
			out := res.(*v1alpha1.CTFAddLocalResource).Output.Resource
			r.NotNil(out.Digest)
			r.Equal(value, out.Digest.Value)
		})
	}
}
