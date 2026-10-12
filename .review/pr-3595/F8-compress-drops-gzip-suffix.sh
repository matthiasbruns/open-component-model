#!/usr/bin/env bash
# PR 3595 / F8: decision 3 says `compress: true` "appends a +gzip suffix to the media type" and
# recommends it for raw images. Since #3514 a *declared* mediaType is used as-is when compressing,
# and every ADR example declares one. The descriptor then claims an uncompressed raw disk while the
# bytes are gzip, and nothing downstream (including `ocm download resource`) can tell.
#
# Expected (per ADR L93): File input with mediaType vm.disk.raw.v1 + compress: true is recorded as
#   application/vnd.ocm.software.vm.disk.raw.v1+gzip, and a default download yields the raw disk.
# Actual on 0ce47b76d: recorded as ...raw.v1 (no +gzip); the default download writes gzip bytes.
#
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout bash .review/pr-3595/F8-compress-drops-gzip-suffix.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

(cd "$REPO_ROOT/bindings/go/cli" && CGO_ENABLED=0 go build -o "$WORK/ocm" .) || setup_fail "cli build"
OCM="$WORK/ocm"

# build <name> <declared mediaType>: constructs a component with a compressed raw disk, downloads it with defaults.
build() {
  local dir="$WORK/$1" mt="$2"
  mkdir -p "$dir/out"
  head -c 65536 /dev/zero > "$dir/disk.raw"; printf 'MBR' >> "$dir/disk.raw"
  cat > "$dir/component-constructor.yaml" <<YAML
components:
  - name: acme.org/vm/$1
    version: 1.0.0
    provider:
      name: acme.org
    resources:
      - name: disk
        type: VirtualMachineImage
        version: 1.0.0
        input:
          type: File
          path: ./disk.raw
          mediaType: $mt
          compress: true
YAML
  (cd "$dir" && "$OCM" add component-version -r "$dir/ctf" -c component-constructor.yaml >/dev/null 2>&1) || setup_fail "add $1"
  (cd "$dir/out" && "$OCM" download resource "$dir/ctf//acme.org/vm/$1:1.0.0" --identity name=disk >/dev/null 2>&1) || setup_fail "download $1"
  recorded="$("$OCM" get cv "$dir/ctf//acme.org/vm/$1:1.0.0" -o yaml 2>/dev/null | awk '/access:/{a=1} a && /mediaType:/{print $2; exit}')"
  if cmp -s "$dir/out/disk" "$dir/disk.raw"; then content=raw; elif file -b "$dir/out/disk" | grep -q gzip; then content=gzip; else content=other; fi
}

# 1. The ADR's recipe: declared VM media type + compress: true.
build adr-recipe application/vnd.ocm.software.vm.disk.raw.v1
echo "ADR recipe     : recorded mediaType=$recorded, default download content=$content"
adr_recorded="$recorded"; adr_content="$content"

# 2. Possible ADR fix: declare the +gzip media type explicitly (informational).
build explicit-gzip application/vnd.ocm.software.vm.disk.raw.v1+gzip
echo "explicit +gzip : recorded mediaType=$recorded, default download content=$content"

if [[ "$adr_recorded" != *+gzip ]]; then
  echo "REPRODUCED: compressed raw disk recorded as '$adr_recorded' (no +gzip); default download yields $adr_content bytes"
  exit 1
fi
echo "OK: compression is reflected in the media type"
