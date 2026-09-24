#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup nested-literal
add_source wget
expression='${resource.name}/${"${resource.name}"}'
expected='blob/${resource.name}'
config=$(uploader "$URL/target/nested-literal" PUT | jq --arg expression "$expression" '.header={"X-Review-Template":[$expression]}')
if transfer "$config"; then
    jq -se 'any(.[]; .path == "/target/nested-literal" and .method == "PUT" and .body_size == 24)' "$WORK/requests.jsonl" >/dev/null
    jq -s '[.[] | select(.path == "/target/nested-literal") | .headers["X-Review-Template"]]' "$WORK/requests.jsonl"
    if jq -se --arg expected "$expected" 'all(.[] | select(.path == "/target/nested-literal"); .headers["X-Review-Template"] == [$expected])' "$WORK/requests.jsonl" >/dev/null; then
        printf 'NOT REPRODUCED: nested literal preserved\n'
    else
        printf 'REPRODUCED: nested literal header changed\n'
    fi
else
    grep -Eiq 'undeclared|overload|syntax error|argument is not an identifier' "$WORK/transfer.stderr" || { cat "$WORK/transfer.stderr" >&2; exit 1; }
    printf 'REPRODUCED: nested literal failed expression compilation\n'
fi
