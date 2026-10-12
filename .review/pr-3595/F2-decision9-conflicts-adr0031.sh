#!/usr/bin/env bash
# PR 3595 / F2: decision 9 (large-blob splitting) restates and contradicts ADR 0031, merged on main (#3626).
#
# Expected: the VM ADR defers to docs/adr/0031_oci_local_blob_chunking.md and does not reopen or
#           contradict what 0031 decided.
# Actual on 0ce47b76d: no reference to 0031; proposes a chunkIndex annotation (0031: "no chunk-index
#           annotation"); lists an input-level split trigger (0031: "Keep chunking outside input methods");
#           leaves boundary strategy / whether to split open (0031: fixed-size, opt-in).
#
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout MAIN_REF=upstream/main bash .review/pr-3595/F2-decision9-conflicts-adr0031.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
MAIN_REF="${MAIN_REF:-upstream/main}"
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

adr=""
for f in "$REPO_ROOT"/docs/adr/*_vm_image_packaging.md; do [[ -f "$f" ]] && { adr="$f"; break; }; done
[[ -n "$adr" ]] || setup_fail "VM image packaging ADR not present in this checkout"
c31="$(git -C "$REPO_ROOT" show "$MAIN_REF:docs/adr/0031_oci_local_blob_chunking.md" 2>/dev/null)" || setup_fail "ADR 0031 not on $MAIN_REF"

problems=0
report() { echo "  - $1"; problems=$((problems+1)); }

grep -q '0031' "$adr" || report "VM ADR never references ADR 0031"

if grep -q 'no chunk-index annotation' <<<"$c31" && grep -q 'chunkIndex' "$adr"; then
  report "chunk order: VM ADR uses chunkIndex (lines $(grep -n 'chunkIndex' "$adr" | cut -d: -f1 | paste -sd, -)); 0031: 'manifest layer order is content order; no chunk-index annotation'"
fi
if grep -q 'Keep chunking outside input methods' <<<"$c31" && grep -qE 'split: *\{ *maxLayerSize' "$adr"; then
  report "trigger: VM ADR lists input-level split (line $(grep -nE 'split: *\{' "$adr" | cut -d: -f1)); 0031: 'Keep chunking outside input methods', global maxChunkSize config"
fi
if grep -q 'Use fixed-size chunks' <<<"$c31" && grep -q 'Open — chunk-boundary strategy' "$adr"; then
  report "boundaries: VM ADR leaves boundary strategy open; 0031: 'Use fixed-size chunks', CDC as later extension"
fi
if grep -q 'Chunking is opt-in' <<<"$c31" && grep -q 'Open — should splitting be offered at all' "$adr"; then
  report "VM ADR leaves 'should splitting be offered' open; 0031: 'Chunking is opt-in'"
fi

if [[ $problems -gt 0 ]]; then
  echo "REPRODUCED: $problems conflict(s) between $(basename "$adr") and ADR 0031 on $MAIN_REF"
  exit 1
fi
echo "OK: VM ADR is consistent with ADR 0031"
