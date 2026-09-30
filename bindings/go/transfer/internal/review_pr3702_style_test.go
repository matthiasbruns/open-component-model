package internal

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// expensiveExpr nests `depth` comprehensions over a 10-element list: 10^depth iterations.
func expensiveExpr(depth int) string {
	list := "[0, 1, 2, 3, 4, 5, 6, 7, 8, 9]"
	expr := "true"
	for i := range depth {
		expr = fmt.Sprintf("%s.all(v%d, %s)", list, i, expr)
	}
	return expr
}

// Uploader match and imageReference expressions come from user-supplied OCM config; since this
// PR the controller admits oci/localblob/reference uploader entries and evaluates them during
// reconcile. They must be bounded like the controller's other user CEL
// (kubernetes/controller/internal/discovery/query.go: cel.CostLimit + InterruptCheckFrequency),
// so a pathological expression is rejected quickly instead of pinning a reconcile worker.
func TestReviewPR3702_S1_UploaderCELHasNoCostLimit(t *testing.T) {
	desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{
		ociImageResource("image", "1.0.0", "ghcr.io/org/image:v1"),
	}, nil)
	v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		eval func(env *uploaderEnv, aliases map[string]string) error
	}{
		{
			name: "match",
			eval: func(env *uploaderEnv, aliases map[string]string) error {
				_, err := matches(expensiveExpr(9), aliases, env)
				return err
			},
		},
		{
			name: "imageReference",
			eval: func(env *uploaderEnv, aliases map[string]string) error {
				u := &transferv1alpha1.OCIUploaderConfig{
					ImageReference: "${" + expensiveExpr(9) + " ? \"ghcr.io/x/y:1\" : \"\"}",
				}
				_, err := ociImageReference(u, aliases, env)
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			tgd := &transformv1alpha1.TransformationGraphDefinition{Environment: &runtime.Unstructured{Data: map[string]any{}}}
			r.NoError(addDescriptorToEnvironment(v2desc, "base", tgd))
			env := &uploaderEnv{baseID: "base", node: tgd.Environment.Data["base"]}
			aliases, err := uploaderAliases(env, 0, testOCIRepo("ghcr.io/target"))
			r.NoError(err)

			done := make(chan error, 1)
			go func() { done <- tc.eval(env, aliases) }()
			select {
			case err := <-done:
				r.Error(err, "a 10^9-iteration expression must be rejected by a cost limit")
				r.True(strings.Contains(strings.ToLower(err.Error()), "cost"), "expected a cost-limit error, got: %v", err)
			case <-time.After(5 * time.Second):
				r.Fail("uploader CEL evaluation is unbounded: still running after 5s (no cel.CostLimit / InterruptCheckFrequency)")
			}
		})
	}
}
