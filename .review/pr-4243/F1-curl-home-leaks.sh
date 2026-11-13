#!/usr/bin/env bash
# F1: the sigstore integration Taskfile's top-level `env: CURL_HOME` leaks into every task
# of the repo-root Taskfile, because the root includes it (Taskfile.yml, "bindings/go/sigstore/integration").
#
# Harness: a scratch root Taskfile that includes the real sigstore Taskfile with the same
# stanza as the repo root, plus a sibling Taskfile whose task prints $CURL_HOME.
# Contract: exit 0 = CURL_HOME not visible to the unrelated task (correct),
#           exit 1 = reproduced (leaks), other = inconclusive.
# Usage: F1-curl-home-leaks.sh [repo-root]   (default: this checkout)
set -uo pipefail
REPO=$(cd "${1:-$(dirname "$0")/../..}" && pwd)
SIG="$REPO/bindings/go/sigstore/integration"
[ -f "$SIG/Taskfile.yml" ] || { echo "no sigstore Taskfile at $SIG" >&2; exit 2; }
command -v task >/dev/null || { echo "task not installed" >&2; exit 2; }
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/other"
cat > "$TMP/Taskfile.yml" <<YAML
version: '3'
includes:
  other:
    taskfile: ./other/Taskfile.yml
    dir: ./other
  bindings/go/sigstore/integration:
    optional: true
    taskfile: $SIG/Taskfile.yml
    dir: $SIG
tasks:
  rootprobe:
    cmds: ['echo "CURL_HOME=[\${CURL_HOME:-}]"']
YAML
cat > "$TMP/other/Taskfile.yml" <<'YAML'
version: '3'
tasks:
  probe:
    cmds: ['echo "CURL_HOME=[${CURL_HOME:-}]"']
YAML
echo "task $(task --version)"
unset CURL_HOME
out_other=$(cd "$TMP" && task --silent other:probe 2>&1) || { echo "$out_other" >&2; exit 2; }
out_root=$(cd "$TMP" && task --silent rootprobe 2>&1) || { echo "$out_root" >&2; exit 2; }
echo "other:probe -> $out_other"
echo "rootprobe   -> $out_root"
if [ "$out_other" != "CURL_HOME=[]" ] || [ "$out_root" != "CURL_HOME=[]" ]; then
  echo "REPRODUCED: CURL_HOME from the sigstore Taskfile is set in unrelated tasks"
  exit 1
fi
echo "OK: unrelated tasks do not see CURL_HOME"
exit 0
