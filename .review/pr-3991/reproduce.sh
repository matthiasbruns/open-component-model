#!/bin/sh
# Exit 0: expected cleanup; exit 1: reproduced regression; other: inconclusive.
set -eu
cd "$(dirname "$0")/../../bindings/go"
output=$(mktemp)
trap 'rm -f "$output"' EXIT
if go test ./oci/transformer -run '^TestReviewPR3991_SourceClosedOnTargetFailure$' -count=1 -v >"$output" 2>&1; then
  cat "$output"
  exit 0
fi
cat "$output"
if grep -q -- '--- PASS: TestReviewPR3991_SourceClosedOnTargetFailure/previous_get_add' "$output" && grep -q -- 'source reader must be closed when target fails before consuming it' "$output"; then
  exit 1
fi
exit 2
