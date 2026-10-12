#!/usr/bin/env bash
# PR 3595 / F4: the VM format label name does not follow the OCM label naming scheme.
#
# ocm-spec doc/01-model/07-extensions.md ("Label Types"): label names are "composed of a DNS domain
# owned by the providing organization, followed by a slash and a kebab-case local name"; labels with a
# predefined OCM meaning "use the reserved ocm.software DNS prefix"; every label must define a
# (versioned) specification of its value space.
#
# Expected: every label name the ADR defines uses ocm.software or a subdomain of it, and its examples
#           carry a label `version`.
# Actual on 0ce47b76d: `software.ocm.vm/format` (reversed domain nobody owns), examples without `version`.
#
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout bash .review/pr-3595/F4-label-name-not-owned-domain.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }
adr="$(ls "$REPO_ROOT"/docs/adr/*_vm_image_packaging.md 2>/dev/null | head -1 || true)"
[[ -n "$adr" ]] || setup_fail "VM image packaging ADR not present in this checkout"

problems=0
# Label names used in YAML examples (`- name: <label>` directly under `labels:`).
while IFS= read -r name; do
  domain="${name%%/*}"
  if [[ "$domain" != "ocm.software" && "$domain" != *.ocm.software ]]; then
    echo "  - label '$name': domain '$domain' is not ocm.software or a subdomain of it"
    problems=$((problems+1))
  fi
done < <(awk '/labels:/{l=1; next} l && /- name:/{print $3; next} l && !/^ +(value|signing|version|-)/{l=0}' "$adr" | sort -u)

# Examples of the format label without a `version` field.
missing="$(awk '/- name: [^ ]+\/format/{f=1; n=NR; v=0; next} f && /version:/{v=1} f && (/- name:/ || /access:|input:/){ if(!v) print n; f=0 }' "$adr" | paste -sd, -)"
if [[ -n "$missing" ]]; then
  echo "  - format label without 'version' at line(s) $missing"
  problems=$((problems+1))
fi

if [[ $problems -gt 0 ]]; then echo "REPRODUCED: $problems label naming problem(s) in $(basename "$adr")"; exit 1; fi
echo "OK: label names follow the spec"
