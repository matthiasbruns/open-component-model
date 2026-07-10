// Package bindinggen scaffolds a new binding under bindings/go in the canonical
// layout used across this repository: a Go module with a spec/<access|input> tree,
// scheme registration, a repository or input-method implementation, an integration
// sub-module, and the three wiring points (module Taskfile, module go.mod and the
// root Taskfile includes).
//
// It only writes the hand-authored skeletons. The generated files (zz_generated.*)
// are produced afterwards by `task generate` (ocmtypegen + jsonschemagen).
package bindinggen

import (
	"fmt"
	"strings"
	"unicode"
)

// Kind is a facet of a binding to scaffold. A binding is either a plain utility
// library (KindUtil) or a spec-based binding exposing an access type, an input
// method, or both.
type Kind string

const (
	// KindUtil is a plain library binding (like runtime or blob): no spec tree.
	KindUtil Kind = "util"
	// KindAccess scaffolds a spec/access tree plus a resource repository.
	KindAccess Kind = "access"
	// KindInput scaffolds a spec/input tree plus a constructor input method.
	KindInput Kind = "input"
)

// DefaultModulePrefix is the module path prefix shared by all go bindings.
const DefaultModulePrefix = "ocm.software/open-component-model/bindings/go"

// DefaultGoVersion is the go directive written into generated go.mod files.
const DefaultGoVersion = "1.26.4"

// Binding is the fully-resolved description of a binding to scaffold. All string
// fields are pre-derived so templates stay free of logic.
type Binding struct {
	// Name is the binding directory and package name, e.g. "wget".
	Name string
	// Package is the sanitized Go package identifier derived from Name.
	Package string
	// Spec is the exported spec type / OCM consumer type name, e.g. "Wget".
	Spec string
	// LowerSpec is the lower-cased Spec, used for file names and type aliases.
	LowerSpec string
	// Version is the spec API version directory, e.g. "v1" or "v1alpha1".
	Version string
	// ModulePrefix is the module path prefix (see DefaultModulePrefix).
	ModulePrefix string
	// ModulePath is the full module path, ModulePrefix + "/" + Name.
	ModulePath string
	// GoVersion is the go directive for generated go.mod files.
	GoVersion string
	// Util reports whether this is a plain utility binding (no spec tree).
	Util bool
	// Access reports whether an access type should be scaffolded.
	Access bool
	// Input reports whether an input method should be scaffolded.
	Input bool
	// Credentials reports whether a credentials spec should be scaffolded.
	Credentials bool
}

// Options are the raw inputs for building a Binding, typically from CLI flags.
type Options struct {
	Name         string
	Spec         string
	Version      string
	Kinds        []Kind
	Credentials  bool
	ModulePrefix string
	GoVersion    string
}

// NewBinding validates opts and resolves them into a Binding, deriving all the
// string forms the templates rely on.
func NewBinding(opts Options) (*Binding, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	pkg := sanitizePackage(name)
	if pkg == "" {
		return nil, fmt.Errorf("name %q does not yield a valid go package identifier", name)
	}

	b := &Binding{
		Name:         name,
		Package:      pkg,
		Version:      firstNonEmpty(strings.TrimSpace(opts.Version), "v1"),
		ModulePrefix: firstNonEmpty(strings.TrimSpace(opts.ModulePrefix), DefaultModulePrefix),
		GoVersion:    firstNonEmpty(strings.TrimSpace(opts.GoVersion), DefaultGoVersion),
		Credentials:  opts.Credentials,
	}
	b.ModulePath = b.ModulePrefix + "/" + b.Name

	for _, k := range opts.Kinds {
		switch k {
		case KindUtil:
			b.Util = true
		case KindAccess:
			b.Access = true
		case KindInput:
			b.Input = true
		default:
			return nil, fmt.Errorf("unknown kind %q (want util, access or input)", k)
		}
	}
	if !b.Util && !b.Access && !b.Input {
		return nil, fmt.Errorf("at least one kind is required (util, access or input)")
	}
	if b.Util && (b.Access || b.Input) {
		return nil, fmt.Errorf("kind util cannot be combined with access or input")
	}

	if b.Util {
		if b.Credentials {
			return nil, fmt.Errorf("credentials are not applicable to a util binding")
		}
		return b, nil
	}

	// Spec-based bindings need a spec type name. It must contain only letters and
	// digits; the canonical OCM type is UpperCamelCase, so the first character is
	// upper-cased (e.g. "s3" -> "S3"). When omitted it defaults from the binding name.
	spec := strings.TrimSpace(opts.Spec)
	if spec == "" {
		spec = b.Package
	}
	if !isAlphanumeric(spec) {
		return nil, fmt.Errorf("spec %q must contain only letters and digits", opts.Spec)
	}
	spec = upperFirst(spec)
	if !isExportedIdent(spec) {
		return nil, fmt.Errorf("spec %q must start with a letter", opts.Spec)
	}
	b.Spec = spec
	b.LowerSpec = strings.ToLower(spec)
	return b, nil
}

// ConsumerType is the OCM type string registered in the scheme. It mirrors the
// exported spec type name, matching the convention used by existing bindings.
func (b *Binding) ConsumerType() string { return b.Spec }

// LowerConsumerType is the lower-cased alias registered alongside ConsumerType.
func (b *Binding) LowerConsumerType() string { return strings.ToLower(b.Spec) }

// sanitizePackage lower-cases name and strips any character that is not a letter
// or digit, yielding a valid (unqualified) go package identifier or "".
func sanitizePackage(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	out := sb.String()
	if out == "" || unicode.IsDigit(rune(out[0])) {
		return ""
	}
	return out
}

// isAlphanumeric reports whether s is non-empty and contains only letters and digits.
func isAlphanumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// upperFirst upper-cases the first rune of s and leaves the rest unchanged, so
// existing internal capitals and acronyms survive: "s3" -> "S3", "OCIImage" ->
// "OCIImage".
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// isExportedIdent reports whether s is a valid, exported (upper-case first rune)
// go identifier.
func isExportedIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsUpper(r) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
