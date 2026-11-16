//go:build windows

package osutil

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

const reviewPR4302Rounds = 300

// TestReviewPR4302_SecondKillRightAfterTermination kills a running child and kills it again
// immediately, which is the window in which ProcessAlive has to report the process as gone
// although its teardown has only just started.
func TestReviewPR4302_SecondKillRightAfterTermination(t *testing.T) {
	r := require.New(t)
	var failures []error
	var normalized, aliveRightAfterKill int
	for range reviewPR4302Rounds {
		cmd := helperCommand(t, "block")
		r.NoError(cmd.Start())
		r.NoError(cmd.Process.Kill())
		err := Kill(cmd.Process)
		if ProcessAlive(cmd.Process.Pid) {
			aliveRightAfterKill++
		}
		_, waitErr := cmd.Process.Wait()
		r.NoError(waitErr)
		switch {
		case err == nil:
		case errors.Is(err, os.ErrProcessDone):
			normalized++
		default:
			failures = append(failures, err)
		}
	}
	t.Logf("rounds=%d normalized_to_ErrProcessDone=%d alive_after_second_kill=%d failures=%d",
		reviewPR4302Rounds, normalized, aliveRightAfterKill, len(failures))
	r.Empty(failures, "a second kill right after termination must report a terminated process")
}

// TestReviewPR4302_RawSecondKillIsDenied is the positive control: without osutil.Kill the same
// sequence has to hit ERROR_ACCESS_DENIED, or the test above exercises nothing.
func TestReviewPR4302_RawSecondKillIsDenied(t *testing.T) {
	r := require.New(t)
	var denied int
	for range reviewPR4302Rounds {
		cmd := helperCommand(t, "block")
		r.NoError(cmd.Start())
		r.NoError(cmd.Process.Kill())
		if err := cmd.Process.Kill(); errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			denied++
		}
		_, waitErr := cmd.Process.Wait()
		r.NoError(waitErr)
	}
	t.Logf("rounds=%d raw_access_denied=%d", reviewPR4302Rounds, denied)
	r.Positive(denied, "the raw second kill never hit access denied: the race is not exercised")
}

// TestReviewPR4302_ConcurrentKills races a raw kill, as os/exec's default Cancel does, against
// osutil.Kill, as the test cleanups do. Whoever loses must not report a failed kill.
func TestReviewPR4302_ConcurrentKills(t *testing.T) {
	r := require.New(t)
	var failures []error
	var rawDenied int
	for range reviewPR4302Rounds {
		cmd := helperCommand(t, "block")
		r.NoError(cmd.Start())
		start := make(chan struct{})
		var rawErr, err error
		var wg sync.WaitGroup
		wg.Go(func() { <-start; rawErr = cmd.Process.Kill() })
		wg.Go(func() { <-start; err = Kill(cmd.Process) })
		close(start)
		wg.Wait()
		_, waitErr := cmd.Process.Wait()
		r.NoError(waitErr)
		if errors.Is(rawErr, windows.ERROR_ACCESS_DENIED) {
			rawDenied++
		}
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			failures = append(failures, err)
		}
	}
	t.Logf("rounds=%d raw_loser_access_denied=%d osutil_kill_failures=%d", reviewPR4302Rounds, rawDenied, len(failures))
	r.Empty(failures, "osutil.Kill losing the race must report a terminated process")
}
