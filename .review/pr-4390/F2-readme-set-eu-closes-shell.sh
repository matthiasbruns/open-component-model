#!/usr/bin/env bash
# PR 4390 / F2: the kustomize-configuration-localization README's "execute directly" block
# starts with `set -eu` and uses `exit 1`, so pasting it into an interactive shell closes the
# shell when oras is missing, and leaves `set -eu` active after a successful run.
#
# Expected: the copy-paste block can fail without terminating the reader's terminal.
# Actual on dd7ebff866: in `bash -i` with oras not on PATH, the shell exits at the guard and
# no later command runs.
#
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout bash .review/pr-4390/F2-readme-set-eu-closes-shell.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

README="$REPO_ROOT/bindings/go/kubernetes/controller/examples/kustomize-configuration-localization/README.md"
# The first ```bash fence after "To execute the same sequence directly".
awk '/To execute the same sequence/{f=1} f&&/^```bash/{c=1;next} c&&/^```/{exit} c' "$README" > "$WORK/block.sh"
if [[ ! -s "$WORK/block.sh" ]]; then
  echo "OK: no direct-execution block on this revision"; exit 0
fi

# PATH with the tools the block needs except oras.
mkdir -p "$WORK/bin"
for t in bash cp tar mktemp rm env cat; do
  p="$(command -v "$t")" || setup_fail "$t missing"; ln -s "$p" "$WORK/bin/$t"
done
{ cat "$WORK/block.sh"; echo 'echo SHELL_STILL_ALIVE'; } > "$WORK/paste.txt"

cd "$(dirname "$README")"
out="$(env -i HOME="$WORK" PATH="$WORK/bin" TERM=dumb OCM_CLI=/nonexistent IMAGE_REGISTRY=ctf::"$WORK/t" \
  "$WORK/bin/bash" --norc --noprofile -i < "$WORK/paste.txt" 2>&1 || true)"
grep -q "oras is required\|oras" <<<"$out" || { echo "$out"; setup_fail "block did not reach the oras guard"; }

if grep -q "^SHELL_STILL_ALIVE" <<<"$out"; then
  echo "OK: interactive shell survives the failing block"; exit 0
fi
echo "$out" | grep -E "oras is required" || true
echo "REPRODUCED: the pasted README block terminated the interactive shell"; exit 1
