#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup redirect
add_source wget
config=$(uploader "$URL/redirect" PUT)
if transfer "$config"; then
    printf 'OBSERVATION: transfer accepted redirect response\n'
else
    grep -Eiq 'redirect|status.*302|digest.*mismatch' "$WORK/transfer.stderr" || { cat "$WORK/transfer.stderr" >&2; exit 1; }
    printf 'OBSERVATION: transfer rejected redirect response\n'
    cat "$WORK/transfer.stderr"
fi
jq -se 'any(.[]; .path == "/redirect" and .method == "PUT" and .body_size == 24)' "$WORK/requests.jsonl" >/dev/null
jq -s '{followed_get:any(.[]; .path == "/page" and .method == "GET"), requests:[.[] | select(.path == "/redirect" or .path == "/page") | {method,path,body_size}]}' "$WORK/requests.jsonl"
curl -fsS "$URL/state" >"$WORK/state.json"
jq '{stored_objects:(.objects | length)}' "$WORK/state.json"
