#!/usr/bin/env bash
# PR 4396 / F1: with a registry-wide entry plus a path-scoped override, `ocm download resource`
# picks either entry at random, so downloads of a project1 image fail intermittently.
#
# Expected: the same command with the same config behaves the same on every run (all succeed with the
# override's credentials).
# Actual on 08e8ce2ff6: some runs send the registry-wide credentials and get 401.
#
# Contract: exit 0 = behaves correctly, exit 1 = finding reproduced, anything else = setup problem.
#   REPO_ROOT=/path/to/checkout bash .review/pr-4396/F1-cli-overlap.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

RUNS="${RUNS:-30}"
{ config_header; entry default default; entry user1 password1 "project1/**"; http_footer; } > "$WORK/overlap.yaml"
result="$(download_with "$WORK/overlap.yaml" "$RUNS")"
echo "registry-wide entry + project1/** override, $RUNS runs: $result"
echo "usernames the registry saw for project1/app manifests:"
grep 'project1/app/manifests' "$WORK/registry.log" | grep -o 'user="[^"]*"' | sort | uniq -c
if [[ "$result" != *"failed=0"* ]]; then
  echo "--- last failure:"; grep -v "^time=.*level=DEBUG" "$WORK/last-failure.log" | tail -3
  echo "REPRODUCED: not every run succeeded with the same config"; exit 1
fi
echo "OK: every run used the override"
