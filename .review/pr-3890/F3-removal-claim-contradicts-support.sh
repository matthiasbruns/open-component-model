#!/usr/bin/env bash
# PR 3890 / F3: website callouts promise removal of GitHub/v1 while the same PR says it stays for published descriptors.
#
# Expected: every deprecation callout says GitHub/v1 stays supported for published
#           component versions (as github/doc.go and resource-repositories.md do),
#           and none announces a removal.
# Actual on 599c0a156: how-to/add-resources-from-github.md and
#           reference/input-and-access-types.md say "will be removed in a future release".
#
# Contract (verify.sh): exit 0 = behaves correctly, exit 1 = finding reproduced,
# anything else = setup problem (inconclusive). Run from any checkout:
#   REPO_ROOT=/path/to/checkout bash .review/pr-3890/F3-removal-claim-contradicts-support.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
DOCS="$REPO_ROOT/website/content/docs"
[[ -d "$DOCS" ]] || { echo "SETUP FAILED: no website docs" >&2; exit 2; }

set +e
hits="$(grep -rnE 'GitHub/v1.*(will be removed|removed in a future)' "$DOCS")"
set -e

if [[ -n "$hits" ]]; then
  echo "$hits"
  echo "REPRODUCED: docs announce removal of GitHub/v1, which published descriptors still need"
  exit 1
fi
echo "OK: no removal announcement for GitHub/v1"
