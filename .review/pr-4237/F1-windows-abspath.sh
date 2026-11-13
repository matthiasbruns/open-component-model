#!/usr/bin/env bash
# PR 4237 / F1: the two new private-infrastructure cases in TestHandler_Verify fail on Windows.
#
# Expected: TestHandler_Verify passes on GOOS=windows, like every other case that uses absPath().
# Actual on 4cd3905850: both new cases fail with
#   resolve trusted root: trusted root file path must be absolute, got "/path/to/private_trusted_root.json"
# because handler_test.go:724 and :742 pass a bare "/path/to/..." path, which filepath.IsAbs rejects on Windows.
#
# Windows unit tests only run on pushes to main (needs.changes.outputs.run_windows), so PR CI stays green
# and main's windows leg breaks after merge. This script cross-compiles the test binary for windows/amd64
# and runs it under wine, so it needs wine on PATH.
#
# Contract: exit 0 = behaves correctly, exit 1 = finding reproduced, anything else = setup problem.
#   REPO_ROOT=/path/to/checkout bash .review/pr-4237/F1-windows-abspath.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

command -v wine >/dev/null || setup_fail "wine not on PATH"
(cd "$REPO_ROOT/bindings/go" && GOOS=windows GOARCH=amd64 go test -c -o "$WORK/handler.test.exe" ./sigstore/signing/handler/) \
  || setup_fail "cross-compile handler tests for windows"

set +e
out="$(cd "$WORK" && WINEDEBUG=-all WINEPREFIX="$WORK/wineprefix" wine ./handler.test.exe -test.run '^TestHandler_Verify$' -test.v 2>&1)"
rc=$?
set -e
echo "$out" | grep -E '^\s*--- (PASS|FAIL)|must be absolute|^(PASS|FAIL)$' || true

if ! echo "$out" | grep -qE '^(PASS|FAIL)$'; then
  setup_fail "test binary did not run to completion under wine (rc=$rc)"
fi
if [[ $rc -ne 0 ]]; then
  echo "REPRODUCED: TestHandler_Verify fails on windows"; exit 1
fi
echo "OK: TestHandler_Verify passes on windows"
