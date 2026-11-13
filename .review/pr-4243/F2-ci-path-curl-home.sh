#!/usr/bin/env bash
# F2: on the CI path (`task bindings/go:test/integration` from the repo root) the sigstore
# setup's curls do not get hack/.curlrc, so the retries this PR adds never apply in CI.
#
# CI chain: root Taskfile -> bindings/go:test/integration -> `task -d <sig> exec --`
# (EXEC in bindings/go/Taskfile.yml). The harness copies the real sigstore Taskfile
# unchanged, stubs bindings/go:test/integration to spawn `task -d <sig> cosign:install`
# the same way, and puts a curl shim on PATH that records the CURL_HOME each real curl
# call would see (then fails, so nothing is downloaded).
# Contract: exit 0 = every curl sees <sig>/hack (correct), 1 = reproduced, other = inconclusive.
# Usage: F2-ci-path-curl-home.sh [repo-root]
set -uo pipefail
REPO=$(cd "${1:-$(dirname "$0")/../..}" && pwd)
SRC="$REPO/bindings/go/sigstore"
[ -f "$SRC/integration/hack/.curlrc" ] || { echo "no hack/.curlrc in $REPO (not applicable)" >&2; exit 2; }
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
SIG="$TMP/bindings/go/sigstore/integration"
mkdir -p "$SIG" "$TMP/bindings/go/sigstore/signing/handler/internal" "$TMP/shim"
cp "$SRC/integration/Taskfile.yml" "$SRC/integration/.env" "$SIG/"
cp -R "$SRC/integration/hack" "$SIG/hack"
cp "$SRC/signing/handler/internal/.env" "$TMP/bindings/go/sigstore/signing/handler/internal/.env"
LOG="$TMP/curl.log"
cat > "$TMP/shim/curl" <<SH
#!/bin/sh
echo "\${CURL_HOME:-<unset>}" >> "$LOG"
exit 22
SH
chmod +x "$TMP/shim/curl"
cat > "$TMP/bindings/go/Taskfile.yml" <<'YAML'
version: '3'
tasks:
  test/integration:
    cmds:
      - task --silent -d '{{.TASKFILE_DIR}}/sigstore/integration' cosign:install
YAML
cat > "$TMP/Taskfile.yml" <<'YAML'
version: '3'
includes:
  bindings/go:
    optional: true
    taskfile: ./bindings/go/Taskfile.yml
    dir: ./bindings/go
  bindings/go/sigstore/integration:
    optional: true
    taskfile: ./bindings/go/sigstore/integration/Taskfile.yml
    dir: ./bindings/go/sigstore/integration
YAML
echo "task $(task --version)"
unset CURL_HOME
run() { : > "$LOG"; (cd "$1" && shift && PATH="$TMP/shim:$PATH" task --silent "$@" >/dev/null 2>&1); sort -u "$LOG"; }
direct=$(run "$SIG" cosign:install)
ci=$(run "$TMP" bindings/go:test/integration)
echo "direct  (task -d sig cosign:install):       curl saw CURL_HOME=$direct"
echo "CI path (root -> bindings/go -> task -d):   curl saw CURL_HOME=$ci"
[ "$direct" = "$SIG/hack" ] || { echo "direct run unexpected (harness broken?)"; exit 2; }
[ "$ci" = "$SIG/hack" ] && { echo "OK"; exit 0; }
echo "REPRODUCED: on the CI path curl does not get $SIG/hack/.curlrc"
exit 1
