package v2_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func TestReviewPR3635ExternalAWSDateCompatibility5d3e672f(t *testing.T) {
	r := require.New(t)
	if os.Getenv("OCM_REVIEW_EXTERNAL_AWSDATE") != "1" {
		t.Skip("set OCM_REVIEW_EXTERNAL_AWSDATE=1 to fetch the pinned legacy schema through gh api")
	}

	// Pin the upstream revision so the review evidence remains reproducible.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "gh", "api",
		"repos/open-component-model/ocm/contents/resources/component-descriptor-v2-schema.yaml?ref=5adad3efc20d52eb6f7e1b38922a51a0f1ea5712",
		"-H", "Accept: application/vnd.github.raw+json").Output()
	r.NoError(err)
	var schemaDocument map[string]any
	r.NoError(yaml.Unmarshal(raw, &schemaDocument))
	compiler := jsonschema.NewCompiler()
	const schemaURL = "https://gardener.cloud/schemas/component-descriptor-v2"
	r.NoError(compiler.AddResource(schemaURL, schemaDocument))
	legacySchema, err := compiler.Compile(schemaURL)
	r.NoError(err)
	awsDate, ok := versioning.BuiltinScheme(versioning.BuiltinAWSDate)
	r.True(ok)

	for _, tc := range []struct {
		version        string
		legacyAccepted bool
	}{
		{version: "1.0.0", legacyAccepted: true},
		{version: "2024-03-15", legacyAccepted: true},
		{version: "2024.03.15", legacyAccepted: false},
		{version: "2024/03/15", legacyAccepted: false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			r := require.New(t)
			descriptor := map[string]any{
				"meta": map[string]any{"schemaVersion": "v2"},
				"component": map[string]any{
					"name": "example.com/review", "version": tc.version,
					"provider": "example.com", "repositoryContexts": []any{},
					"sources": []any{}, "componentReferences": []any{}, "resources": []any{},
				},
			}
			legacyErr := legacySchema.Validate(descriptor)
			r.Equal(tc.legacyAccepted, legacyErr == nil, "legacy schema: %v", legacyErr)
			encoded, err := json.Marshal(descriptor)
			r.NoError(err)
			r.NoError(descriptorv2.ValidateRawJSON(encoded))
			t.Logf("version=%q legacySchemaAccepted=%t legacyError=%v", tc.version, legacyErr == nil, legacyErr)

			if tc.version == "2024-03-15" {
				r.True(awsDate.Valid(tc.version))
				r.True(versioning.NewLooseSemverScheme().Valid(tc.version))
				parsed, err := semver.NewVersion(tc.version)
				r.NoError(err)
				r.Equal("2024.0.0-03-15", parsed.String())
				_, err = semver.StrictNewVersion(tc.version)
				r.Error(err)
				t.Logf("AWS date and loose semver both accept; loose normalization=%q; strict semver rejects", parsed.String())
			}
		})
	}
}
