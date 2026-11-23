#!/usr/bin/env bash
# PR 4341 / F1: with two nested pathprefix entries the CLI sends the broader entry's credentials.
#
# Expected: for a repository below my-org/team the CLI authenticates as team-user, the entry
#           with the longest matching prefix (what OCM v1 does with the same config).
# Actual on d84721ee8f: every run authenticates as org-user.
#
# The script starts a registry stub on 127.0.0.1 that challenges with Basic auth and records
# the username of each authenticated request. It contacts nothing else.
#
# Contract (verify.sh): exit 0 = behaves correctly, exit 1 = finding reproduced,
# anything else = setup problem (inconclusive). Run from any checkout:
#   REPO_ROOT=/path/to/checkout bash .review/pr-4341/F1-cli-nested-pathprefix.sh
# Set OCM_BIN to test an existing binary (for example an OCM v1 CLI) instead of building one.
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
RUNS="${RUNS:-20}"
WORK="$(mktemp -d)"
STUB_PID=""
cleanup() {
  if [[ -n "$STUB_PID" ]]; then kill "$STUB_PID" 2>/dev/null || true; wait "$STUB_PID" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

if [[ -n "${OCM_BIN:-}" ]]; then
  OCM="$OCM_BIN"
else
  (cd "$REPO_ROOT/bindings/go/cli" && CGO_ENABLED=0 go build -o "$WORK/ocm" .) || setup_fail "cli build"
  OCM="$WORK/ocm"
fi

# --- registry stub ---------------------------------------------------------------
cat > "$WORK/stub.py" <<'PY'
import base64, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port_file, log_file = sys.argv[1], sys.argv[2]

class Handler(BaseHTTPRequestHandler):
    def _answer(self):
        auth = self.headers.get("Authorization", "")
        if auth.startswith("Basic "):
            user = base64.b64decode(auth[6:]).decode().split(":", 1)[0]
            with open(log_file, "a") as f:
                f.write(user + "\n")
            body = b'{"errors":[{"code":"NAME_UNKNOWN","message":"stub"}]}'
            self.send_response(404)
        else:
            body = b'{"errors":[{"code":"UNAUTHORIZED","message":"stub"}]}'
            self.send_response(401)
            self.send_header("WWW-Authenticate", 'Basic realm="review"')
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)
    do_GET = do_HEAD = do_POST = do_PUT = _answer
    def log_message(self, *args):
        pass

server = HTTPServer(("127.0.0.1", 0), Handler)
with open(port_file, "w") as f:
    f.write(str(server.server_address[1]))
server.serve_forever()
PY
: > "$WORK/users.log"
python3 -I "$WORK/stub.py" "$WORK/port" "$WORK/users.log" &
STUB_PID=$!
for _ in $(seq 50); do [[ -s "$WORK/port" ]] && break; sleep 0.1; done
[[ -s "$WORK/port" ]] || setup_fail "registry stub did not start"
PORT="$(cat "$WORK/port")"

# --- fixture: an OCM v1 style config with two nested pathprefix entries -----------
cat > "$WORK/ocmconfig" <<EOF
type: generic.config.ocm.software/v1
configurations:
  - type: http.config.ocm.software/v1alpha1
    hosts:
      "127.0.0.1:$PORT":
        plainHTTP: true
  - type: credentials.config.ocm.software
    consumers:
      - identity:
          type: OCIRegistry
          hostname: 127.0.0.1
          port: "$PORT"
          pathprefix: my-org
        credentials:
          - type: Credentials
            properties:
              username: org-user
              password: org-password
      - identity:
          type: OCIRegistry
          hostname: 127.0.0.1
          port: "$PORT"
          pathprefix: my-org/team
        credentials:
          - type: Credentials
            properties:
              username: team-user
              password: team-password
EOF

# --- exercise: every run is a new process and builds a new credential graph ---------
REF="${REF:-127.0.0.1:$PORT/my-org/team//ocm.software/review:1.0.0}"
for _ in $(seq "$RUNS"); do
  "$OCM" get componentversion "$REF" --config "$WORK/ocmconfig" >"$WORK/last.out" 2>&1 || true
done

# --- assert the CORRECT behavior ---------------------------------------------------
[[ -s "$WORK/users.log" ]] || { tail -5 "$WORK/last.out" >&2; setup_fail "no authenticated request reached the stub"; }
echo "usernames sent over $RUNS runs (authenticated requests):"
sort "$WORK/users.log" | uniq -c
if grep -qvx 'team-user' "$WORK/users.log"; then
  echo "REPRODUCED: a repository below my-org/team was accessed with credentials other than team-user"; exit 1
fi
echo "OK: every authenticated request used team-user"
