#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup extra-identity
add_source extra
expression='${"'"$URL"'/target/extra/" + resource.name}'
config=$(uploader "$URL/target/extra" PUT | jq --arg expression "$expression" '
    .targetURL=$expression | .match.extraIdentity={tier:"public"}')
if transfer "$config"; then
    curl -fsS "$URL/state" >"$WORK/state.json"
    file=$(jq -er '.objects["/target/extra/blob"].file' "$WORK/state.json")
    curl -fsS "$URL/source/blob" >"$WORK/expected.bin"
    cmp "$WORK/expected.bin" "$WORK/$file"
    jq -se 'any(.[]; .path == "/target/extra/blob" and .method == "PUT") and all(.[]; .path != "/target/extra/plain")' "$WORK/requests.jsonl" >/dev/null
    printf 'NOT REPRODUCED: matching public resource uploaded to resource.name URL\n'
else
    grep -q 'extraIdentity' "$WORK/transfer.stderr" &&
        grep -Eiq 'overload|check|compile' "$WORK/transfer.stderr" || { cat "$WORK/transfer.stderr" >&2; exit 1; }
    printf 'REPRODUCED: extraIdentity expression failed type checking\n'
fi
