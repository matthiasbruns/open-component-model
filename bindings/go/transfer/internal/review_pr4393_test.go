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

// TestReviewPR4393_F3_ReferencedChildChartThatCannotKeepItsDigestFailsUpFront: a parent records
// the digest of a child whose Helm chart comes from an oci:// chart repository, so the chart
// digest is ociArtifactDigest/v1 (what `ocm add cv` records for such a chart). Converting the
// chart changes that digest and with it the child descriptor, so the copy breaks the parent's
// reference digest (and the signature of a signed parent that covers it). For a signed child the
// PR fails such a copy up front; a child covered by a reference digest must not be converted
// silently either.
func TestReviewPR4393_F3_ReferencedChildChartThatCannotKeepItsDigestFailsUpFront(t *testing.T) {
	r := require.New(t)
	const parentName = "ocm.software/parent"
	ociChartDigest := &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: strings.Repeat("b", 64)}
	child := helmDigestDescriptor(ociChartDigest, false, "oci://registry.example/charts")

	dig, err := signing.GenerateDigest(t.Context(), child, slog.New(slog.DiscardHandler), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	parent := testDescriptor(parentName, helmDigestVersion, nil, []descriptor.Reference{{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: helmDigestVersion}},
		Component:   helmDigestComponent,
		Digest:      *dig,
	}})
	source := testOCIRepo("ghcr.io/source")
	resolver := testMultiResolver(map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{
		parentName + ":" + helmDigestVersion:          {spec: source, desc: parent},
		helmDigestComponent + ":" + helmDigestVersion: {spec: source, desc: child},
	})
	roots := testTransferRoots(parentName, helmDigestVersion, testOCIRepo("ghcr.io/target"), resolver)

	_, err = BuildGraphDefinition(t.Context(), roots,
		transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, withLocalBlobUploader())
	r.Error(err, "the chart would be converted and the parent's reference digest would no longer match the copied child")
}
