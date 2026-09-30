#!/usr/bin/env bash
# PR 3702 / S1 fix check: the suggested one-line change bounds uploader CEL evaluation.
#
# Applies the review suggestion to a temporary copy of bindings/go (the checkout is not
# modified), then runs the S1 repro and the existing transfer/internal tests.
#
# Exit 0 = with the fix, TestReviewPR3702_S1_* passes and transfer/internal stays green.
# Exit 1 = the fix does not hold. Anything else = setup problem.
#   REPO_ROOT=/path/to/checkout bash .review/pr-3702/S1-fix-check.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

cp -R "$REPO_ROOT/bindings/go" "$WORK/go" || setup_fail "copy bindings/go"
FILE="$WORK/go/transfer/internal/uploader_match.go"
[[ -f "$WORK/go/transfer/internal/review_pr3702_style_test.go" ]] || setup_fail "S1 repro test missing"

grep -qx $'\treturn celEnv.Program(ast)' "$FILE" || setup_fail "unexpected uploader_match.go (already fixed or changed)"
sed -i.bak $'s/^\treturn celEnv.Program(ast)$/\treturn celEnv.Program(ast, cel.CostLimit(1_000_000), cel.InterruptCheckFrequency(100))/' "$FILE"
rm -f "$FILE.bak"
echo "applied:"; grep -n 'celEnv.Program' "$FILE"

cd "$WORK/go"
if ! go test -count=1 -run '^TestReviewPR3702_S1_UploaderCELHasNoCostLimit$' ./transfer/internal/; then
  echo "FIX DOES NOT HOLD: S1 repro still fails"; exit 1
fi
# The other review repros in this package fail by design; run only the PR's own tests.
if ! go test -count=1 -skip '^TestReviewPR3702_' ./transfer/internal/; then
  echo "FIX DOES NOT HOLD: existing transfer/internal tests fail"; exit 1
fi
echo "OK: fix bounds CEL evaluation and keeps transfer/internal green"
