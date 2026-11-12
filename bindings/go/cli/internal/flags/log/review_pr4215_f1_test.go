package log_test

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/log"
)

// TestReviewPR4215_F1_ExplicitDefaultFormatStillTripsTheTrigger shows that
// `--logformat text` selects the format the CLI already uses by default, yet
// still makes cmd.Flags().Changed(FormatFlagName) report true. PR #4215 keys the
// suppression of the interactive progress tracker on exactly that predicate, so
// this invocation loses the progress bar while the log format is unchanged.
func TestReviewPR4215_F1_ExplicitDefaultFormatStillTripsTheTrigger(t *testing.T) {
	r := require.New(t)

	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "ocm", RunE: func(*cobra.Command, []string) error { return nil }}
		log.RegisterLoggingFlags(cmd.PersistentFlags())
		return cmd
	}

	// Baseline: with no flag at all the effective format is already "text".
	base := newCmd()
	base.SetArgs([]string{})
	r.NoError(base.Execute())
	defaultFormat, err := enum.Get(base.Flags(), log.FormatFlagName)
	r.NoError(err)
	r.Equal(log.FormatText, defaultFormat, "default log format is expected to be text")
	r.False(base.Flags().Changed(log.FormatFlagName))

	// Explicitly asking for the default value yields the same format...
	explicit := newCmd()
	explicit.SetArgs([]string{"--logformat", log.FormatText})
	r.NoError(explicit.Execute())
	explicitFormat, err := enum.Get(explicit.Flags(), log.FormatFlagName)
	r.NoError(err)
	r.Equal(defaultFormat, explicitFormat, "--logformat text must not change the effective format")

	// ...but it flips the predicate PR #4215 uses to kill the progress tracker.
	// Asserting the correct behaviour: suppression should depend on the requested
	// format being incompatible with the tracker, not on the flag merely having
	// been set to its own default value.
	r.False(explicit.Flags().Changed(log.FormatFlagName),
		"--logformat text requests the default format, so it must not be treated as a request to disable the interactive progress display")
}
