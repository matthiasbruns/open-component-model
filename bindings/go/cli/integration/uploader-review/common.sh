#!/usr/bin/env bash
# Source from Bash scenario scripts. Artifacts are deliberately retained.
REVIEW_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
CLI_DIR=$(cd -- "$REVIEW_DIR/../.." && pwd)

# Stop only this command's descendants, including the executable spawned by go run.
_kill_tree() (
    local pid=$1 child
    kill -STOP "$pid" 2>/dev/null || return 0
    for child in $(ps -axo pid=,ppid= | awk -v parent="$pid" '$2 == parent {print $1}'); do
        _kill_tree "$child"
    done
    kill -KILL "$pid" 2>/dev/null || :
)

_cleanup() {
    if [[ -n ${FIXTURE_PID:-} ]]; then
        curl -fsS --max-time 3 "$URL/shutdown" >/dev/null 2>&1 || _kill_tree "$FIXTURE_PID"
        wait "$FIXTURE_PID" 2>/dev/null || :
        FIXTURE_PID=
    fi
}

setup() {
    local case_name=${1:?setup CASE} tool attempt
    [[ $case_name =~ ^[a-zA-Z0-9_-]+$ ]] || return 2
    for tool in go jq curl; do command -v "$tool" >/dev/null || return 1; done
    WORK=$(mktemp -d "${TMPDIR:-/tmp}/uploader-review-$case_name.XXXXXX") || return
    printf 'Artifacts retained: %s\n' "$WORK"
    mkdir -p "$WORK/plugins"
    printf '%s\n' '{"type":"generic.config.ocm.software/v1","configurations":[]}' >"$WORK/base-config.json"
    URL=
    (cd "$REVIEW_DIR" && exec go run server.go --dir "$WORK") >"$WORK/server.stdout" 2>"$WORK/server.stderr" &
    FIXTURE_PID=$!
    trap _cleanup EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    for ((attempt=0; attempt<300; attempt++)); do
        if [[ -s $WORK/url ]]; then
            URL=$(cat "$WORK/url")
            if curl -fsS --max-time 1 "$URL/state" >"$WORK/initial-state.json"; then return 0; fi
        fi
        kill -0 "$FIXTURE_PID" 2>/dev/null || break
        sleep 0.1
    done
    printf 'Fixture startup failed: %s/server.stderr\n' "$WORK" >&2
    _kill_tree "$FIXTURE_PID"
    wait "$FIXTURE_PID" 2>/dev/null || :
    FIXTURE_PID=
    return 1
}

ocm() (
    local label=${1:?ocm LABEL args...} pid watchdog status
    shift
    [[ $label =~ ^[a-zA-Z0-9_-]+$ ]] || return 2
    mkdir -p "$WORK/$label-temp"
    (cd "$CLI_DIR" && exec go run main.go --config "${CONFIG:-$WORK/base-config.json}" \
        --plugin-directory "$WORK/plugins" --temp-folder "$WORK/$label-temp" "$@") \
        >"$WORK/$label.stdout" 2>"$WORK/$label.stderr" &
    pid=$!
    (
        sleep "${OCM_TIMEOUT:-90}"
        printf 'CLI timed out after %ss\n' "${OCM_TIMEOUT:-90}" >>"$WORK/$label.stderr"
        _kill_tree "$pid"
    ) &
    watchdog=$!
    trap '_kill_tree "$watchdog"; _kill_tree "$pid"' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    if wait "$pid"; then status=0; else status=$?; fi
    { _kill_tree "$watchdog"; wait "$watchdog" || :; } 2>/dev/null
    trap - EXIT
    return "$status"
)

add_source() {
    local kind=${1:-wget}
    case "$kind" in wget|oci|large|extra) ;; *) printf 'Unknown source: %s\n' "$kind" >&2; return 2 ;; esac
    SOURCE="ctf::$WORK/source//ocm.software/uploader-review:1.0.0"
    TARGET="ctf::$WORK/target"
    RESOURCE_TARGET="$TARGET//ocm.software/uploader-review:1.0.0"
    jq -n --arg url "$URL" --arg kind "$kind" '
        {name:"blob",version:"1.0.0",relation:"external",type:"blob",
         access:{type:"wget/v1",url:($url + if $kind == "large" then "/source/large" else "/source/blob" end)}}
        | if $kind == "oci" then .type="ociArtifact" |
            .access={type:"ociArtifact/v1",imageReference:($url+"/review/image:latest")} else . end
        | (if $kind == "extra" then [(.name="plain"),(.extraIdentity={tier:"public"})] else [.] end) as $resources
        | {components:[{name:"ocm.software/uploader-review",version:"1.0.0",
            provider:{name:"ocm.software"},resources:$resources}]}
    ' >"$WORK/constructor.json" || return
    ocm add add cv --repository "ctf::$WORK/source" --constructor "$WORK/constructor.json" || return
    ocm source get cv "$SOURCE" -o json
}

uploader() {
    jq -n --arg url "${1:?uploader URL [METHOD]}" --arg method "${2:-}" '
        {type:"http.uploader.transfer.config.ocm.software/v1alpha1",
         match:{accessType:"wget/v1"},targetURL:$url}
        | if $method != "" then .method=$method else . end'
}

transfer() {
    jq -n --argjson uploader "${1:?transfer JSON_UPLOADER}" \
        '{type:"generic.config.ocm.software/v1",configurations:[$uploader]}' >"$WORK/transfer-config.json" || return
    CONFIG="$WORK/transfer-config.json" ocm transfer transfer cv "$SOURCE" "$TARGET"
}
