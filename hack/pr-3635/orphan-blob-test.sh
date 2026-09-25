#!/usr/bin/env bash
# Checks that `ocm add cv` rejects an invalid resource version before uploading
# anything, so no orphaned blob is left in the target repository.
#
# Usage: run from anywhere inside an open-component-model checkout.
# Exits 0 if the check passes, 1 if an orphaned blob is left behind.
set -euo pipefail

REPO=$(git rev-parse --show-toplevel)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

(cd "$REPO/bindings/go" && go build -o "$WORK/ocm" ./cli)

mkdir -p "$WORK/home"
echo hello > "$WORK/data.txt"
cat > "$WORK/cc.yaml" <<EOF
components:
- name: ocm.software/demo
  version: 1.0.0
  provider: {name: ocm.software}
  resources:
  - name: data
    type: blob
    version: foo
    input: {type: file, path: $WORK/data.txt}
EOF

rc=0
HOME="$WORK/home" "$WORK/ocm" add cv --repository "$WORK/ctf" --constructor "$WORK/cc.yaml" >/dev/null 2>&1 || rc=$?
blobs=$( (find "$WORK/ctf/blobs" -type f 2>/dev/null || true) | wc -l | tr -d ' ')

echo "add cv with resource version 'foo': exit=$rc, blobs left in CTF=$blobs"
if [ "$rc" -eq 0 ]; then
  echo "FAIL: invalid resource version was accepted"
  exit 1
fi
if [ "$blobs" -ne 0 ]; then
  echo "FAIL: resource blob was uploaded before version validation (orphaned blob)"
  exit 1
fi
echo "OK: rejected before any upload"
