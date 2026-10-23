#!/usr/bin/env bash
# PR 3978 / F3: two of the "80 pass" results of the tailored GPOS SRG profile
# describe the CI runner's kernel, not the image. SV-203753 (non-executable
# data) and SV-203754 (ASLR) are SCE scripts that run
# `sysctl kernel.randomize_va_space` inside the scanner container; SCE does not
# honour OSCAP_PROBE_ROOT.
#
# Expected: rules whose result cannot come from the image root filesystem are
#           not reported as passing for it (deselected in tailoring.xml, or
#           otherwise marked as runtime/host requirements).
# Actual on 9bcb427b0: both pass even when the scanned root filesystem is empty.
#
# Contract: exit 0 = behaves correctly, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout bash .review/pr-3978/F3-sce-rules-check-runner-kernel.sh
# Needs Docker; pulls OPENSCAP_IMAGE from .env (digest-pinned).
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

[[ -f "$REPO_ROOT/.github/stig/tailoring.xml" ]] || setup_fail "no .github/stig/tailoring.xml"
OPENSCAP_IMAGE="$(sed -n 's/^OPENSCAP_IMAGE=//p' "$REPO_ROOT/.env")"
[[ -n "$OPENSCAP_IMAGE" ]] || setup_fail "no OPENSCAP_IMAGE in .env"
mkdir -p "$WORK/rootfs" "$WORK/out"   # an empty "image"

# Same invocation as the STIG step in image-scan.yml.
docker run --rm -u 0:0 -e OSCAP_PROBE_ROOT=/rootfs \
  -v "$WORK/rootfs:/rootfs:ro" -v "$REPO_ROOT/.github/stig:/stig:ro" -v "$WORK/out:/out" \
  --entrypoint /bin/sh "$OPENSCAP_IMAGE" -c '
    oscap xccdf eval --tailoring-file /stig/tailoring.xml --profile xccdf_software.ocm_profile_gpos-container \
      --results /out/gpos-results.xml /usr/share/xml/scap/ssg/content/ssg-chainguard-gpos-ds.xml >/out/log 2>&1
    grep -c "sysctl kernel.randomize_va_space" /usr/share/xml/scap/ssg/content/ssg-chainguard-gpos-ds.xml > /out/sce' ||
  setup_fail "oscap run"
[[ -s "$WORK/out/gpos-results.xml" ]] || setup_fail "no results"
echo "SCE scripts calling sysctl in the datastream: $(cat "$WORK/out/sce")"

reproduced=0
for rule in SV-203753 SV-203754; do
  res="$(grep -A40 "rule-result idref=\"xccdf_mil.disa.stig_rule_${rule}r" "$WORK/out/gpos-results.xml" |
    sed -n 's:.*<result>\([a-z]*\)</result>.*:\1:p' | head -1)"
  echo "$rule on an empty root filesystem: ${res:-missing}"
  [[ "$res" == pass ]] && reproduced=1
done
[[ $reproduced -eq 1 ]] && { echo "REPRODUCED: host-kernel rules pass for an empty image"; exit 1; }
echo "OK: no host-kernel rule passes for an empty image"
