package internal

import (
	"context"
	"crypto"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// slowChildResolver looks up the child's repository only after the parent's references
// were discovered, which is one legal ordering of the concurrent graph discovery.
type slowChildResolver struct {
	*mockCVRepoResolver
	childSpec runtime.Typed
}

func (r *slowChildResolver) GetComponentVersionRepositoryForSpecification(ctx context.Context, spec runtime.Typed) (repository.ComponentVersionRepository, error) {
	if spec == r.childSpec {
		time.Sleep(300 * time.Millisecond)
	}
	return r.mockCVRepoResolver.GetComponentVersionRepositoryForSpecification(ctx, spec)
}

// A root pinned by the user must be verified against that pin, even when another
// (unpinned) root references the same component with a digest of its own.
func TestReviewPR3889_F1_RootPinOverwrittenByParentReference(t *testing.T) {
	r := require.New(t)
	child := testDescriptor("ocm.software/child", "1.0.0", nil, nil)
	childDigest, err := signing.GenerateDigest(t.Context(), child, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)

	parent := testDescriptor("ocm.software/parent", "1.0.0", nil, []descriptor.Reference{{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: "1.0.0"}},
		Component:   "ocm.software/child",
		Digest:      *childDigest,
	}})

	childSpec := testOCIRepo("ghcr.io/source")
	resolver := &slowChildResolver{
		mockCVRepoResolver: testMultiResolver(map[string]struct {
			spec runtime.Typed
			desc *descriptor.Descriptor
		}{
			"ocm.software/parent:1.0.0": {spec: testOCIRepo("ghcr.io/source"), desc: parent},
			"ocm.software/child:1.0.0":  {spec: childSpec, desc: child},
		}),
		childSpec: childSpec,
	}

	// The user pins the child to a digest it does not have.
	wrongPin := *childDigest
	wrongPin.Value = strings.Repeat("0", len(childDigest.Value))

	target := testOCIRepo("ghcr.io/target")
	roots := map[string]TransferRoot{
		"ocm.software/parent:1.0.0": {RootComponentKey: "ocm.software/parent:1.0.0", Targets: []runtime.Typed{target}, SourceResolver: resolver},
		"ocm.software/child:1.0.0":  {RootComponentKey: "ocm.software/child:1.0.0", Targets: []runtime.Typed{target}, SourceResolver: resolver, ExpectedDigest: &wrongPin},
	}

	_, err = BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, nil)
	r.Error(err, "the child root does not match the digest the user pinned it to")
}
