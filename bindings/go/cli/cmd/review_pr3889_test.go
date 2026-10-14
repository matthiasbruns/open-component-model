package cmd_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
	"ocm.software/open-component-model/bindings/go/ctf"
	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/signing"
)

// A CTF holds a child and two parents referencing it: one with the digest OCM v1 writes
// (jsonNormalisation/v3, which v2 verifies as v4alpha1), one with the v2 digest. Both digests
// are valid for the child, so transferring the repository must succeed.
func TestReviewPR3889_F2_TransferMixedLegacyReferenceDigests(t *testing.T) {
	r := require.New(t)
	archive := filepath.Join(t.TempDir(), "archive")
	fs, err := filesystem.NewFS(archive, os.O_RDWR|os.O_CREATE)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)

	cv := func(name string, refs ...descriptor.Reference) *descriptor.Descriptor {
		return &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: "1.0.0"}},
				Provider:      descriptor.Provider{Name: "ocm.software"},
				References:    refs,
			},
		}
	}
	ref := func(dig descriptor.Digest) descriptor.Reference {
		return descriptor.Reference{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: "1.0.0"}},
			Component:   "ocm.software/child",
			Digest:      dig,
		}
	}

	child := cv("ocm.software/child")
	r.NoError(repo.AddComponentVersion(t.Context(), child))
	v2Digest, err := signing.GenerateDigest(t.Context(), child, slog.Default(), v4alpha1.Algorithm, "SHA-256")
	r.NoError(err)
	legacyDigest := *v2Digest
	legacyDigest.NormalisationAlgorithm = signing.LegacyNormalisationAlgo

	r.NoError(repo.AddComponentVersion(t.Context(), cv("ocm.software/parent-legacy", ref(legacyDigest))))
	r.NoError(repo.AddComponentVersion(t.Context(), cv("ocm.software/parent-v2", ref(*v2Digest))))

	target := filepath.Join(t.TempDir(), "target")
	_, err = test.OCM(t,
		test.WithArgs("transfer", "component-version", archive, target, "--recursive"),
		test.WithOutput(new(bytes.Buffer)),
	)
	r.NoError(err, "both parents' reference digests are valid for the child")
}
