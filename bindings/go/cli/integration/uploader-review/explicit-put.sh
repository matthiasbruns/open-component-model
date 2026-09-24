#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup explicit-put
add_source wget
config=$(uploader "$URL/target/put" PUT)
transfer "$config"
curl -fsS "$URL/source/blob" >"$WORK/expected.bin"
curl -fsS "$URL/state" >"$WORK/upload-state.json"
file=$(jq -er '.objects["/target/put"].file' "$WORK/upload-state.json")
cmp "$WORK/expected.bin" "$WORK/$file"
jq -se 'any(.[]; .path == "/target/put" and .method == "PUT" and .body_size == 24)' "$WORK/requests.jsonl" >/dev/null
if ocm download download resource "$RESOURCE_TARGET" --identity name=blob --output "$WORK/download.bin"; then
    download_status=0
else
    download_status=$?
fi
curl -fsS "$URL/state" >"$WORK/readback-state.json"
if jq -e '.objects["/target/put"].size == 0' "$WORK/readback-state.json" >/dev/null &&
    jq -se 'any(.[]; .path == "/target/put" and .method == "PUT" and .body_size == 0)' "$WORK/requests.jsonl" >/dev/null; then
    printf 'REPRODUCED: explicit PUT reused for readback and emptied object (download exit %s)\n' "$download_status"
else
    [[ $download_status == 0 ]] || { cat "$WORK/download.stderr" >&2; exit 1; }
    cmp "$WORK/expected.bin" "$WORK/download.bin"
    cmp "$WORK/expected.bin" "$WORK/$file"
    jq -se 'any(.[]; .path == "/target/put" and .method == "GET")' "$WORK/requests.jsonl" >/dev/null
    printf 'NOT REPRODUCED: readback preserved the object and downloaded bytes\n'
fi
