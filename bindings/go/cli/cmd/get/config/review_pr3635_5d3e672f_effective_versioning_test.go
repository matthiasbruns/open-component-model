package config_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
)

func TestReviewPR3635_5d3e672f_EffectiveVersioningConfig(t *testing.T) {
	r := require.New(t)
	work := t.TempDir()
	config := filepath.Join(work, "config.json")
	const versioning = `{"type":"versioning.config.ocm.software/v1alpha1","schemes":[{"name":"review-build","pattern":"^build-(?P<n>[0-9]+)$","comparisonGroups":["n"]}]}`
	r.NoError(filesystem.CopyBlobToOSPath(inmemory.New(strings.NewReader(`{"type":"generic.config.ocm.software/v1","configurations":[`+versioning+`]}`)), config))
	constructor := filepath.Join(work, "constructor.json")
	r.NoError(filesystem.CopyBlobToOSPath(inmemory.New(strings.NewReader(`{"components":[{"name":"example.org/review-pr3635","version":"build-10","provider":{"name":"example.org"}}]}`)), constructor))
	repository := "ctf::" + filepath.Join(work, "repository")

	_, err := test.OCM(t, test.WithArgs("--config", config, "add", "component-version", "--repository", repository, "--constructor", constructor))
	r.NoError(err, "the configured non-semver scheme must actually be accepted")
	var listing bytes.Buffer
	_, err = test.OCM(t, test.WithArgs("--config", config, "get", "component-versions", repository+"//example.org/review-pr3635", "--constraint", ">=build-10", "--output", "json"), test.WithOutput(&listing))
	r.NoError(err)
	var descriptors []struct {
		Component struct {
			Version string `json:"version"`
		} `json:"component"`
	}
	r.NoError(json.Unmarshal(listing.Bytes(), &descriptors))
	r.Len(descriptors, 1)
	r.Equal("build-10", descriptors[0].Component.Version)

	var output bytes.Buffer
	_, err = test.OCM(t, test.WithArgs("--config", config, "get", "config", "--output", "json"), test.WithOutput(&output))
	r.NoError(err)
	var effective struct {
		Configurations []json.RawMessage `json:"configurations"`
	}
	r.NoError(json.Unmarshal(output.Bytes(), &effective))
	for _, entry := range effective.Configurations {
		var header struct {
			Type string `json:"type"`
		}
		r.NoError(json.Unmarshal(entry, &header))
		if header.Type == "versioning.config.ocm.software/v1alpha1" {
			r.JSONEq(versioning, string(entry))
			return
		}
	}
	t.Fatalf("get config omitted the active versioning configuration: %s", output.String())
}
