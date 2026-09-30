#!/usr/bin/env bash
# PR 3702 / D4: transfer-configuration.md claims the Selection Examples are executed by
# TestUploaderExamples, but the E9 "does not compile" row documents `match: resource.access.isType(`
# while the test runs `match: resource.name ==`.
#
# Expected: the documented E9 invalid-match expression is the one TestUploaderExamples executes.
# Actual on dc65b8520: different expression.
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
set -euo pipefail
REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
DOC="$REPO_ROOT/website/content/docs/reference/transfer-configuration.md"
TEST="$REPO_ROOT/bindings/go/transfer/internal/uploader_examples_test.go"
[[ -f "$DOC" && -f "$TEST" ]] || { echo "SETUP FAILED: files missing (not applicable on this revision)"; exit 2; }

row="$(grep -E '^\| OCI, `match: .*\| `invalid match` \|' "$DOC" || true)"
[[ -n "$row" ]] || { echo "SETUP FAILED: E9 invalid-match row not found"; exit 2; }
doc_expr="$(sed -E 's/^\| OCI, `match: ([^`]*)`.*/\1/' <<<"$row")"
echo "doc E9 invalid match: '$doc_expr'"

test_expr="$(awk '/name: +"E9 match that does not compile"/{f=1} f && /^    match: /{sub(/^    match: /,""); print; exit}' "$TEST")"
echo "test E9 invalid match: '$test_expr'"
[[ -n "$test_expr" ]] || { echo "SETUP FAILED: test case not found"; exit 2; }

if [[ "$doc_expr" != "$test_expr" ]]; then
  echo "REPRODUCED: documented example differs from the executed one"; exit 1
fi
echo "OK: doc and test agree"
