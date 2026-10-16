#!/usr/bin/env bash
# PR 3887 / R1: jsonNormalisation/v1 bytes change across the OCI store round trip.
#
# Expected: a descriptor normalises to the same bytes before and after it is
#   stored and read back (yaml.Marshal in oci/spec/descriptor/encode.go,
#   sigs.k8s.io/yaml.Unmarshal for every media type in decode.go).
# Actual on 5164e368: signing labels with `1.0`, `1.0e+5` (since eac7582a) or an
#   object with unsorted keys inside a list (since 5164e368) normalise
#   differently in memory than after the round trip, so an in-memory signature
#   does not verify against the stored copy.
#
# Contract: exit 0 = behaves correctly, exit 1 = finding reproduced,
# anything else = setup problem (inconclusive). Run from any checkout:
#   REPO_ROOT=/path/to/checkout bash .review/pr-3887/R1-storage-roundtrip.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
PKG="$REPO_ROOT/bindings/go/descriptor"
[[ -f "$PKG/normalisation/json/v1/review_pr3887_roundtrip_test.go" ]] || { echo "SETUP FAILED: test file missing" >&2; exit 2; }

set +e
out="$(go -C "$PKG" test ./normalisation/json/v1/ -run 'TestReviewPR3887_R1_' -count=1 -v 2>&1)"
rc=$?
set -e
echo "$out" | grep -E '^\s*--- (PASS|FAIL)|^(ok|FAIL)\s'

if echo "$out" | grep -qE 'build failed|cannot|undefined|setup failed'; then
  echo "SETUP FAILED: test did not build" >&2; exit 2
fi
if [[ $rc -ne 0 ]]; then
  echo "REPRODUCED: normalised bytes change across the OCI store round trip"; exit 1
fi
echo "OK: normalisation is stable across the store round trip"
