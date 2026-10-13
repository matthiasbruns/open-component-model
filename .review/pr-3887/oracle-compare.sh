#!/usr/bin/env bash
# Compares jsonNormalisation/v1 output of this PR against two independent oracles:
#   cc-utils ocm/sign.py (PyPI gardener-ocm) and OCM v1 compdesc.Normalize (ocm.software/ocm).
# usage: .review/pr-3887/oracle-compare.sh            (run from the repository root)
# Needs python3, go and network access to PyPI and the Go module proxy.
# exit 0 = PR matches both oracles for every case, 1 = at least one divergence, 2 = setup failure
set -uo pipefail
D=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$D/../.." && pwd)
W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
python3 -m venv "$W/venv" && "$W/venv/bin/pip" install -q 'gardener-ocm==1.2847.0' || exit 2
(cd "$D/ocmv1-oracle" && GOWORK=off go build -o "$W/ocmv1-oracle" .) || exit 2
mkdir -p "$W/cases" && python3 -I "$D/mkcases.py" "$W/cases" >/dev/null || exit 2
(cd "$ROOT/bindings/go" && GOWORK=off go run "$D/pr_main.go" "$W"/cases/*.json) || exit 2
rc=0
for f in "$W"/cases/*.json; do
  n=$(basename "$f" .json)
  "$W/venv/bin/python" -I "$D/ccutils_oracle.py" "$f" > "$f.cc" 2>/dev/null || echo ERROR > "$f.cc"
  "$W/ocmv1-oracle" "$f" > "$f.v1" 2>/dev/null || echo ERROR > "$f.v1"
  pr=$(cat "$f.pr"); cc=$(cat "$f.cc"); v1=$(cat "$f.v1")
  a=$([ "$pr" = "$cc" ] && echo yes || echo NO); b=$([ "$pr" = "$v1" ] && echo yes || echo NO); c=$([ "$cc" = "$v1" ] && echo yes || echo NO)
  printf '%-30s pr==cc-utils:%-4s pr==ocm-v1:%-4s cc-utils==ocm-v1:%s\n' "$n" "$a" "$b" "$c"
  [ "$a$b" = "yesyes" ] || rc=1
  if [ "$a$b" != "yesyes" ]; then printf '  pr      : %s\n  cc-utils: %s\n  ocm-v1  : %s\n' "$pr" "$cc" "$v1" >> "$W/diff.txt"; fi
done
[ -f "$W/diff.txt" ] && { echo; echo "Divergent outputs:"; cat "$W/diff.txt"; }
exit $rc
