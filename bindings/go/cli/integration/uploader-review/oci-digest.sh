#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
setup oci-digest
add_source oci
config=$(uploader "$URL/target/oci" PUT | jq '.match.accessType="ociArtifact/v1"')
if transfer "$config"; then
    status=0
else
    status=$?
fi
curl -fsS "$URL/state" >"$WORK/state.json"
jq '{manifest_sha256,uploaded:.objects["/target/oci"]}' "$WORK/state.json"
if [[ $status != 0 ]] && grep -qi 'digest' "$WORK/transfer.stderr" &&
    grep -qi 'mismatch' "$WORK/transfer.stderr" &&
    jq -e '.objects["/target/oci"] as $o | $o.size > 0 and $o.sha256 != .manifest_sha256' "$WORK/state.json" >/dev/null; then
    printf 'REPRODUCED: uploaded OCI bytes differ from manifest digest; transfer reports mismatch\n'
else
    printf 'NOT REPRODUCED: OCI transfer exit %s (unrecognized failures are observations)\n' "$status"
    if [[ $status != 0 ]]; then cat "$WORK/transfer.stderr"; fi
fi
