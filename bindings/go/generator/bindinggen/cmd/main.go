// Command bindinggen scaffolds a new binding under bindings/go in the canonical
// repository layout, wires it into the root Taskfile, and (unless disabled) runs
// scoped code generation for the new module.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/generator/bindinggen"
)

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type flags struct {
	kind         string
	name         string
	spec         string
	version      string
	credentials  bool
	dir          string
	modulePrefix string
	root         string
	wireTaskfile bool
	tidy         bool
	generate     bool
	dryRun       bool
}

func newCommand() *cobra.Command {
	f := &flags{}
	cmd := &cobra.Command{
		Use:   "bindinggen <kind> <name>",
		Short: "Scaffold a new binding under bindings/go",
		Long: "bindinggen scaffolds a new binding in the canonical repository layout: a Go module,\n" +
			"a spec/<access|input> tree, a repository or input method, an integration sub-module,\n" +
			"and the wiring into the root Taskfile. Run `task generate` afterwards for zz_generated files.\n\n" +
			"<kind> is a comma-separated list of util, access and/or input (e.g. \"access,input\").\n" +
			"<name> is the binding name / directory / package (e.g. wget).",
		Example: "  bindinggen access,input wget --spec Wget --credentials\n" +
			"  bindinggen util semver",
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.kind = args[0]
			f.name = args[1]
			return run(f)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&f.spec, "spec", "", "spec type name (default: title-cased name), e.g. Wget")
	fs.StringVar(&f.version, "version", "v1", "spec API version directory (e.g. v1, v1alpha1)")
	fs.BoolVar(&f.credentials, "credentials", false, "scaffold a credentials spec")
	fs.StringVar(&f.dir, "dir", "", "output directory (default: <root>/bindings/go/<name>)")
	fs.StringVar(&f.modulePrefix, "module-prefix", bindinggen.DefaultModulePrefix, "go module path prefix")
	fs.StringVar(&f.root, "root", "", "repository root (default: auto-discovered)")
	fs.BoolVar(&f.wireTaskfile, "wire-taskfile", true, "insert include entries into the root Taskfile")
	fs.BoolVar(&f.tidy, "tidy", true, "run `go mod tidy` in the new modules")
	fs.BoolVar(&f.generate, "generate", true, "run scoped code generation (ocmtypegen, deepcopy-gen, jsonschemagen) for the new binding after scaffolding")
	fs.BoolVar(&f.dryRun, "dry-run", false, "print what would be generated without writing")

	return cmd
}

func run(f *flags) error {
	kinds, err := parseKinds(f.kind)
	if err != nil {
		return err
	}

	b, err := bindinggen.NewBinding(bindinggen.Options{
		Name:         f.name,
		Spec:         f.spec,
		Version:      f.version,
		Kinds:        kinds,
		Credentials:  f.credentials,
		ModulePrefix: f.modulePrefix,
	})
	if err != nil {
		return err
	}

	root, err := resolveRoot(f.root)
	if err != nil {
		return err
	}

	outDir := f.dir
	if outDir == "" {
		outDir = filepath.Join(root, "bindings", "go", b.Name)
	}
	outDir, err = filepath.Abs(outDir)
	if err != nil {
		return err
	}
	relDir, err := filepath.Rel(root, outDir)
	if err != nil {
		return fmt.Errorf("binding dir must be inside the repository root: %w", err)
	}
	relDir = filepath.ToSlash(relDir)

	// Wiring writes relDir as a root-Taskfile include key, so it must be a real
	// in-tree binding path. Scaffolding outside the tree is allowed only without wiring.
	if f.wireTaskfile {
		if strings.HasPrefix(relDir, "..") {
			return fmt.Errorf("output dir %s is outside the repository root %s; pass --wire-taskfile=false to scaffold outside the tree", outDir, root)
		}
		if !strings.HasPrefix(relDir, "bindings/go/") {
			return fmt.Errorf("output dir %s is not under bindings/go; pass --wire-taskfile=false to scaffold elsewhere", relDir)
		}
	}

	if f.dryRun {
		return dryRun(b, relDir)
	}

	written, err := bindinggen.Generate(b, outDir)
	if err != nil {
		return err
	}
	fmt.Printf("Scaffolded %s (%d files) at %s\n", b.ModulePath, len(written), relDir)
	for _, w := range written {
		fmt.Printf("  %s\n", filepath.ToSlash(filepath.Join(relDir, w)))
	}

	if f.wireTaskfile {
		changed, err := bindinggen.PatchRootTaskfile(filepath.Join(root, "Taskfile.yml"), relDir)
		if err != nil {
			return fmt.Errorf("wiring root Taskfile: %w", err)
		}
		if changed {
			fmt.Println("Wired includes into root Taskfile.yml")
		} else {
			fmt.Println("Root Taskfile.yml already contains the includes")
		}
	}

	// A util binding has no spec types, so there is nothing to generate. Codegen
	// needs the module's dependencies resolved, so tidy first whenever either runs.
	codegen := f.generate && !b.Util
	if f.tidy || codegen {
		tidyModule(outDir)
		tidyModule(filepath.Join(outDir, "integration"))
	}

	generated := false
	if codegen {
		if err := runCodegen(root, outDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: code generation failed (run `task generate` manually): %v\n", err)
		} else {
			generated = true
			tidyModule(outDir) // refresh go.sum after the generated files land
		}
	}

	printNextSteps(b, relDir, generated)
	return nil
}

// runCodegen generates the zz_generated.* files for just the new binding, in the
// required order (ocmtypegen -> deepcopy-gen -> jsonschemagen), rather than the
// repo-wide `task generate` which would also regenerate unrelated CLI docs and CRD
// manifests and tidy every module.
func runCodegen(root, bindingDir string) error {
	generatorDir := filepath.Join(root, "bindings", "go", "generator")
	fmt.Println("\nGenerating code for the new binding...")

	if err := execStream(generatorDir, "go", "run", "./ocmtypegen", bindingDir); err != nil {
		return fmt.Errorf("ocmtypegen: %w", err)
	}
	deepcopy, err := deepcopyGenBinary(root)
	if err != nil {
		return err
	}
	if err := execStream(bindingDir, deepcopy, "--output-file", "zz_generated.deepcopy.go", "./..."); err != nil {
		return fmt.Errorf("deepcopy-gen: %w", err)
	}
	if err := execStream(generatorDir, "go", "run", "./jsonschemagen/cmd", bindingDir); err != nil {
		return fmt.Errorf("jsonschemagen: %w", err)
	}
	return nil
}

// deepcopyGenBinary returns the path to the newest installed deepcopy-gen binary,
// installing it via the tools Taskfile if none is present.
func deepcopyGenBinary(root string) (string, error) {
	glob := filepath.Join(root, "tmp", "bin", "deepcopy-gen-*")
	matches, _ := filepath.Glob(glob)
	if len(matches) == 0 {
		if err := execStream(root, "task", "tools:deepcopy-gen/install"); err != nil {
			return "", fmt.Errorf("installing deepcopy-gen: %w", err)
		}
		matches, _ = filepath.Glob(glob)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("deepcopy-gen binary not found under %s", glob)
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// execStream runs a command in dir, streaming its output to the user.
func execStream(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func parseKinds(raw string) ([]bindinggen.Kind, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("--kind is required (util, access or input)")
	}
	var kinds []bindinggen.Kind
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kinds = append(kinds, bindinggen.Kind(part))
	}
	return kinds, nil
}

// resolveRoot returns the repository root: the flag value if set, otherwise the
// nearest ancestor of the working directory containing both Taskfile.yml and
// reuse.Taskfile.yml.
func resolveRoot(flagRoot string) (string, error) {
	if flagRoot != "" {
		return filepath.Abs(flagRoot)
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if fileExists(filepath.Join(dir, "Taskfile.yml")) && fileExists(filepath.Join(dir, "reuse.Taskfile.yml")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not locate repository root (Taskfile.yml + reuse.Taskfile.yml); pass --root")
		}
		dir = parent
	}
}

func tidyModule(dir string) {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: go mod tidy in %s failed (run after `task generate`): %v\n%s\n", dir, err, out)
	}
}

func dryRun(b *bindinggen.Binding, relDir string) error {
	rendered, err := b.Render()
	if err != nil {
		return err
	}
	fmt.Printf("[dry-run] would scaffold %s (%d files):\n", b.ModulePath, len(rendered))
	paths := make([]string, 0, len(rendered))
	for p := range rendered {
		paths = append(paths, filepath.ToSlash(filepath.Join(relDir, p)))
	}
	sortStrings(paths)
	for _, p := range paths {
		fmt.Printf("  %s\n", p)
	}
	return nil
}

func printNextSteps(b *bindinggen.Binding, relDir string, generated bool) {
	fmt.Println("\nNext steps:")
	n := 1
	if !generated && !b.Util {
		fmt.Printf("  %d. task generate                     # fill in zz_generated.* for the new spec types\n", n)
		n++
	}
	fmt.Printf("  %d. task %s:test\n", n, relDir)
	n++
	fmt.Printf("  %d. task \"%s/integration:test/integration\"\n", n, relDir)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// sortStrings is a tiny insertion sort to avoid pulling in sort for one call.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
