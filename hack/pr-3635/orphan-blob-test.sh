#!/usr/bin/env bash
# Demonstrates that `ocm add cv` uploads resource blobs before it validates
# explicit resource versions, leaving orphaned blobs in the target repository.
#
# Usage: ./test.sh [<repo-url>#<branch> ...]
# Run from inside any open-component-model clone. Each ref is fetched by URL,
# so no particular remotes are needed. Defaults: the PR head and the fix branch.
set -euo pipefail

REFS=("$@")
[ ${#REFS[@]} -eq 0 ] && REFS=(
  "https://github.com/jakobmoellerdev/open-component-model.git#feat/pluggable-versioning-schemes"
  "https://github.com/matthiasbruns/open-component-model.git#fix/pr-3635-validate-versions-before-upload"
)

REPO=$(git rev-parse --show-toplevel)
WORK=$(mktemp -d)
trap 'for d in "$WORK"/src-*; do git -C "$REPO" worktree remove --force "$d" 2>/dev/null || true; done; rm -rf "$WORK"' EXIT

mkdir -p "$WORK/home"
echo hello > "$WORK/data.txt"

constructor() { # $1 = resource version line (may be empty)
  cat > "$WORK/cc.yaml" <<EOF
components:
- name: ocm.software/demo
  version: 1.0.0
  provider: {name: ocm.software}
  resources:
  - name: data
    type: blob
$1
    input: {type: file, path: $WORK/data.txt}
EOF
}

scenario() { # $1 = ocm binary, $2 = label, $3 = resource version line
  constructor "$3"
  rm -rf "$WORK/ctf"
  local rc=0 out
  out=$(HOME="$WORK/home" "$1" add cv --repository "$WORK/ctf" --constructor "$WORK/cc.yaml" 2>&1) || rc=$?
  local blobs; blobs=$( (find "$WORK/ctf/blobs" -type f 2>/dev/null || true) | wc -l | tr -d ' ')
  printf '  %-34s exit=%s blobs-in-ctf=%s\n' "$2" "$rc" "$blobs"
  [ "$rc" -ne 0 ] && printf '    %s\n' "$(grep -o 'resource "data" has an invalid version[^:]*' <<<"$out" | head -1)"
  return 0
}

for ref in "${REFS[@]}"; do
  git -C "$REPO" fetch -q "${ref%%#*}" "${ref#*#}"
  sha=$(git -C "$REPO" rev-parse --short FETCH_HEAD)
  src="$WORK/src-$sha"
  git -C "$REPO" worktree add -q --detach "$src" "$sha"
  (cd "$src/bindings/go" && go build -o "$WORK/ocm-$sha" ./cli)

  echo "== ${ref#*#} ($sha)"
  scenario "$WORK/ocm-$sha" "valid, defaulted resource version" ""
  scenario "$WORK/ocm-$sha" "valid, explicit resource version" "    version: 1.2.3"
  scenario "$WORK/ocm-$sha" "INVALID resource version 'foo'" "    version: foo"
done

cat <<'EOF'

Expected: the invalid case fails with exit=1 on both refs. On the PR head it
leaves blobs-in-ctf=1 (the resource was uploaded before validation). With the
fix it leaves blobs-in-ctf=0.
EOF
