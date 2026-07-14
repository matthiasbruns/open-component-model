package coordinate

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
)

func ociType() runtime.Type { return runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion) }

func Test_OCI_ToCoordinate(t *testing.T) {
	r := require.New(t)
	coord, err := New(Config{}).ToCoordinate(&v1.OCIImage{
		Type:           ociType(),
		ImageReference: "ghcr.io/acme/podinfo:6.7.1",
	})
	r.NoError(err)
	r.Equal("acme/podinfo", coord.Path, "registry host is dropped")
	r.Equal("6.7.1", coord.Version)
}

func Test_OCI_SameTech_RoundTrip(t *testing.T) {
	r := require.New(t)
	coord, err := New(Config{}).ToCoordinate(&v1.OCIImage{Type: ociType(), ImageReference: "ghcr.io/acme/podinfo:6.7.1"})
	r.NoError(err)

	out, err := New(Config{RegistryBaseURL: "registry.internal/mirror"}).FromCoordinate(coord)
	r.NoError(err)
	got := out.(*v1.OCIImage)
	r.Equal("registry.internal/mirror/acme/podinfo:6.7.1", got.ImageReference)
}

// Test_OCI_CrossTech_FromS3Coordinate proves the OCI placer consumes a coordinate produced by
// S3 without knowing anything about S3.
func Test_OCI_CrossTech_FromS3Coordinate(t *testing.T) {
	r := require.New(t)
	fromS3 := &descriptor.Coordinate{Path: "artifacts/data", Version: "v1.0.0"}
	out, err := New(Config{RegistryBaseURL: "ghcr.io/acme"}).FromCoordinate(fromS3)
	r.NoError(err)
	got := out.(*v1.OCIImage)
	r.Equal("ghcr.io/acme/artifacts/data:v1.0.0", got.ImageReference)
}
