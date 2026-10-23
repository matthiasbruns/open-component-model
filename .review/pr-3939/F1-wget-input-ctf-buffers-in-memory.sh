#!/usr/bin/env bash
# PR #3939 F1: a wget input without a pinned or advertised SHA-256, constructed into a CTF,
# is buffered in memory (oci/blob ArtifactBlob.Buffer) because the streaming blob no longer
# reports a digest. Exit 0 = peak RSS stays well below the content size (correct),
# 1 = reproduced (peak RSS grows with the content), other = inconclusive.
# Env: SIZE (bytes, default 256 MiB), ADVERTISE=1 (control: send X-Checksum-Sha256),
#      OCM_BIN (prebuilt CLI; otherwise built from this checkout), PORT.
set -u
HERE=$(cd "$(dirname "$0")" && pwd); ROOT=$(cd "$HERE/../.." && pwd)
SIZE=${SIZE:-$((256<<20))}; PORT=${PORT:-18939}; export SIZE
D=$(mktemp -d); SP=""; trap '[ -n "$SP" ] && kill $SP 2>/dev/null; rm -rf "$D"' EXIT
if [ -z "${OCM_BIN:-}" ]; then
  OCM_BIN="$D/ocm"; (cd "$ROOT/bindings/go" && go build -o "$OCM_BIN" ./cli) || exit 2
fi
python3 -I "$HERE/F1-server.py" "$PORT" 2>"$D/srv.log" & SP=$!
for _ in $(seq 50); do curl -sfI "http://127.0.0.1:$PORT/" >/dev/null 2>&1 && break; sleep 0.2; done
cat > "$D/component-constructor.yaml" <<YAML
components:
  - name: ocm.software/review/wget-big
    version: v1.0.0
    provider: {name: ocm.software}
    resources:
      - name: big
        version: v1.0.0
        type: blob
        relation: local
        input:
          type: wget
          url: http://127.0.0.1:$PORT/big.bin
YAML
/usr/bin/time -l "$OCM_BIN" add cv --repository "ctf::$D/ctf" --constructor "$D/component-constructor.yaml" >"$D/out.log" 2>"$D/time.log" || { cat "$D/time.log"; exit 2; }
RSS=$(awk '/maximum resident set size/{print $1}' "$D/time.log")
echo "size_MiB=$((SIZE>>20)) maxRSS_MiB=$((RSS>>20)) GETs=$(grep -c '^REQ GET' "$D/srv.log")"
[ "$RSS" -lt $((SIZE/2)) ] && exit 0 || exit 1
