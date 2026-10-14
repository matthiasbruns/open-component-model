package internal

import (
	"crypto"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// A parent whose reference expects a wrong digest for a child must fail the transfer,
// also when that parent is discovered only after the child was already resolved and
// verified against another parent's (correct) digest.
func TestReviewPR3889_F5_LateParentReferenceDigestNotVerified(t *testing.T) {
	r := require.New(t)
	child := testDescriptor("ocm.software/child", "1.0.0", nil, nil)
	goodDigest, err := signing.GenerateDigest(t.Context(), child, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	wrongDigest := *goodDigest
	wrongDigest.Value = strings.Repeat("0", len(goodDigest.Value))

	parent := func(name string, dig descriptor.Digest) *descriptor.Descriptor {
		return testDescriptor(name, "1.0.0", nil, []descriptor.Reference{{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: "1.0.0"}},
			Component:   "ocm.software/child",
			Digest:      dig,
		}})
	}
	lateParentSpec := testOCIRepo("ghcr.io/source")
	resolver := &slowChildResolver{
		mockCVRepoResolver: testMultiResolver(map[string]struct {
			spec runtime.Typed
			desc *descriptor.Descriptor
		}{
			"ocm.software/a:1.0.0":     {spec: testOCIRepo("ghcr.io/source"), desc: parent("ocm.software/a", *goodDigest)},
			"ocm.software/b:1.0.0":     {spec: lateParentSpec, desc: parent("ocm.software/b", wrongDigest)},
			"ocm.software/child:1.0.0": {spec: testOCIRepo("ghcr.io/source"), desc: child},
		}),
		// delay parent b, so the child is resolved (and verified against a's digest) first
		childSpec: lateParentSpec,
	}
	target := testOCIRepo("ghcr.io/target")
	roots := map[string]TransferRoot{
		"ocm.software/a:1.0.0": {RootComponentKey: "ocm.software/a:1.0.0", Targets: []runtime.Typed{target}, SourceResolver: resolver},
		"ocm.software/b:1.0.0": {RootComponentKey: "ocm.software/b:1.0.0", Targets: []runtime.Typed{target}, SourceResolver: resolver},
	}
	_, err = BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, nil)
	r.Error(err, "b references the child with a digest that does not match it")
}
