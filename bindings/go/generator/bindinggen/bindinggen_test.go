package bindinggen

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_Util(t *testing.T) {
	b, err := NewBinding(Options{Name: "widget", Kinds: []Kind{KindUtil}})
	require.NoError(t, err)

	dir := t.TempDir()
	written, err := Generate(b, dir)
	require.NoError(t, err)

	assertContains(t, written, "doc.go", "go.mod", "Taskfile.yml", "widget.go", "widget_test.go",
		"integration/go.mod", "integration/Taskfile.yml", "integration/integration_test.go")
	// A util binding must not scaffold a spec tree.
	for _, w := range written {
		assert.False(t, strings.HasPrefix(w, "spec/"), "util binding should not produce %s", w)
	}
	assertAllGoFilesParse(t, dir, written)
	assertModulePath(t, dir, "ocm.software/open-component-model/bindings/go/widget")
}

func TestGenerate_AccessInputCredentials(t *testing.T) {
	b, err := NewBinding(Options{
		Name:        "demo",
		Spec:        "Demo",
		Kinds:       []Kind{KindAccess, KindInput},
		Credentials: true,
	})
	require.NoError(t, err)

	dir := t.TempDir()
	written, err := Generate(b, dir)
	require.NoError(t, err)

	assertContains(t, written,
		"spec/access/scheme.go", "spec/access/v1/group_version.go", "spec/access/v1/demo.go",
		"repository/options.go", "repository/resource_repository.go", "repository/resource_repository_test.go",
		"spec/input/scheme.go", "spec/input/v1/demo.go", "input/method.go", "input/method_test.go",
		"spec/credentials/scheme.go", "spec/credentials/v1/demo_credentials.go",
		"integration/go.mod", "integration/integration_test.go",
	)
	assertAllGoFilesParse(t, dir, written)
	assertModulePath(t, dir, "ocm.software/open-component-model/bindings/go/demo")

	// integration module replaces the parent.
	intMod := readFile(t, filepath.Join(dir, "integration", "go.mod"))
	assert.Contains(t, intMod, "replace ocm.software/open-component-model/bindings/go/demo => ../")
}

func TestGenerate_RefusesNonEmptyDir(t *testing.T) {
	b, err := NewBinding(Options{Name: "demo", Kinds: []Kind{KindUtil}})
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o644))
	_, err = Generate(b, dir)
	require.Error(t, err)
}

func TestNewBinding_Validation(t *testing.T) {
	_, err := NewBinding(Options{Name: "", Kinds: []Kind{KindUtil}})
	require.Error(t, err)

	_, err = NewBinding(Options{Name: "x", Kinds: []Kind{KindUtil, KindAccess}})
	require.Error(t, err, "util must not combine with access")

	_, err = NewBinding(Options{Name: "x", Kinds: []Kind{KindUtil}, Credentials: true})
	require.Error(t, err, "util cannot have credentials")

	b, err := NewBinding(Options{Name: "wget", Kinds: []Kind{KindAccess}})
	require.NoError(t, err)
	assert.Equal(t, "Wget", b.Spec, "spec defaults to title-cased name")
	assert.Equal(t, "v1", b.Version)
}

func TestNewBinding_SpecUpperCamelCase(t *testing.T) {
	valid := map[string]string{
		"s3":       "S3",       // lower-case input canonicalized
		"S3":       "S3",       // already canonical
		"maven":    "Maven",    // first letter upper-cased
		"OCIImage": "OCIImage", // internal capitals/acronyms preserved
	}
	for in, want := range valid {
		b, err := NewBinding(Options{Name: "x", Spec: in, Kinds: []Kind{KindAccess}})
		require.NoError(t, err, "spec %q", in)
		assert.Equal(t, want, b.Spec, "spec %q", in)
		assert.Equal(t, want, b.ConsumerType(), "consumer type for %q", in)
	}

	// Anything other than letters and digits is rejected.
	for _, in := range []string{"oci-image", "my_thing", "s3.store", "a b", "3d"} {
		_, err := NewBinding(Options{Name: "x", Spec: in, Kinds: []Kind{KindAccess}})
		require.Error(t, err, "spec %q must be rejected", in)
	}

	// A lower-case binding name also yields an UpperCamelCase default spec.
	b, err := NewBinding(Options{Name: "s3", Kinds: []Kind{KindAccess}})
	require.NoError(t, err)
	assert.Equal(t, "S3", b.Spec)
}

func TestPatchRootTaskfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Taskfile.yml")
	original := strings.Join([]string{
		"version: '3'",
		"",
		"includes:",
		"  bindings/go/aaa:",
		"    optional: true",
		"    taskfile: ./bindings/go/aaa/Taskfile.yml",
		"    dir: ./bindings/go/aaa",
		"  bindings/go/zzz:",
		"    optional: true",
		"    taskfile: ./bindings/go/zzz/Taskfile.yml",
		"    dir: ./bindings/go/zzz",
		"",
		"tasks:",
		"  test:",
		"    cmds: []",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	changed, err := PatchRootTaskfile(path, "bindings/go/demo")
	require.NoError(t, err)
	assert.True(t, changed)

	got := readFile(t, path)
	// Inserted alphabetically between aaa and zzz, both keys present, before tasks:.
	assert.Contains(t, got, "  bindings/go/demo:")
	assert.Contains(t, got, "  bindings/go/demo/integration:")
	assert.Less(t, strings.Index(got, "bindings/go/demo:"), strings.Index(got, "bindings/go/zzz:"))
	assert.Greater(t, strings.Index(got, "bindings/go/demo:"), strings.Index(got, "bindings/go/aaa:"))
	assert.Less(t, strings.Index(got, "bindings/go/demo/integration:"), strings.Index(got, "tasks:"))

	// Idempotent: a second run is a no-op.
	changed2, err := PatchRootTaskfile(path, "bindings/go/demo")
	require.NoError(t, err)
	assert.False(t, changed2)
	assert.Equal(t, got, readFile(t, path))
}

func assertContains(t *testing.T, written []string, want ...string) {
	t.Helper()
	set := make(map[string]bool, len(written))
	for _, w := range written {
		set[w] = true
	}
	for _, w := range want {
		assert.True(t, set[w], "expected generated file %s; got %v", w, written)
	}
}

func assertAllGoFilesParse(t *testing.T, dir string, written []string) {
	t.Helper()
	fset := token.NewFileSet()
	for _, w := range written {
		if !strings.HasSuffix(w, ".go") {
			continue
		}
		_, err := parser.ParseFile(fset, filepath.Join(dir, w), nil, parser.AllErrors)
		assert.NoError(t, err, "generated %s should parse", w)
	}
}

func assertModulePath(t *testing.T, dir, want string) {
	t.Helper()
	mod := readFile(t, filepath.Join(dir, "go.mod"))
	assert.Contains(t, mod, "module "+want)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
