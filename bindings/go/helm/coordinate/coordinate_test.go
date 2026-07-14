package coordinate

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func helmType() runtime.Type { return runtime.NewVersionedType(v1.Type, v1.Version) }

func Test_Helm_ToCoordinate(t *testing.T) {
	r := require.New(t)
	coord, err := New(Config{}).ToCoordinate(&v1.Helm{
		Type:           helmType(),
		HelmRepository: "https://charts.example.com",
		HelmChart:      "podinfo:6.7.1",
	})
	r.NoError(err)
	r.Equal("podinfo", coord.Path, "helm repository URL is dropped")
	r.Equal("6.7.1", coord.Version)
}

func Test_Helm_SameTech_RoundTrip(t *testing.T) {
	r := require.New(t)
	coord, err := New(Config{}).ToCoordinate(&v1.Helm{Type: helmType(), HelmRepository: "https://src", HelmChart: "podinfo:6.7.1"})
	r.NoError(err)

	out, err := New(Config{HelmRepository: "https://mirror.example.com"}).FromCoordinate(coord)
	r.NoError(err)
	got := out.(*v1.Helm)
	r.Equal("https://mirror.example.com", got.HelmRepository)
	r.Equal("podinfo:6.7.1", got.HelmChart)
}

// Test_Helm_CrossTech_FromOCICoordinate proves the Helm placer consumes an OCI-produced coordinate.
func Test_Helm_CrossTech_FromOCICoordinate(t *testing.T) {
	r := require.New(t)
	fromOCI := &descriptor.Coordinate{Path: "acme/podinfo", Version: "6.7.1"}
	out, err := New(Config{HelmRepository: "https://mirror.example.com"}).FromCoordinate(fromOCI)
	r.NoError(err)
	got := out.(*v1.Helm)
	r.Equal("acme/podinfo:6.7.1", got.HelmChart)
}
