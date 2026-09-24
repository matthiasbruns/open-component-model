#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup default-method
add_source wget
config=$(uploader "$URL/target/default")
transfer "$config"
curl -fsS "$URL/source/blob" >"$WORK/expected.bin"
curl -fsS "$URL/state" >"$WORK/upload-state.json"
file=$(jq -er '.objects["/target/default"].file' "$WORK/upload-state.json")
cmp "$WORK/expected.bin" "$WORK/$file"
ocm download download resource "$RESOURCE_TARGET" --identity name=blob --output "$WORK/download.bin"
cmp "$WORK/expected.bin" "$WORK/download.bin"
curl -fsS "$URL/state" >"$WORK/readback-state.json"
jq -e '.objects["/target/default"].size == 24' "$WORK/readback-state.json" >/dev/null
jq -se 'any(.[]; .path == "/target/default" and .method == "PUT" and .body_size == 24) and any(.[]; .path == "/target/default" and .method == "GET")' "$WORK/requests.jsonl" >/dev/null
printf 'CONTROL PASSED: default upload uses PUT; CLI readback uses GET and preserves bytes\n'
