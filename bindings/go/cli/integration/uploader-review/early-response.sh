#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup early-response
add_source large
config=$(uploader "$URL/early" PUT)
if transfer "$config"; then
    printf 'OBSERVATION: transfer accepted early 200 without fixture consuming body\n'
else
    grep -Eiq 'digest.*mismatch|broken pipe|connection reset|closed.*connection|unexpected EOF' "$WORK/transfer.stderr" || { cat "$WORK/transfer.stderr" >&2; exit 1; }
    printf 'OBSERVATION: transfer rejected early response\n'
    cat "$WORK/transfer.stderr"
fi
jq -se 'any(.[]; .path == "/early" and .method == "PUT" and .body_consumed == false)' "$WORK/requests.jsonl" >/dev/null
jq -s '{source_size:33554432,early_requests:[.[] | select(.path == "/early") | {method,body_size,body_consumed}]}' "$WORK/requests.jsonl"
curl -fsS "$URL/state" >"$WORK/state.json"
jq '{stored_objects:(.objects | length)}' "$WORK/state.json"
