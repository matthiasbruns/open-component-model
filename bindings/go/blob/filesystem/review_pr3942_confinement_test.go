package filesystem_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// TestReviewPR3942_F1_RootFileSystemPinsBase checks that a RootFileSystem keeps
// operating on the directory it was opened on, even when that directory is
// replaced by a symlink while the filesystem is in use.
//
// Passes on 34a53d964f (merge base), fails on 7a00d1824607 (PR head).
func TestReviewPR3942_F1_RootFileSystemPinsBase(t *testing.T) {
	r := require.New(t)
	parent := t.TempDir()
	base := filepath.Join(parent, "base")
	outside := filepath.Join(parent, "outside")
	r.NoError(os.MkdirAll(base, 0o755))
	r.NoError(os.MkdirAll(outside, 0o755))
	r.NoError(os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside-content"), 0o644))

	fsys, err := filesystem.NewFS(base, os.O_RDWR)
	r.NoError(err)

	// First operation: the filesystem resolves against the real base directory.
	r.NoError(os.WriteFile(filepath.Join(base, "inside.txt"), []byte("inside"), 0o644))
	f, err := fsys.Open("inside.txt")
	r.NoError(err)
	content, err := io.ReadAll(f)
	r.NoError(err)
	r.NoError(f.Close())
	r.Equal("inside", string(content))

	// base is replaced by a symlink to a directory outside the tree, which
	// anyone with write access to parent can do between two operations.
	r.NoError(os.Rename(base, filepath.Join(parent, "base.moved")))
	r.NoError(os.Symlink(outside, base))

	// Second operation on the same filesystem: it must not follow the swap.
	escaped, err := fsys.Open("secret.txt")
	if err != nil {
		return // confined: the filesystem still refers to the original directory
	}
	leaked, readErr := io.ReadAll(escaped)
	r.NoError(readErr)
	r.NoError(escaped.Close())
	t.Fatalf("escaped confinement: read %q from %s, outside the directory the filesystem was opened on", string(leaked), outside)
}
