#!/usr/bin/env bash
# Control for F2: count curl attempts against a refused local port with CURL_HOME set to
# (a) the sigstore hack dir (what the PR intends) and (b) the leaked <bindings/go>/hack
# (what the CI path delivers). --retry-delay 1 only shortens the wait; the retry count
# still comes from hack/.curlrc. Exit 0 = both get retries, 1 = leaked dir gets none.
set -uo pipefail
REPO=$(cd "${1:-$(dirname "$0")/../..}" && pwd)
attempts() { CURL_HOME="$1" HOME=/nonexistent XDG_CONFIG_HOME= curl -sS --retry-delay 1 http://127.0.0.1:9/ 2>&1 | grep -c 'Failed to connect'; }
curl --version | head -1
a=$(attempts "$REPO/bindings/go/sigstore/integration/hack")
b=$(attempts "$REPO/bindings/go/hack")
echo "attempts with CURL_HOME=sigstore/integration/hack: $a"
echo "attempts with CURL_HOME=bindings/go/hack (CI path): $b"
[ "$a" -gt 1 ] || { echo "control failed: intended curlrc not applied"; exit 2; }
[ "$b" -gt 1 ] && { echo "OK"; exit 0; }
echo "REPRODUCED: on the CI path curl makes a single attempt"; exit 1
