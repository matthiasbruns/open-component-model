package v4alpha2_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha2"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// The real-world legacy v3 fixture (ocmcli 0.27.0, `ocm hash cv --normalization
// jsonNormalisation/v3 -onorm`) is only exercised under v4alpha1. Pin the new
// alias target against it too.
func TestReviewPR4006_LegacyV3ConformanceFixture(t *testing.T) {
	r := require.New(t)
	dir := filepath.Join("..", "v4alpha1", "testdata", "conformance", "legacy", "jsonNormalisation", "v3", "01")
	input, err := os.ReadFile(filepath.Join(dir, "input.yaml"))
	r.NoError(err)
	expected, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	r.NoError(err)

	var v2desc descriptorv2.Descriptor
	r.NoError(yaml.Unmarshal(input, &v2desc))
	desc, err := descruntime.ConvertFromV2(&v2desc)
	r.NoError(err)

	got, err := normalisation.Normalise(desc, v4alpha2.Algorithm)
	r.NoError(err)
	r.JSONEq(string(expected), string(got))
}
