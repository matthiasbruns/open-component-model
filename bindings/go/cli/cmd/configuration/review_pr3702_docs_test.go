package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestReviewPR3702_D3_Precondition_ConfigFlagReplacesDefaultLookup documents the CLI behavior the
// migrated docs overlook: without --config, $HOME/.ocmconfig (credentials, resolvers) is loaded;
// with `--config ocmconfig.yaml` holding only uploader entries, it is not. This test PASSES on
// head; it is the precondition for .review/pr-3702/D3-config-flag-drops-home-config.sh.
func TestReviewPR3702_D3_Precondition_ConfigFlagReplacesDefaultLookup(t *testing.T) {
	r := require.New(t)
	home := t.TempDir()
	wd := t.TempDir()

	r.NoError(os.WriteFile(filepath.Join(home, ".ocmconfig"), []byte(`type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers: []
`), 0o600))
	// Exactly the file the migrated tutorials write.
	uploaderOnly := filepath.Join(wd, "ocmconfig.yaml")
	r.NoError(os.WriteFile(uploaderOnly, []byte(`type: generic.config.ocm.software/v1
configurations:
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
`), 0o600))

	hasCredentials := func(types []runtime.Type) bool {
		for _, typ := range types {
			if typ.Name == "credentials.config.ocm.software" {
				return true
			}
		}
		return false
	}
	typesOf := func(t *testing.T, cmd *cobra.Command) []runtime.Type {
		cfg, err := GetOCMConfigForCommand(cmd)
		require.NoError(t, err)
		var out []runtime.Type
		for _, e := range cfg.Configurations {
			out = append(out, e.GetType())
		}
		return out
	}

	// Default lookup (no --config): $HOME/.ocmconfig is loaded.
	defaults, err := GetOCMConfig(OCMConfigOptions{
		Stat:        os.Stat,
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
		Getwd:       func() (string, error) { return wd, nil },
		Executable:  func() (string, error) { return filepath.Join(wd, "ocm"), nil },
	})
	r.NoError(err)
	var defaultTypes []runtime.Type
	for _, e := range defaults.Configurations {
		defaultTypes = append(defaultTypes, e.GetType())
	}
	r.True(hasCredentials(defaultTypes), "default lookup should load $HOME/.ocmconfig")

	// --config ocmconfig.yaml (as in the migrated docs): $HOME/.ocmconfig is not loaded.
	cmd := &cobra.Command{Use: "x"}
	RegisterConfigFlag(cmd)
	r.NoError(cmd.PersistentFlags().Set(OCMConfigCommandArgument, uploaderOnly))
	r.False(hasCredentials(typesOf(t, cmd)), "--config replaces the default lookup")
}
