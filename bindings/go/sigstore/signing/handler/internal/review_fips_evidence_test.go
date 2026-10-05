package internal

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These characterize the current guard; they do not assert CMVP validation.
func TestReviewFIPSEvidenceRealBinary(t *testing.T) {
	for _, version := range []string{"certified", "v1.26.0"} {
		t.Run(version, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			source := filepath.Join(dir, "main.go")
			r.NoError(os.WriteFile(source, []byte(`package main
import ("crypto/fips140"; "fmt")
func main() { fmt.Printf("enabled=%v module=%s\n", fips140.Enabled(), fips140.Version()) }
`), 0o600))
			binary := filepath.Join(dir, "probe")
			cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binary, source)
			cmd.Env = append(os.Environ(), "GOFIPS140="+version, "GOTOOLCHAIN=local", "CGO_ENABLED=0")
			out, err := cmd.CombinedOutput()
			r.NoError(err, "build: %s", out)
			info, err := buildinfo.ReadFile(binary)
			r.NoError(err)
			for _, setting := range info.Settings {
				if setting.Key == "GOFIPS140" || setting.Key == "DefaultGODEBUG" {
					t.Logf("requested=%s %s=%s", version, setting.Key, setting.Value)
				}
			}
			b := NewCosignBinary()
			r.NoError(b.requireFIPSBuild(binary))
			t.Log("current cosign metadata guard ACCEPTED this real Go binary")
			for _, mode := range []string{"on", "only", "off"} {
				cmd := exec.CommandContext(t.Context(), binary)
				cmd.Env = append(os.Environ(), "GODEBUG=fips140="+mode)
				out, err := cmd.CombinedOutput()
				r.NoError(err, "probe mode=%s: %s", mode, out)
				want := "enabled=true"
				if mode == "off" {
					want = "enabled=false"
				}
				r.Contains(string(out), want)
				t.Logf("mode=%s %s", mode, strings.TrimSpace(string(out)))
			}
		})
	}
}
