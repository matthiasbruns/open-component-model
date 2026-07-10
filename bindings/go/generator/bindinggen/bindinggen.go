package bindinggen

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates
var templatesFS embed.FS

// tmpl holds every parsed template. Templates are grouped into a handful of files,
// each defining named blocks ({{define "name"}}) addressed by ExecuteTemplate.
var tmpl = template.Must(template.New("bindinggen").ParseFS(templatesFS, "templates/*.tmpl"))

// genFile binds a named template block to the output path it renders to and, for the
// spec-tree blocks shared between access and input, the layer they render for.
type genFile struct {
	def   string // named template block
	out   string // output path relative to the binding directory
	layer string // "access" or "input" for shared spec-tree blocks; empty otherwise
}

// view is the data passed to each template: the Binding plus the layer context used
// by the shared scheme.go/spec.go blocks.
type view struct {
	*Binding
	Layer     string // "access" | "input" | ""
	LayerNoun string // "access type" | "input method" | ""
}

// plan returns the ordered set of blocks to render for this binding, with output
// paths relative to the binding directory. The scheme.go, group_version.go and
// spec.go blocks are shared between the access and input layers.
func (b *Binding) plan() []genFile {
	files := []genFile{
		{def: "doc.go", out: "doc.go"},
		{def: "go.mod", out: "go.mod"},
		{def: "Taskfile.yml", out: "Taskfile.yml"},
	}

	if b.Util {
		files = append(files,
			genFile{def: "util.go", out: b.Package + ".go"},
			genFile{def: "util_test.go", out: b.Package + "_test.go"},
		)
	}

	files = append(files, b.specTree("access", b.Access)...)
	files = append(files, b.specTree("input", b.Input)...)

	if b.Access {
		files = append(files,
			genFile{def: "options.go", out: "repository/options.go"},
			genFile{def: "resource_repository.go", out: "repository/resource_repository.go"},
			genFile{def: "resource_repository_test.go", out: "repository/resource_repository_test.go"},
		)
	}

	if b.Input {
		files = append(files,
			genFile{def: "method.go", out: "input/method.go"},
			genFile{def: "method_test.go", out: "input/method_test.go"},
		)
	}

	if b.Credentials {
		files = append(files,
			genFile{def: "credentials_scheme.go", out: "spec/credentials/scheme.go"},
			genFile{def: "group_version.go", out: "spec/credentials/" + b.Version + "/group_version.go"},
			genFile{def: "credentials.go", out: "spec/credentials/" + b.Version + "/" + b.LowerSpec + "_credentials.go"},
		)
	}

	// The integration sub-module is always scaffolded so CI discovers and runs it.
	files = append(files,
		genFile{def: "integration.go.mod", out: "integration/go.mod"},
		genFile{def: "integration.Taskfile.yml", out: "integration/Taskfile.yml"},
		genFile{def: "integration_test.go", out: "integration/integration_test.go"},
	)

	return files
}

// specTree returns the shared scheme/group_version/spec blocks for a layer
// ("access" or "input") when enabled.
func (b *Binding) specTree(layer string, enabled bool) []genFile {
	if !enabled {
		return nil
	}
	return []genFile{
		{def: "scheme.go", out: "spec/" + layer + "/scheme.go", layer: layer},
		{def: "group_version.go", out: "spec/" + layer + "/" + b.Version + "/group_version.go"},
		{def: "spec.go", out: "spec/" + layer + "/" + b.Version + "/" + b.LowerSpec + ".go", layer: layer},
	}
}

// Render produces the file contents for the binding, keyed by output path relative
// to the binding directory. Go sources are gofmt-formatted; a formatting failure is
// returned as an error so template bugs surface immediately.
func (b *Binding) Render() (map[string][]byte, error) {
	out := make(map[string][]byte)
	for _, f := range b.plan() {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, f.def, b.viewFor(f.layer)); err != nil {
			return nil, fmt.Errorf("rendering %s: %w", f.def, err)
		}
		content := buf.Bytes()
		if strings.HasSuffix(f.out, ".go") {
			formatted, err := format.Source(content)
			if err != nil {
				return nil, fmt.Errorf("formatting %s: %w", f.out, err)
			}
			content = formatted
		}
		out[f.out] = content
	}
	return out, nil
}

func (b *Binding) viewFor(layer string) view {
	v := view{Binding: b, Layer: layer}
	switch layer {
	case "access":
		v.LayerNoun = "access type"
	case "input":
		v.LayerNoun = "input method"
	}
	return v
}

// Generate renders the binding and writes it under dir. It refuses to overwrite an
// existing, non-empty binding directory. It returns the sorted list of written
// files (relative to dir).
func Generate(b *Binding, dir string) ([]string, error) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("target directory %s already exists and is not empty", dir)
	}

	rendered, err := b.Render()
	if err != nil {
		return nil, err
	}

	written := make([]string, 0, len(rendered))
	for rel, content := range rendered {
		dst := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("creating directory for %s: %w", rel, err)
		}
		if err := os.WriteFile(dst, content, 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", rel, err)
		}
		written = append(written, rel)
	}
	sortStrings(written)
	return written, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
