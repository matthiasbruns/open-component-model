#!/usr/bin/env bash
# PR 3887 / R2: jsonNormalisation/v1 emits resource types verbatim, cc-utils
# signs their canonical ArtefactType value.
#
# Expected: blob, helmChart, ociArtifact, filesystem, ociImage/v1 and git/v1
#   normalise to blob/v1, helmChart/v1, ociArtifact/v1, directoryTree, ociImage
#   and git, as cc-utils ocm/sign.py does (alias table in ocm/__init__.py).
# Actual on 5164e368: the stored type is emitted unchanged, so a cc-utils
#   signature over such a descriptor does not verify. The PR's own fixtures
#   02, 03 and 08 carry the unaliased types.
#
# Steps:
#   1. offline Go test TestReviewPR3887_R2_ResourceTypeAliases;
#   2. with CCUTILS=1 (needs network): run gardener/cc-utils ocm/sign.py unchanged
#      on the PR's own fixtures and on the alias case, and diff against the PR.
#
# Contract: exit 0 = behaves correctly, exit 1 = finding reproduced,
# anything else = setup problem (inconclusive). Run from any checkout:
#   REPO_ROOT=/path/to/checkout [CCUTILS=1] bash .review/pr-3887/R2-type-aliases.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
PKG="$REPO_ROOT/bindings/go/descriptor"
ORACLE="$REPO_ROOT/.review/pr-3887/ccutils-oracle"
[[ -f "$PKG/normalisation/json/v1/review_pr3887_typealias_test.go" ]] || { echo "SETUP FAILED: test file missing" >&2; exit 2; }

reproduced=0

set +e
out="$(go -C "$PKG" test ./normalisation/json/v1/ -run 'TestReviewPR3887_R2_' -count=1 -v 2>&1)"
rc=$?
set -e
echo "$out" | grep -E '^\s*--- (PASS|FAIL)|^(ok|FAIL)\s'
if echo "$out" | grep -qE 'build failed|cannot|undefined'; then
  echo "SETUP FAILED: test did not build" >&2; exit 2
fi
[[ $rc -ne 0 ]] && reproduced=1

if [[ "${CCUTILS:-0}" == "1" ]]; then
  WORK="$(mktemp -d)"
  trap 'rm -rf "$WORK"' EXIT
  CCUTILS_REV="${CCUTILS_REV:-01d0cffaea759127c915eed481b5fc6339fb1e06}"
  git clone -q https://github.com/gardener/cc-utils.git "$WORK/cc-utils" || { echo "SETUP FAILED: clone cc-utils" >&2; exit 2; }
  git -C "$WORK/cc-utils" checkout -q "$CCUTILS_REV"
  python3 -m venv "$WORK/venv" && "$WORK/venv/bin/pip" -q install dacite pyyaml || { echo "SETUP FAILED: venv" >&2; exit 2; }

  echo "== cc-utils on the alias case (c16)"
  CCUTILS_DIR="$WORK/cc-utils" "$WORK/venv/bin/python" -I "$ORACLE/ccutils_digest.py" \
    "$ORACLE/cases/c16-resource-type-aliases/input.yaml" | grep -oE '\{"type":"[^"]*"\}' | tr '\n' ' '; echo

  echo "== cc-utils vs the PR's own known-answer fixtures"
  mkdir "$WORK/fx"
  set +e
  CCUTILS_DIR="$WORK/cc-utils" "$WORK/venv/bin/python" -I "$ORACLE/fixtures_vs_ccutils.py" \
    "$PKG/normalisation/json/v1/fixtures_test.go" "$ORACLE/ccutils_digest.py" "$WORK/fx" | grep -E -A2 ': DIFF|ERROR'
  set -e
  if CCUTILS_DIR="$WORK/cc-utils" "$WORK/venv/bin/python" -I "$ORACLE/fixtures_vs_ccutils.py" \
    "$PKG/normalisation/json/v1/fixtures_test.go" "$ORACLE/ccutils_digest.py" "$WORK/fx" >/dev/null; then :; else reproduced=1; fi
fi

if [[ $reproduced -ne 0 ]]; then
  echo "REPRODUCED: resource types are not normalised to the cc-utils ArtefactType value"; exit 1
fi
echo "OK: resource types match cc-utils"
