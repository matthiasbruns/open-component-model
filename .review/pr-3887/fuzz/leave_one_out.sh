#!/usr/bin/env bash
# Differential fuzz of jsonNormalisation/v1 against OCM v1 (ocm.software/ocm v0.52.0).
# usage: .review/pr-3887/fuzz/leave_one_out.sh [count] [seed]     (default 3000 3887)
# Prints agree/diverge counts vs OCM v1 for: PR head (.pr), PR + all candidate patches (.all),
# and PR + all patches except one (.wo-<patch>). Work happens in a temp dir.
set -euo pipefail
F=$(cd "$(dirname "$0")" && pwd); P=$F/patches; W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
ALL=(p1-html p2-none-access p3-ref-empty-digest p4-provider-labels p5-ocmv1-dup)
python3 -I "$F/gen.py" "$W/cases" "${1:-3000}" "${2:-3887}" >/dev/null
(cd "$F/ocmv1multi" && GOWORK=off GOFLAGS=-mod=mod go build -o "$W/oracle-v1" .)
"$F/build_variant.sh" "$W/v-pr"
args=(); for y in "${ALL[@]}"; do args+=("$P/$y.diff"); done
"$F/build_variant.sh" "$W/v-all" "${args[@]}"
for x in "${ALL[@]}"; do
  args=(); for y in "${ALL[@]}"; do [ "$y" != "$x" ] && args+=("$P/$y.diff"); done
  "$F/build_variant.sh" "$W/v-without-$x" "${args[@]}"
done
cd "$W/cases"
ls c*.json | xargs -n 500 "$W/oracle-v1"
ls c*.json | SUFFIX=.pr xargs -n 500 "$W/v-pr"
ls c*.json | SUFFIX=.all xargs -n 500 "$W/v-all"
for x in "${ALL[@]}"; do ls c*.json | SUFFIX=.wo-$x xargs -n 500 "$W/v-without-$x"; done
cd "$F"
for s in .pr .all; do echo "== $s"; python3 -I cmp.py "$W/cases" "$s"; done
for x in "${ALL[@]}"; do echo "== .wo-$x"; python3 -I cmp.py "$W/cases" ".wo-$x"; done
