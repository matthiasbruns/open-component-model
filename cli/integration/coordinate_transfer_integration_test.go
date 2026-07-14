package integration

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocicoord "ocm.software/open-component-model/bindings/go/oci/coordinate"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3coord "ocm.software/open-component-model/bindings/go/s3/coordinate"
	s3repository "ocm.software/open-component-model/bindings/go/s3/repository"
	s3accessv1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	s3credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
	transfercoord "ocm.software/open-component-model/bindings/go/transfer/coordinate"
	"ocm.software/open-component-model/cli/integration/internal"
)

// ociSourceAdapter adapts the oci.Repository (whose resource methods take no credentials,
// resolving them from the configured resolver) to the generic repository.ResourceRepository
// consumed by the coordinate transfer. Download-only; it is used as a transfer source.
type ociSourceAdapter struct{ repo *oci.Repository }

var _ repository.ResourceRepository = ociSourceAdapter{}

func (a ociSourceAdapter) DownloadResource(ctx context.Context, res *descriptor.Resource, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
	return a.repo.DownloadResource(ctx, res)
}

func (a ociSourceAdapter) UploadResource(context.Context, *descriptor.Resource, blob.ReadOnlyBlob, runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("oci source adapter is download-only")
}

func (a ociSourceAdapter) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

// Test_Integration_Coordinate_Transfer_OCI_To_S3 drives the full coordinate-based transfer:
// an OCI artifact is moved to S3 through Transfer, which produces the neutral coordinate from
// the OCI access, places it into S3, keeps the original OCI access as origin, and uploads.
func Test_Integration_Coordinate_Transfer_OCI_To_S3(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	// Source: OCI registry. Target: MinIO bucket.
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)
	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	ociRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	m := internal.CreateMinIO(t)
	const bucket = "mirror-bucket"
	m.CreateBucket(t, bucket)
	s3repo := s3repository.NewResourceRepository()
	s3creds := &s3credv1.S3Credentials{
		Type:            s3credv1.S3CredentialsVersionedType,
		AccessKeyID:     m.User,
		SecretAccessKey: m.Password,
	}

	// Push an OCI artifact to the source registry.
	ociType := runtime.NewVersionedType(ociaccessv1.LegacyType, ociaccessv1.LegacyTypeVersion)
	ociRes := &descriptor.Resource{}
	ociRes.Name = "image"
	ociRes.Version = "6.7.1"
	ociRes.Type = "ociImage"
	ociRes.Access = &ociaccessv1.OCIImage{
		Type:           ociType,
		ImageReference: fmt.Sprintf("%s/acme/podinfo:6.7.1", registry.RegistryAddress),
	}
	layoutTar := internal.CreateSingleLayerOCIImageLayoutTar(t, []byte("oci artifact payload"), "podinfo:6.7.1")
	pushed, err := ociRepo.UploadResource(ctx, ociRes, direct.NewFromBuffer(layoutTar, true))
	r.NoError(err)

	// Wire the coordinate transfer: OCI producer registered; S3 placer configured for the target.
	reg := transfercoord.NewRegistry()
	reg.Register(ocicoord.New(ocicoord.Config{}), ociType)
	targetCoordinator := s3coord.New(s3coord.Config{
		Bucket:       bucket,
		KeyPrefix:    "oci",
		Region:       "us-east-1",
		Endpoint:     m.Endpoint,
		UsePathStyle: true,
	})

	// oci -> s3 via the coordinate system.
	out, err := transfercoord.Transfer(ctx, ociSourceAdapter{ociRepo}, pushed, nil, reg, targetCoordinator, s3repo, s3creds)
	r.NoError(err, "coordinate transfer oci -> s3 should succeed")

	// The resource now carries an S3 access placed from the neutral coordinate.
	r.Equal("S3/v1", out.Access.GetType().String())
	s3out := out.Access.(*s3accessv1.S3)
	r.Equal(bucket, s3out.BucketName)
	r.Equal("oci/acme/podinfo/6.7.1", s3out.ObjectKey, "path+version mapped into the S3 key under the configured prefix")

	// The reference hint carries the neutral coordinate AND the original OCI access as origin.
	hint, ok := transfercoord.ReferenceHintOf(out)
	r.True(ok, "reference hint must be present after transfer")
	r.Equal("acme/podinfo", hint.Coordinate.Path, "coordinate keeps the host-stripped repository path")
	r.Equal("6.7.1", hint.Coordinate.Version)
	r.NotNil(hint.Origin, "origin must be preserved")
	r.Equal("ociArtifact/v1", hint.Origin.GetType().String(), "origin is the original OCI access")

	// The content really moved to S3, byte-identical to the OCI artifact.
	ociBlob, err := ociRepo.DownloadResource(ctx, pushed)
	r.NoError(err)
	ociRC, err := ociBlob.ReadCloser()
	r.NoError(err)
	ociData, err := io.ReadAll(ociRC)
	r.NoError(err)
	r.NoError(ociRC.Close())

	verify := &descriptor.Resource{}
	verify.Access = out.Access
	fromS3, err := s3repo.DownloadResource(ctx, verify, s3creds)
	r.NoError(err)
	s3RC, err := fromS3.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(s3RC.Close()) }()
	s3Data, err := io.ReadAll(s3RC)
	r.NoError(err)
	r.Equal(ociData, s3Data, "the OCI artifact must be byte-identical in S3 after the coordinate transfer")
}
