#!/usr/bin/env bash
# Run real gardener/cc-utils ocm/sign.py on every input_* fixture in the PR's
# json/v1 fixtures_test.go and compare with the PR's norm_* "known-answer" bytes.
# Exit 0 = every PR fixture matches cc-utils, 1 = at least one fixture diverges.
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
mkdir "$work/fx"
CCUTILS_DIR="$work/cc-utils" "$work/venv/bin/python" -I "$here/fixtures_vs_ccutils.py" \
  "$root/bindings/go/descriptor/normalisation/json/v1/fixtures_test.go" \
  "$here/ccutils_digest.py" "$work/fx"
