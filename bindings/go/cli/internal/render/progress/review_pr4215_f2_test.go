package progress

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReviewPR4215_F2_BufferedHandlerDiscardsFormatAndDestination documents the
// root cause that PR #4215 routes around instead of fixing: in terminal mode the
// tracker replaces the configured logger with a hard-coded slog.TextHandler, so
// both the requested log format (--logformat) and the requested destination
// (--logoutput) are dropped while the progress bar is active.
//
// The assertion states the correct behaviour: a JSON logger must keep emitting
// JSON through the progress buffer. It fails on PR head 924abb860d.
func TestReviewPR4215_F2_BufferedHandlerDiscardsFormatAndDestination(t *testing.T) {
	r := require.New(t)

	// The user asked for JSON logs on a specific destination.
	var requestedDestination bytes.Buffer
	configured := slog.New(slog.NewJSONHandler(&requestedDestination, &slog.HandlerOptions{Level: slog.LevelInfo}))

	buf := &SyncBuffer{}
	handler := newBufferedHandler(buf, configured.Handler())
	slog.New(handler).Info("hello", slog.String("k", "v"))

	captured := buf.DrainString()
	r.NotEmpty(captured, "progress buffer captured the record")
	t.Logf("captured through progress buffer: %q", captured)
	t.Logf("requested destination received:   %q", requestedDestination.String())

	var decoded map[string]any
	r.NoError(json.Unmarshal([]byte(captured), &decoded),
		"the progress log buffer must honour the configured --logformat (JSON here), but the record was rendered as text")
}
