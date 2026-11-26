#!/usr/bin/env bash
# PR 4396 / C1 (claim check, not a finding): with only a path-scoped consumer entry, `ocm download resource`
# authenticates against a registry that protects project1/.
#
# Expected: every download succeeds (the PR's claim). Without the PR, the resource identity has no path,
# the entry never matches and the registry answers 401.
#
# Contract: exit 0 = behaves correctly, exit 1 = reproduced failure, anything else = setup problem.
#   REPO_ROOT=/path/to/checkout bash .review/pr-4396/C1-cli-path-scoped.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

{ config_header; entry user1 password1 "project1/**"; http_footer; } > "$WORK/path-only.yaml"
result="$(download_with "$WORK/path-only.yaml" 5)"
echo "path-scoped entry only: $result"
if [[ "$result" != *"failed=0"* ]]; then
  echo "--- last failure:"; grep -v "^time=.*level=DEBUG" "$WORK/last-failure.log" | tail -5
  echo "REPRODUCED: download with a path-scoped entry failed"; exit 1
fi
echo "OK: path-scoped credentials are used for the image"
