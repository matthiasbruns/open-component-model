package coordinate

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

func Test_ToCoordinate_S3(t *testing.T) {
	r := require.New(t)

	spec := &v1.S3{
		Type:       accessspec.V1VersionedType,
		BucketName: "source-bucket",
		ObjectKey:  "artifacts/data.bin",
		MediaType:  "application/octet-stream",
	}

	coord, err := New(Config{}).ToCoordinate(spec)
	r.NoError(err)
	r.Equal("artifacts/data.bin", coord.Path)
	r.Equal("application/octet-stream", coord.MediaType)
}

func Test_S3_SameTech_RoundTrip(t *testing.T) {
	r := require.New(t)

	src := &v1.S3{
		Type:       accessspec.V1VersionedType,
		BucketName: "source-bucket",
		ObjectKey:  "artifacts/data.bin",
		MediaType:  "application/octet-stream",
	}

	coord, err := New(Config{}).ToCoordinate(src)
	r.NoError(err)

	// place into a different bucket/environment.
	out, err := New(Config{
		Bucket:       "target-bucket",
		Region:       "us-east-1",
		Endpoint:     "http://minio.internal:9000",
		UsePathStyle: true,
	}).FromCoordinate(coord)
	r.NoError(err)

	got := out.(*v1.S3)
	r.Equal("target-bucket", got.BucketName)
	r.Equal("artifacts/data.bin", got.ObjectKey, "path survives the round-trip")
	r.Equal("application/octet-stream", got.MediaType)
	r.Equal("http://minio.internal:9000", got.Endpoint, "environment comes from config, not the coordinate")
}

// Test_CrossTech_OCICoordinateToS3 proves the S3 placer consumes a coordinate produced by
// a DIFFERENT technology (OCI) without knowing anything about OCI — no N*M coupling.
func Test_CrossTech_OCICoordinateToS3(t *testing.T) {
	r := require.New(t)

	// This is what an OCI ToCoordinate would produce for ghcr.io/acme/podinfo:6.7.1.
	// The S3 placer never sees "imageReference" — only the neutral coordinate.
	fromOCI := &descriptor.Coordinate{
		Path:      "acme/podinfo",
		Version:   "6.7.1",
		MediaType: "application/vnd.oci.image.manifest.v1+tar+gzip",
	}

	out, err := New(Config{Bucket: "mirror-bucket", KeyPrefix: "oci"}).FromCoordinate(fromOCI)
	r.NoError(err)

	got := out.(*v1.S3)
	r.Equal("mirror-bucket", got.BucketName)
	r.Equal("oci/acme/podinfo/6.7.1", got.ObjectKey, "coordinate path+version map into the S3 key")
	r.Equal("application/vnd.oci.image.manifest.v1+tar+gzip", got.MediaType)
}

// Test_ContentAddressed proves a coordinate with only a digest (IPFS/CAS-style source)
// still places into S3, using the digest as the object key.
func Test_ContentAddressed(t *testing.T) {
	r := require.New(t)

	coord := &descriptor.Coordinate{
		Digest:    "sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
		MediaType: "application/octet-stream",
	}

	out, err := New(Config{Bucket: "cas-bucket"}).FromCoordinate(coord)
	r.NoError(err)

	got := out.(*v1.S3)
	r.Equal("sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae", got.ObjectKey)
}
