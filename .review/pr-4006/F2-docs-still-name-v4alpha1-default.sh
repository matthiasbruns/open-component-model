#!/usr/bin/env bash
# PR 4006 / F2: user docs still present jsonNormalisation/v4alpha1 as the current/default algorithm
#
# Expected: docs that describe what `ocm sign` produces (or the "current" algorithm) name the
#           new default, i.e. the --normalisation default in the generated CLI reference.
# Actual on 51106f985: website how-tos/tutorials/concepts and docs/security still say v4alpha1.
#
# Contract: exit 0 = consistent, exit 1 = reproduced, other = inconclusive.
set -euo pipefail
REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT"

ref=bindings/go/cli/docs/reference/ocm_sign_component-version.md
[[ -f $ref ]] || { echo "SETUP FAILED: $ref missing" >&2; exit 2; }
default="$(grep -- '--normalisation string' "$ref" | sed -nE 's/.*\(default "([^"]*)"\)[[:space:]]*$/\1/p' | head -1 || true)"
[[ -n $default ]] || { echo "SETUP FAILED: no --normalisation default in $ref" >&2; exit 2; }
echo "CLI default (generated reference): $default"
[[ $default == jsonNormalisation/v4alpha1 ]] && { echo "OK: default is still v4alpha1, docs consistent"; exit 0; }

stale="$(grep -rl 'jsonNormalisation/v4alpha1' website/content/docs docs/security 2>/dev/null \
  | while IFS= read -r p; do grep -q "$default" "$p" || echo "$p"; done)"
if [[ -n $stale ]]; then
  echo "REPRODUCED: pages naming v4alpha1 but never $default:"
  echo "$stale" | sed 's/^/  /'
  grep -rn 'signed successfully.*v4alpha1\|currently `jsonNormalisation/v4alpha1`\|(`jsonNormalisation/v4alpha1`)' website/content/docs docs/security | head -5
  exit 1
fi
echo "OK"
