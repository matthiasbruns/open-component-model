#!/usr/bin/env bash
# PR 3595 / F5: decision 8 says "The default no-transform path writes the raw stream, using the media
# type to choose a file suffix." The CLI default is not no-transform and picks no suffix.
#
# Expected (per ADR L237): default extraction policy is no-transform; the output file gets a suffix
#   derived from the media type (e.g. disk.qcow2).
# Actual on 0ce47b76d: default --extraction-policy is `auto` (decompress + extract recognized archives);
#   the file is named after the resource ('disk').
#
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout bash .review/pr-3595/F5-download-default-policy-and-name.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

(cd "$REPO_ROOT/bindings/go/cli" && CGO_ENABLED=0 go build -o "$WORK/ocm" .) || setup_fail "cli build"
OCM="$WORK/ocm"

cd "$WORK"
printf 'QFI\xfb fake qcow2 disk' > disk.qcow2
cat > component-constructor.yaml <<'YAML'
components:
  - name: acme.org/vm/ubuntu-noble
    version: 24.4.1
    provider:
      name: acme.org
    resources:
      - name: disk
        type: VirtualMachineImage
        version: 24.4.1
        input:
          type: File
          path: ./disk.qcow2
          mediaType: application/vnd.ocm.software.vm.disk.qcow2.v1
YAML
"$OCM" add component-version -r "$WORK/ctf" -c component-constructor.yaml >/dev/null 2>&1 || setup_fail "add component-version"

mkdir out
(cd out && "$OCM" download resource "$WORK/ctf//acme.org/vm/ubuntu-noble:24.4.1" --identity name=disk >/dev/null 2>&1) || setup_fail "download"
help="$("$OCM" download resource --help 2>&1)" || setup_fail "help"
default_policy="$(grep -A2 -- '--extraction-policy' <<<"$help" | grep -oE '\(default [a-z]+\)' | head -1 | tr -d '()' | awk '{print $2}' || true)"
[[ -n "$default_policy" ]] || setup_fail "could not read --extraction-policy default from --help"

echo "output file: $(ls out)"
echo "default --extraction-policy: $default_policy"

problems=0
[[ "$(ls out)" == "disk" ]] && { echo "  - no media-type suffix: output is named after the resource"; problems=$((problems+1)); }
[[ "$default_policy" == auto* ]] && { echo "  - default policy is 'auto', not no-transform"; problems=$((problems+1)); }

if [[ $problems -gt 0 ]]; then echo "REPRODUCED: CLI does not behave as ADR decision 8 describes"; exit 1; fi
echo "OK: CLI matches ADR decision 8"
