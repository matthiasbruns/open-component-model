package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/oras-project/oras-go/v3/registry/remote/properties"
	"github.com/stretchr/testify/require"
)

// baseSnapshot is a refs/<sha256(namespace)>.json snapshot produced verbatim by
// the commit before the oras-go v3 migration (c1f262431a), where the LRU key
// came from oras v2's registry.Reference.Reference. The filename is
// sha256("ghcr.io/acme/app").
const (
	baseSnapshotName = "5aaba65fadaf3a9e139f75dac40ab5021fea203f699d0cbaf73de0cc7286fa35.json"
	baseSnapshotBody = `{"namespace":"ghcr.io/acme/app","references":{` +
		`"sha256:05b3abf2579a5eb66403cd78be557fd860633a1fe2103c7642030defe32c657f":` +
		`{"descriptor":{"mediaType":"application/vnd.oci.image.manifest.v1+json",` +
		`"digest":"sha256:05b3abf2579a5eb66403cd78be557fd860633a1fe2103c7642030defe32c657f","size":11},` +
		`"savedAt":"2525-01-01T00:00:00Z"},` +
		`"v1.0.0":{"descriptor":{"mediaType":"application/vnd.oci.image.manifest.v1+json",` +
		`"digest":"sha256:05b3abf2579a5eb66403cd78be557fd860633a1fe2103c7642030defe32c657f","size":11},` +
		`"savedAt":"2525-01-01T00:00:00Z"}}}`
)

// TestReviewPR4221_RefCacheReadsPreMigrationSnapshot checks the PR's claim that
// the reference cache's on-disk snapshot format is unchanged: an entry written
// before the v3 migration must still be found, under the same key, with the
// same descriptor. A tag+digest reference must key off the digest, as it did
// when the key came from registry.Reference.Reference.
func TestReviewPR4221_RefCacheReadsPreMigrationSnapshot(t *testing.T) {
	r := require.New(t)

	dir := t.TempDir()
	refs := filepath.Join(dir, "refs")
	r.NoError(os.MkdirAll(refs, 0o755))
	r.NoError(os.WriteFile(filepath.Join(refs, baseSnapshotName), []byte(baseSnapshotBody), 0o600))

	c, err := NewReferenceCache(Options{Dir: dir})
	r.NoError(err)

	dgst := digest.FromString("manifest")
	base := properties.Reference{Registry: "ghcr.io", Repository: "acme/app"}

	tagRef := base
	tagRef.Tag = "v1.0.0"
	got, ok := c.Lookup(tagRef)
	r.True(ok, "tag entry from a pre-migration snapshot must still be found")
	r.Equal(dgst, got.Digest)

	digestRef := base
	digestRef.Digest = dgst.String()
	got, ok = c.Lookup(digestRef)
	r.True(ok, "digest entry from a pre-migration snapshot must still be found")
	r.Equal(dgst, got.Digest)

	bothRef := base
	bothRef.Tag, bothRef.Digest = "v1.0.0", dgst.String()
	got, ok = c.Lookup(bothRef)
	r.True(ok, "tag+digest must resolve via the digest key")
	r.Equal(dgst, got.Digest)

	unwritten := base
	unwritten.Tag = "v2.0.0"
	_, ok = c.Lookup(unwritten)
	r.False(ok, "an unwritten tag must not hit")

	otherNamespace := properties.Reference{Registry: "ghcr.io", Repository: "other/app", Tag: "v1.0.0"}
	_, ok = c.Lookup(otherNamespace)
	r.False(ok, "a different namespace must not hit")
}
