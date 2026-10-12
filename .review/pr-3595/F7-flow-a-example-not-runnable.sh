#!/usr/bin/env bash
# PR 3595 / F7: the ADR's Flow A component-constructor.yaml (advertised as runnable) is rejected by the CLI.
#
# Expected: the YAML block under "Packaging Flow A" builds a component version.
# Actual on 0ce47b76d: schema validation fails - '24.04.1' does not match the semver pattern
#   (leading zero in "04"), for both the component and the resource version.
#
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout bash .review/pr-3595/F7-flow-a-example-not-runnable.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

adr=""
for f in "$REPO_ROOT"/docs/adr/*_vm_image_packaging.md; do [[ -f "$f" ]] && { adr="$f"; break; }; done
[[ -n "$adr" ]] || setup_fail "VM image packaging ADR not present in this checkout"
(cd "$REPO_ROOT/bindings/go/cli" && CGO_ENABLED=0 go build -o "$WORK/ocm" .) || setup_fail "cli build"

# First ```yaml block after the Flow A heading, verbatim.
awk '/^#### Packaging Flow A/{a=1} a && /^```yaml/{y=1; next} y && /^```/{exit} y{print}' "$adr" > "$WORK/component-constructor.yaml"
grep -q '^components:' "$WORK/component-constructor.yaml" || setup_fail "could not extract Flow A constructor"
path="$(awk '/path:/{print $2; exit}' "$WORK/component-constructor.yaml")"
(cd "$WORK" && printf 'QFI\xfb fake qcow2' > "$path")

set +e
out="$(cd "$WORK" && ./ocm add component-version -r "$WORK/ctf" -c component-constructor.yaml 2>&1)"
rc=$?
set -e
if [[ $rc -ne 0 ]]; then
  echo "$out" | grep -E "does not match pattern|Error|error" | sed -E 's/pattern .*/pattern <semver>/' | head -5
  echo "REPRODUCED: ADR Flow A constructor fails (exit $rc)"; exit 1
fi
echo "OK: Flow A constructor builds"
