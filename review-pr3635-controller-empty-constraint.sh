#!/bin/sh
# Controller selection is NOT reachable through the public CLI.
# An isolated Ginkgo overlay exercises the real internal function, without
# modifying the checkout or depending on any existing review/suite test files.
# Requires Go and Python 3. Exit 0: fixed; 1: known bug; 2: setup/control failure.
set -eu
exec python3 - "$0" <<'PY'
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve().parent
module = root / "bindings/go"
package = module / "kubernetes/controller/internal/ocm"


def interrupted(signum, frame):
    raise RuntimeError("interrupted or overall 300-second deadline exceeded")


signal.signal(signal.SIGTERM, interrupted)
signal.signal(signal.SIGALRM, interrupted)
signal.alarm(300)

TEST = r'''
package ocm_test
import (
    "fmt"
    "os"
    "testing"
    "github.com/Masterminds/semver/v3"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
    "ocm.software/open-component-model/bindings/go/runtime/versioning"
)
func TestReviewPR3635IsolatedEmptyConstraint(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Isolated PR3635 controller constraint regression")
}
var _ = Describe("Isolated controller empty constraint", func() {
    It("retains legacy rejection rather than opting into prereleases", func(ctx SpecContext) {
        _, err := semver.NewConstraint("")
        Expect(err).To(HaveOccurred(), "legacy parser control")
        versions := []string{"1.0.0", "2.0.0-rc.1", "zzz"}
        stable, err := ocm.GetLatestValidVersion(ctx, versioning.Default(), versions, "*")
        Expect(err).NotTo(HaveOccurred())
        Expect(stable).To(Equal("1.0.0"), "explicit wildcard must exclude prereleases")
        filter, err := ocm.RegexpFilter(`^v1\.`)
        Expect(err).NotTo(HaveOccurred())
        spelled, err := ocm.GetLatestValidVersion(ctx, versioning.Default(),
            []string{"v1.2.0+build.5", "2.0.0", "zzz"}, ">=1.0.0", filter)
        Expect(err).NotTo(HaveOccurred())
        Expect(spelled).To(Equal("v1.2.0+build.5"))
        got, err := ocm.GetLatestValidVersion(ctx, versioning.Default(), versions, "")
        fmt.Fprintf(GinkgoWriter, "wildcard=%q empty=%q error=%v; legacy empty constraint rejects\n", stable, got, err)
        if err == nil && got == "2.0.0-rc.1" {
            Expect(os.WriteFile(os.Getenv("REVIEW_PR3635_RESULT"), []byte("known-bug"), 0600)).To(Succeed())
        }
        Expect(err).To(HaveOccurred(), "empty constraints must retain legacy rejection")
        Expect(got).To(BeEmpty())
        Expect(os.WriteFile(os.Getenv("REVIEW_PR3635_RESULT"), []byte("fixed"), 0600)).To(Succeed())
    })
})
'''

try:
    with tempfile.TemporaryDirectory(prefix="review-pr3635-controller-") as directory:
        work = Path(directory)
        source = work / "isolated_test.go"
        source.write_text(TEST)
        # Remove all checkout tests from this invocation only (including review
        # files); add a uniquely named virtual file in a valid internal location.
        replacements = {str(path): "" for path in package.glob("*_test.go")}
        replacements[str(package / ("review_pr3635_" + work.name.replace("-", "_") + "_test.go"))] = str(source)
        overlay = work / "overlay.json"
        overlay.write_text(json.dumps({"Replace": replacements}))
        marker = work / "result"
        result = subprocess.run([
            "go", "test", "-overlay", str(overlay),
            "./kubernetes/controller/internal/ocm", "-count=1", "-timeout=120s",
            "-run", "^TestReviewPR3635IsolatedEmptyConstraint$", "-v", "-ginkgo.no-color",
        ], cwd=module, env=dict(os.environ, REVIEW_PR3635_RESULT=str(marker)),
           capture_output=True, text=True, timeout=240)
        print(result.stdout + result.stderr, end="", flush=True)
        outcome = marker.read_text() if marker.exists() else ""
        if result.returncode == 0 and outcome == "fixed":
            print("PASS: empty controller constraint rejected; all controls passed.")
            sys.exit(0)
        if result.returncode == 1 and outcome == "known-bug":
            print("FAIL: wildcard selects stable, but empty constraint selects prerelease instead of rejecting.")
            sys.exit(1)
        raise RuntimeError(f"overlay/control failure (exit {result.returncode}, marker {outcome!r})")
except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.SubprocessError) as error:
    print(f"ERROR (not a confirmed regression): {error}", file=sys.stderr)
    sys.exit(2)
finally:
    signal.alarm(0)
PY
