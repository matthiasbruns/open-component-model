#!/usr/bin/env bash
# PR 4006 / F3: the OCI-pin fallback error still suggests a v4alpha1 normalized pin
#
# Expected: repository/pin.go suggests the same normalized pin form the CLI help now documents
#           (signing.DefaultNormalisationAlgorithm, i.e. the digest `ocm sign` logs).
# Actual on 51106f985: GetPinnedComponentVersion hard-codes v4alpha1.Algorithm in the hint.
#
# Contract: exit 0 = consistent, exit 1 = reproduced, other = inconclusive.
set -euo pipefail
REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
f="$REPO_ROOT/bindings/go/repository/pin.go"
[[ -f $f ]] || { echo "SETUP FAILED: $f missing" >&2; exit 2; }
grep -q 'DefaultNormalisationAlgorithm' "$REPO_ROOT/bindings/go/signing/digest.go" \
  || { echo "OK: no new default on this revision (v4alpha1 hint is consistent)"; exit 0; }
if grep -n 'NormalisationAlgorithm: v4alpha1.Algorithm' "$f"; then
  echo "REPRODUCED: pin hint still suggests jsonNormalisation.v4alpha1+<hash>:<hex>"; exit 1
fi
echo "OK"
