package internal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// reviewPR3702Outcomes builds the transfer graph for configYAML against the CLI-parsed target
// ref and returns the per-resource outcome as classified by uploaderOutcomes.
func reviewPR3702Outcomes(t *testing.T, configYAML, targetRef string, resources []descriptor.Resource) map[string]string {
	t.Helper()
	r := require.New(t)

	var generic genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(configYAML), &generic))
	cfg, err := transferv1alpha1.LookupConfig(&generic)
	r.NoError(err)
	if cfg == nil {
		cfg = &transferv1alpha1.Config{}
	}
	uploaders, err := transferv1alpha1.LookupUploaderConfigs(&generic)
	r.NoError(err)

	// The target is parsed exactly like `ocm transfer cv <src> <target>` parses it.
	target, err := compref.ParseRepository(targetRef)
	r.NoError(err)

	desc := testDescriptor("ocm.software/demo", "1.0.0", resources, nil)
	resolver := testResolverFor("ocm.software/demo", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
	roots := testTransferRoots("ocm.software/demo", "1.0.0", target, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, *cfg, uploaders)
	r.NoError(err)
	return uploaderOutcomes(t, tgd, resources)
}

// D1: transfer-configuration.md ("Build a reference from resource metadata") and
// migrate-from-upload-as.md ("Build a reference from resource metadata", Troubleshooting)
// document `target.baseUrl + "/" + resource.name + ":" + resource.version`. For a CLI target
// with a path (ghcr.io/target-org/ocm) baseUrl is only the registry host, so the example
// pushes to the registry root instead of below the transfer target.
func TestReviewPR3702_D1_MetadataExampleDropsTargetSubPath(t *testing.T) {
	const targetRef = "ghcr.io/target-org/ocm"

	for _, tc := range []struct {
		name       string
		configYAML string
		resources  []descriptor.Resource
		resource   string
	}{
		{
			name: "reference doc example",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    imageReference: '${target.baseUrl + "/" + resource.name + ":" + resource.version}'
`,
			resources: []descriptor.Resource{examplesResource(t, "app")},
			resource:  "app",
		},
		{
			name: "migration guide example",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: target.type == "OCIRepository" && resource.access.isType("LocalBlob") && isOCIManifest(resource.access.mediaType)
    imageReference: '${target.baseUrl + "/" + resource.name + ":" + resource.version}'
`,
			resources: []descriptor.Resource{examplesResource(t, "bundle")},
			resource:  "bundle",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got := reviewPR3702Outcomes(t, tc.configYAML, targetRef, tc.resources)
			r.True(strings.HasPrefix(got[tc.resource], "oci "+targetRef+"/"),
				"documented example should place the artifact below the transfer target %q, got %q", targetRef, got[tc.resource])
		})
	}
}

// D2: transfer-configuration.md "Selection Examples" states that target ghcr.io/target-org/ocm
// is `baseUrl: ghcr.io/target-org/ocm`, `subPath: ""`. The CLI parses that ref into
// baseUrl ghcr.io and subPath target-org/ocm (as the migration guide correctly states).
func TestReviewPR3702_D2_SelectionExamplesTargetSplit(t *testing.T) {
	r := require.New(t)
	target, err := compref.ParseRepository("ghcr.io/target-org/ocm")
	r.NoError(err)
	literal, err := targetLiteral(target)
	r.NoError(err)
	// Documented: baseUrl ghcr.io/target-org/ocm, subPath "".
	r.Contains(literal, `"baseUrl": "ghcr.io/target-org/ocm"`, "target literal: %s", literal)
}

// Sanity (expected to pass): the documented isType table in transfer-configuration.md.
func TestReviewPR3702_IsTypeTableMatchesDocs(t *testing.T) {
	raw := func(typ string) descriptor.Resource {
		return descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "r", Version: "1.0.0"}},
			Type:        "blob",
			Relation:    descriptor.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.Type{}, Data: []byte(`{"type":"` + typ + `"}`)},
		}
	}
	args := []string{`"OCIImage"`, `"ociArtifact/v1"`, `"Helm"`, `"LocalBlob"`}
	rows := map[string][4]bool{
		"ociArtifact/v1": {true, true, false, false},
		"ociImage/v1":    {true, true, false, false},
		"ociRegistry/v1": {true, true, false, false},
		"localBlob/v1":   {false, false, false, true},
		"helm/v1":        {false, false, true, false},
		"Wget/v1":        {false, false, false, false},
	}
	for typ, want := range rows {
		for i, arg := range args {
			t.Run(typ+"_"+arg, func(t *testing.T) {
				r := require.New(t)
				res := raw(typ)
				rt, err := runtime.TypeFromString(typ)
				r.NoError(err)
				res.Access.(*runtime.Raw).Type = rt
				desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{res}, nil)
				v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
				r.NoError(err)
				tgd := &transformv1alpha1.TransformationGraphDefinition{Environment: &runtime.Unstructured{Data: map[string]any{}}}
				r.NoError(addDescriptorToEnvironment(v2desc, "base", tgd))
				env := &uploaderEnv{baseID: "base", node: tgd.Environment.Data["base"]}
				aliases, err := uploaderAliases(env, 0, testOCIRepo("ghcr.io/target"))
				r.NoError(err)
				got, err := matches("resource.access.isType("+arg+")", aliases, env)
				r.NoError(err)
				r.Equal(want[i], got)
			})
		}
	}
}

// Sanity (expected to pass): E1 outcomes with the CLI-parsed target equal the documented ones.
func TestReviewPR3702_E1WithCLIParsedTarget(t *testing.T) {
	r := require.New(t)
	got := reviewPR3702Outcomes(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
`, "ghcr.io/target-org/ocm", examplesResources())
	r.Equal(map[string]string{
		"app":    "oci ghcr.io/target-org/ocm/acme/app:1.0.0",
		"nginx":  "oci ghcr.io/target-org/ocm/library/nginx:1.25",
		"chart":  "oci ghcr.io/target-org/ocm/stable/app:1.0.0",
		"bundle": "oci ghcr.io/target-org/ocm/acme/bundle:1.0.0",
		"notes":  "local blob",
		"docs":   "by reference",
	}, got)
}

// Sanity for D4 (expected to pass): the documented E9 invalid-match expression does fail with
// "invalid match"; the finding is only that TestUploaderExamples executes a different expression.
func TestReviewPR3702_D4_DocumentedInvalidMatchFails(t *testing.T) {
	r := require.New(t)
	var generic genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(`
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType(
`), &generic))
	uploaders, err := transferv1alpha1.LookupUploaderConfigs(&generic)
	r.NoError(err)
	desc := testDescriptor("ocm.software/demo", "1.0.0", []descriptor.Resource{examplesResource(t, "app")}, nil)
	resolver := testResolverFor("ocm.software/demo", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
	roots := testTransferRoots("ocm.software/demo", "1.0.0", testOCIRepo("ghcr.io/target-org/ocm"), resolver)
	_, err = BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, uploaders)
	r.ErrorContains(err, "invalid match")
}
