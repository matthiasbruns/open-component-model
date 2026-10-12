#!/usr/bin/env bash
# PR 3595 / F1: the new ADR reuses a number that is already taken on main.
#
# Expected: every ADR number in this checkout plus main's docs/adr is used by exactly one file
#           (the rule enforced by .github/scripts/check-adr-numbers.js on main).
# Actual on 0ce47b76d: 0030 is used by 0030_vm_image_packaging.md (PR) and 0030_fips_gpg_cosign.md (main, #3689).
#
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout MAIN_REF=upstream/main bash .review/pr-3595/F1-adr-number-collision.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
MAIN_REF="${MAIN_REF:-upstream/main}"
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }
git -C "$REPO_ROOT" rev-parse --verify --quiet "$MAIN_REF" >/dev/null || setup_fail "ref $MAIN_REF not found (git fetch upstream main)"

dups="$({ ls "$REPO_ROOT/docs/adr"; git -C "$REPO_ROOT" ls-tree --name-only "$MAIN_REF" docs/adr/ | xargs -n1 basename; } \
  | grep -E '^[0-9]+_.+\.md$' | sort -u \
  | awk -F_ '{n=$1+0; files[n]=files[n] " " $0; c[n]++} END {for (n in c) if (c[n]>1) printf "%04d:%s\n", n, files[n]}')"

if [[ -n "$dups" ]]; then
  echo "REPRODUCED: ADR numbers used by more than one file (this checkout + $MAIN_REF):"
  echo "$dups"
  exit 1
fi
echo "OK: ADR numbers are unique"
