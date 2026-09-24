#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup query-token
add_source wget
token='SYNTHETIC-REVIEW-TOKEN-NOT-A-SECRET'
config=$(uploader "$URL/malformed?token=$token" PUT)
if transfer "$config"; then
    printf 'Unexpected success for malformed HTTP response\n' >&2
    exit 1
else
    grep -qi 'malformed HTTP' "$WORK/transfer.stderr" || { cat "$WORK/transfer.stderr" >&2; exit 1; }
    if grep -i 'malformed HTTP' "$WORK/transfer.stderr" | grep -Fq "$token"; then
        printf 'REPRODUCED: synthetic query token appears in malformed HTTP error\n'
    else
        printf 'NOT REPRODUCED: malformed HTTP error does not expose synthetic token\n'
    fi
fi
jq -se --arg path "/malformed?token=$token" 'any(.[]; .path == $path and .method == "PUT" and .body_size == 24)' "$WORK/requests.jsonl" >/dev/null
