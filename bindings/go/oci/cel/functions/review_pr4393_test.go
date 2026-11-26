package functions_test

import (
	"bytes"
	"log/slog"
	"reflect"
	"testing"

	"cel.dev/cel-go/common/types"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/cel/functions"
)

// TestReviewPR4393_F4_ToOCIResolvesChartLayerAccessWithoutDeprecatedFallback feeds toOCI() the
// ociBlob/v1 access that `ocm transfer cv --copy-resources --upload-as ociArtifact` writes for a
// signed Helm chart (copied from a real run of the PR head). The PR adds imageReference so that "a
// deployer finds" the Helm image; toOCI() must resolve it as a typed ociBlob/v1 access rather than
// through the deprecated untyped imageReference fallback (ocm-project#960), which is slated for
// removal.
func TestReviewPR4393_F4_ToOCIResolvesChartLayerAccessWithoutDeprecatedFallback(t *testing.T) {
	r := require.New(t)

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	access := map[string]any{
		"type":           "ociBlob/v1",
		"ref":            "ghcr.io/acme/ocm/mychart",
		"mediaType":      "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
		"digest":         "sha256:c68fb36429431f1bf40e539e52d93e49d41b7ab9a6eaceba43e103ca7043bfcb",
		"size":           int64(467),
		"imageReference": "ghcr.io/acme/ocm/mychart:0.1.0@sha256:bcb6055348e409f296e9e1ccf00af5a8ad27017a46ba9c1f38edfae1822f749c",
	}
	out := functions.BindingToOCI()(types.DefaultTypeAdapter.NativeToValue(access))
	r.False(types.IsError(out), "toOCI() failed: %v", out)

	got, ok := out.Value().(map[string]string)
	if !ok {
		native, err := out.ConvertToNative(reflect.TypeFor[map[string]string]())
		r.NoError(err)
		got = native.(map[string]string)
	}
	r.Equal("mychart", got["repository"][len(got["repository"])-len("mychart"):])
	r.Equal("sha256:bcb6055348e409f296e9e1ccf00af5a8ad27017a46ba9c1f38edfae1822f749c", got["digest"], "toOCI() must name the Helm image manifest")
	r.NotContains(logs.String(), "falling back to untyped 'imageReference' field",
		"ociBlob/v1 is resolved only through the deprecated untyped fallback")
}
