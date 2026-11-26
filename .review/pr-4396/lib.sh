# Shared setup for the PR 4396 CLI scripts. Source it, then call download_with <config> <n>.
# Builds the ocm CLI from $REPO_ROOT, starts the stdlib registry from ./registry (local only),
# and adds a component version to a CTF whose resource references 127.0.0.1:<port>/project1/app:1.0.0
# by value-less ociImage access, so every download needs credentials for project1/app.
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="$(mktemp -d)"
REG_PID=""
cleanup() { [[ -n "$REG_PID" ]] && kill "$REG_PID" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT

setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

(cd "$REPO_ROOT/bindings/go/cli" && CGO_ENABLED=0 go build -o "$WORK/ocm" .) || setup_fail "cli build"
OCM="$WORK/ocm"

mkdir -p "$WORK/registry"
cp "$SCRIPT_DIR/registry/main.go" "$WORK/registry/main.go"
(cd "$WORK/registry" && printf 'module reviewregistry\n\ngo 1.24\n' > go.mod && GOFLAGS= GOWORK=off go build -o "$WORK/registry-bin" .) || setup_fail "registry build"
"$WORK/registry-bin" -addr-file "$WORK/addr" 2>"$WORK/registry.log" &
REG_PID=$!
for _ in $(seq 50); do [[ -s "$WORK/addr" ]] && break; sleep 0.1; done
[[ -s "$WORK/addr" ]] || setup_fail "registry did not start"
ADDR="$(cat "$WORK/addr")"
HOST="${ADDR%:*}"
PORT="${ADDR##*:}"

cat > "$WORK/constructor.yaml" <<EOF
components:
  - name: ocm.software/review/pr4396
    version: 1.0.0
    provider:
      name: ocm.software
    resources:
      - name: hello
        version: 1.0.0
        type: ociImage
        relation: external
        access:
          type: OCIImage/v1
          imageReference: $ADDR/project1/app:1.0.0
EOF

"$OCM" add component-version --repository "ctf::$WORK/ctf" --constructor "$WORK/constructor.yaml" \
  --skip-reference-digest-processing >"$WORK/add.log" 2>&1 || { cat "$WORK/add.log" >&2; setup_fail "add component-version"; }

# entry <username> <password> [path]
entry() {
  cat <<EOF
      - identities:
          - type: OCIRegistry/v1
            hostname: $HOST
            port: "$PORT"
EOF
  [[ $# -ge 3 ]] && echo "            path: \"$3\""
  cat <<EOF
        credentials:
          - type: Credentials/v1
            properties:
              username: $1
              password: $2
EOF
}

config_header() {
  cat <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
EOF
}

http_footer() {
  cat <<EOF
  - type: http.config.ocm.software/v1alpha1
    hosts:
      "$ADDR": {plainHTTP: true}
EOF
}

# download_with <config-file> <runs>: prints "ok=<n> failed=<n>"
download_with() {
  local cfg="$1" runs="$2" ok=0 failed=0 i
  for i in $(seq "$runs"); do
    rm -rf "$WORK/out"
    if "$OCM" download resource "ctf::$WORK/ctf//ocm.software/review/pr4396:1.0.0" --identity name=hello \
         --output "$WORK/out" --config "$cfg" >"$WORK/download-$i.log" 2>&1; then
      ok=$((ok + 1))
    else
      failed=$((failed + 1))
      cp "$WORK/download-$i.log" "$WORK/last-failure.log"
    fi
  done
  echo "ok=$ok failed=$failed"
}
