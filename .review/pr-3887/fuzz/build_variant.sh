#!/usr/bin/env bash
# usage: build_variant.sh <out-binary> [patch...]
# Builds pr_main.go against a temporary copy of this checkout's committed bindings/go
# (git archive HEAD) with the given patches applied. The checkout itself is never modified.
set -euo pipefail
F=$(cd "$(dirname "$0")" && pwd); ROOT=$(git -C "$F" rev-parse --show-toplevel)
out=$1; shift
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
git -C "$ROOT" archive HEAD bindings/go | tar -x -C "$T"
for p in "$@"; do git -C "$T" apply "$p" 2>/dev/null || (cd "$T" && patch -s -p1 < "$p"); done
(cd "$T/bindings/go" && GOWORK=off go build -o "$out" "$F/pr_main.go")
