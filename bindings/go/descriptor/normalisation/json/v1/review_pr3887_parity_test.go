package v1_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

func TestReviewPR3887_CCUtilsEdges(t *testing.T) {
	for _, name := range []string{"list-order"} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			root := filepath.Join("testdata", "review-pr3887-list-order")
			input, err := os.ReadFile(filepath.Join(root, "input.json"))
			r.NoError(err)
			expected, err := os.ReadFile(filepath.Join(root, "expected.json"))
			r.NoError(err)
			var stored v2.Descriptor
			r.NoError(yaml.Unmarshal(input, &stored))
			desc, err := descruntime.ConvertFromV2(&stored)
			r.NoError(err)
			actual, err := normalisation.Normalise(desc, v1.Algorithm)
			r.NoError(err)
			r.Equal(string(expected), string(actual))
		})
	}
}
