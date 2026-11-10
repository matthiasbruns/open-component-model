package ctf_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
)

// TestReviewPR3942_F1_CTFWritesStayInsideStore drives the same confinement
// question through the public CTF API: a CTF is opened on a directory, that
// directory is replaced by a symlink, and the already-open CTF writes a blob.
// The blob must not land outside the directory the CTF was opened on.
//
// Passes on 34a53d964f (merge base), fails on 7a00d1824607 (PR head).
func TestReviewPR3942_F1_CTFWritesStayInsideStore(t *testing.T) {
	r := require.New(t)
	parent := t.TempDir()
	store := filepath.Join(parent, "transport.ctf")
	outside := filepath.Join(parent, "outside")
	r.NoError(os.MkdirAll(outside, 0o755))

	archive, err := ctf.OpenCTFFromOSPath(store, ctf.O_RDWR|ctf.O_CREATE)
	r.NoError(err)

	// First operation: an ordinary blob write into the CTF.
	r.NoError(archive.SaveBlob(t.Context(), inmemory.New(strings.NewReader("first"))))

	// The CTF directory is replaced by a symlink to a directory outside it.
	r.NoError(os.Rename(store, filepath.Join(parent, "transport.ctf.moved")))
	r.NoError(os.Symlink(outside, store))

	// Second operation on the same open CTF.
	saveErr := archive.SaveBlob(t.Context(), inmemory.New(strings.NewReader("second-blob-content")))

	leaked, err := filepath.Glob(filepath.Join(outside, "blobs", "*"))
	r.NoError(err)
	if len(leaked) == 0 {
		return // confined: nothing was written outside the store
	}
	content, readErr := os.ReadFile(leaked[0])
	r.NoError(readErr)
	t.Fatalf("escaped confinement: CTF wrote %q into %s, outside the store it was opened on (SaveBlob err: %v)",
		string(content), filepath.Dir(leaked[0]), saveErr)
}
