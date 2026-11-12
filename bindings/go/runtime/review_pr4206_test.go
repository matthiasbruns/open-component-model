package runtime_test

import (
	"encoding/json/v2"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestReviewPR4206_F4a_EveryMarshalCallPassesJSONOptions makes the invariant the wire format
// depends on mechanical instead of review-enforced.
//
// runtime.JSONOptions pins three encoding/json/v2 defaults back to the behaviour OCM's format
// was specified with. The golangci forbidigo rule added by this PR bans the encoding/json v1
// entrypoints, but it cannot inspect arguments: a plain json.Marshal(v) against
// encoding/json/v2 lints clean, compiles, and silently reintroduces random map ordering and
// {} / [] for a nil map or slice. Those are exactly the changes that move a digest.
//
// There is no golden-bytes canary either - the stored descriptor is produced by
// sigs.k8s.io/yaml, i.e. encoding/json v1 - so such a regression would keep the suite green.
//
// This walks the module and asserts every encoding/json/v2 Marshal/MarshalWrite/MarshalEncode
// call passes runtime.JSONOptions. The one documented exception is a MarshalJSONTo that
// deliberately inherits its caller's options from the encoder it was handed.
func TestReviewPR4206_F4a_EveryMarshalCallPassesJSONOptions(t *testing.T) {
	r := require.New(t)

	// Call sites that intentionally inherit options from the enclosing encoder.
	allowed := map[string]string{
		"runtime/unstructured.go": "MarshalJSONTo inherits the caller's options from enc",
	}

	moduleRoot, err := filepath.Abs("..")
	r.NoError(err)

	var offenders []string
	fset := token.NewFileSet()

	r.NoError(filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil // not our business to police unparsable files
		}

		// Which local name refers to encoding/json/v2 in this file, if any.
		aliases := map[string]bool{}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) != "encoding/json/v2" {
				continue
			}
			name := "json"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			aliases[name] = true
		}
		if len(aliases) == 0 {
			return nil
		}

		rel, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		if _, ok := allowed[filepath.ToSlash(rel)]; ok {
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || !aliases[pkg.Name] {
				return true
			}
			switch sel.Sel.Name {
			case "Marshal", "MarshalWrite", "MarshalEncode":
			default:
				return true
			}

			for _, arg := range call.Args {
				if argMentionsJSONOptions(arg) {
					return true
				}
			}
			offenders = append(offenders, fmt.Sprintf("%s: %s.%s without runtime.JSONOptions",
				fset.Position(call.Pos()), pkg.Name, sel.Sel.Name))
			return true
		})
		return nil
	}))

	r.Empty(offenders, "encoding/json/v2 marshal calls must pass runtime.JSONOptions:\n%s",
		strings.Join(offenders, "\n"))
}

func argMentionsJSONOptions(arg ast.Expr) bool {
	found := false
	ast.Inspect(arg, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "JSONOptions" {
			found = true
		}
		return !found
	})
	return found
}

// TestReviewPR4206_F4b_JSONOptionsPinsAllThreeDefaults covers runtime.JSONOptions directly.
//
// Package runtime has no json_test.go, so the three options are only pinned indirectly by
// whichever tests happen to notice. Dropping Deterministic fails ~30 tests and dropping
// FormatNilSliceAsNull ~26, but dropping FormatNilMapAsNull fails exactly one - and that test
// is about something else (a pointer marshaler encoding a zero time as null); the nil-map shape
// is caught only incidentally by its generic JSON comparison helper. Nothing asserts the
// nil-map wire shape on purpose.
func TestReviewPR4206_F4b_JSONOptionsPinsAllThreeDefaults(t *testing.T) {
	type payload struct {
		Members map[string]string `json:"members"`
		Items   []string          `json:"items"`
	}

	t.Run("nil map and nil slice encode as null", func(t *testing.T) {
		r := require.New(t)

		data, err := json.Marshal(&payload{}, runtime.JSONOptions)
		r.NoError(err)
		r.JSONEq(`{"members":null,"items":null}`, string(data))
		r.Equal(`{"members":null,"items":null}`, string(data),
			"a nil map must encode as null, not {}; v1 wrote null and stored descriptors contain null")
	})

	t.Run("map members are emitted in a deterministic order", func(t *testing.T) {
		r := require.New(t)

		in := &payload{Members: map[string]string{
			"delta": "4", "alpha": "1", "charlie": "3", "bravo": "2",
			"echo": "5", "foxtrot": "6", "golf": "7", "hotel": "8",
		}}

		first, err := json.Marshal(in, runtime.JSONOptions)
		r.NoError(err)

		// Byte-wise sorted key order, matching encoding/json v1.
		r.Contains(string(first), `"alpha":"1","bravo":"2","charlie":"3","delta":"4"`)

		for range 50 {
			again, err := json.Marshal(in, runtime.JSONOptions)
			r.NoError(err)
			r.Equal(string(first), string(again),
				"marshaling the same value twice produced different bytes, so digests are unstable")
		}
	})
}
