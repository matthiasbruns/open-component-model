#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup triple-quotes
add_source wget
expression='${"""a " resource.name " b""" + resource.name}'
expected='a " resource.name " bblob'
config=$(uploader "$URL/target/triple-quotes" PUT | jq --arg expression "$expression" '.header={"X-Review-Template":[$expression]}')
if transfer "$config"; then
    jq -se 'any(.[]; .path == "/target/triple-quotes" and .method == "PUT" and .body_size == 24)' "$WORK/requests.jsonl" >/dev/null
    jq -s '[.[] | select(.path == "/target/triple-quotes") | .headers["X-Review-Template"]]' "$WORK/requests.jsonl"
    if jq -se --arg expected "$expected" 'all(.[] | select(.path == "/target/triple-quotes"); .headers["X-Review-Template"] == [$expected])' "$WORK/requests.jsonl" >/dev/null; then
        printf 'NOT REPRODUCED: triple-quoted literal preserved\n'
    else
        printf 'REPRODUCED: triple-quoted literal header changed\n'
    fi
else
    grep -Eiq 'undeclared|overload|syntax error|argument is not an identifier' "$WORK/transfer.stderr" || { cat "$WORK/transfer.stderr" >&2; exit 1; }
    printf 'REPRODUCED: triple-quoted literal failed expression compilation\n'
fi
