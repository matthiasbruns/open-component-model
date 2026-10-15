#!/usr/bin/env bash
# Regenerate expected.json from real gardener/cc-utils and run the Go comparison.
# Exit 0 = PR matches cc-utils for every case, 1 = divergence reproduced.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/../../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
CCUTILS_REV="${CCUTILS_REV:-01d0cffaea759127c915eed481b5fc6339fb1e06}"
git clone -q https://github.com/gardener/cc-utils.git "$work/cc-utils"
git -C "$work/cc-utils" checkout -q "$CCUTILS_REV"
python3 -m venv "$work/venv"
"$work/venv/bin/pip" -q install dacite pyyaml
for d in "$here"/cases/*/; do
  CCUTILS_DIR="$work/cc-utils" "$work/venv/bin/python" -I "$here/ccutils_digest.py" "$d/input.yaml" > "$d/expected.json"
done
cd "$root/bindings/go/descriptor"
go test ./normalisation/json/v1/ -run TestReviewPR3887_CCUtilsOracle -v 2>&1 | grep -E '^\s*--- (PASS|FAIL)' || true
go test ./normalisation/json/v1/ -run TestReviewPR3887_CCUtilsOracle >/dev/null 2>&1 || exit 1
