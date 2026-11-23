#!/usr/bin/env bash
# PR 4341 / F2: the legacy-credentials migration guide still says matching entries are not ranked.
#
# Expected: once matchAnyNode ranks overlapping matches, the guide describes the ranking
#           (or at least no longer denies it).
# Actual on d84721ee8f: synced_dag.go ranks matches, the guide still reads
#           "the new OCM does not rank matching entries".
#
# Contract (verify.sh): exit 0 = consistent, exit 1 = finding reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout bash .review/pr-4341/F2-stale-migration-guide.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
GUIDE="$REPO_ROOT/website/content/docs/changelog/migrate-legacy-credentials.md"
CODE="$REPO_ROOT/bindings/go/credentials/synced_dag.go"
[[ -f "$GUIDE" && -f "$CODE" ]] || { echo "SETUP FAILED: guide or code not found" >&2; exit 2; }

if ! grep -q 'func moreSpecific' "$CODE"; then
  echo "OK: matchAnyNode does not rank matches in this checkout, the guide is accurate"; exit 0
fi
if grep -n 'does not rank matching entries' "$GUIDE"; then
  echo "REPRODUCED: the code ranks overlapping matches, the guide says it does not"; exit 1
fi
echo "OK: guide no longer denies ranking"
