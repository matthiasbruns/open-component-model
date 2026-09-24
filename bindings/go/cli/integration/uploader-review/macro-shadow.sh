#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup macro-shadow
add_source wget
expression='${["x"].map(resource, resource)[0] + resource.name}'
expected='xblob'
config=$(uploader "$URL/target/macro-shadow" PUT | jq --arg expression "$expression" '.header={"X-Review-Template":[$expression]}')
if transfer "$config"; then
    jq -se 'any(.[]; .path == "/target/macro-shadow" and .method == "PUT" and .body_size == 24)' "$WORK/requests.jsonl" >/dev/null
    jq -s '[.[] | select(.path == "/target/macro-shadow") | .headers["X-Review-Template"]]' "$WORK/requests.jsonl"
    if jq -se --arg expected "$expected" 'all(.[] | select(.path == "/target/macro-shadow"); .headers["X-Review-Template"] == [$expected])' "$WORK/requests.jsonl" >/dev/null; then
        printf 'NOT REPRODUCED: macro binding and outer resource resolved correctly\n'
    else
        printf 'REPRODUCED: macro shadowing changed header\n'
    fi
else
    grep -Eiq 'undeclared|overload|syntax error|argument is not an identifier' "$WORK/transfer.stderr" || { cat "$WORK/transfer.stderr" >&2; exit 1; }
    printf 'REPRODUCED: macro shadowing failed expression compilation\n'
fi
