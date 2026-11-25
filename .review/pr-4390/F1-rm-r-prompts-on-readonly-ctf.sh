#!/usr/bin/env bash
# PR 4390 / F1: `task prepare` cleanup (`trap 'rm -r -- "$work"' EXIT`) prompts for every
# read-only CTF blob when run from a terminal, and leaves the temp CTF behind if declined.
#
# Expected: running an example's `task prepare` in a terminal finishes without prompts and
#           removes its temporary CTF.
# Actual on dd7ebff866: `ocm add cv` writes CTF blobs and artifact-index.json as 0444, so
#           `rm -r` (no -f) asks "override r--r--r-- ...?" per file when stdin is a TTY.
#           Answering "n" (or Enter) leaves the directory, and the task still exits 0.
#           e2e never sees this: Go's exec gives the recipe a non-TTY stdin.
#
# Uses replication-simple (local file input only, no network) and a CTF transfer target.
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout bash .review/pr-4390/F1-rm-r-prompts-on-readonly-ctf.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
cleanup() { chmod -R u+w "$WORK" 2>/dev/null || true; rm -rf "$WORK"; }
trap cleanup EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

EX="$REPO_ROOT/bindings/go/kubernetes/controller/examples/replication-simple"
if [[ ! -f "$EX/Taskfile.yml" ]]; then
  echo "OK: no per-example Taskfile recipe on this revision"; exit 0
fi
command -v task >/dev/null || setup_fail "task not installed"
command -v script >/dev/null || setup_fail "script(1) not installed"

if [[ -n "${OCM_BIN:-}" ]]; then cp "$OCM_BIN" "$WORK/ocm"
else (cd "$REPO_ROOT/bindings/go/cli" && CGO_ENABLED=0 go build -o "$WORK/ocm" .) || setup_fail "cli build"; fi
mkdir -p "$WORK/home/.docker" && echo '{}' > "$WORK/home/.docker/config.json"
cp -R "$EX" "$WORK/example"
printf 'n\n%.0s' $(seq 1 50) > "$WORK/answers"

inner="cd '$WORK/example' && HOME='$WORK/home' OCM_CLI='$WORK/ocm' IMAGE_REGISTRY='ctf::$WORK/target' task prepare; echo TASK_EXIT=\$?"
# Run under a pseudo-terminal so the recipe's stdin is a TTY, as for a user typing `task prepare`.
if script --version 2>/dev/null | grep -q util-linux; then
  script -q -c "$inner" "$WORK/tty.log" < "$WORK/answers" > /dev/null 2>&1 || true      # util-linux
else
  script -q "$WORK/tty.log" sh -c "$inner" < "$WORK/answers" > /dev/null 2>&1 || true   # BSD/macOS
fi
[[ -f "$WORK/tty.log" ]] || setup_fail "script(1) produced no log"
log="$(tr -d '\r' < "$WORK/tty.log")"
grep -q "TASK_EXIT=" <<<"$log" || { echo "$log"; setup_fail "task did not run"; }
[[ -f "$WORK/target/artifact-index.json" ]] || { echo "$log"; setup_fail "transfer did not produce the target CTF"; }

prompts="$( { grep -oE "override r--r--r--[^?]*\?|remove write-protected[^?]*\?" <<<"$log" || true; } | wc -l | tr -d ' ')"
leftover="$(grep -oE "for [^ ]+/ctf/" <<<"$log" | head -1 | sed -E 's#^for (.*)/ctf/$#\1#' || true)"
echo "$(grep -E "TASK_EXIT=" <<<"$log")  prompts=$prompts"
if [[ "$prompts" -gt 0 ]]; then
  grep -oE "override r--r--r--[^?]*\?|remove write-protected[^?]*\?" <<<"$log" | head -3
  if [[ -n "$leftover" && -d "$leftover" ]]; then
    echo "leftover temp CTF after declining: $leftover ($(find "$leftover" -type f | wc -l | tr -d ' ') files)"
    chmod -R u+w "$leftover" && rm -r "$leftover"
  fi
  echo "REPRODUCED: cleanup prompted $prompts time(s) on a TTY"; exit 1
fi
echo "OK: no cleanup prompts on a TTY"
