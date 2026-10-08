#!/usr/bin/env bash
# PR #3810 F1: the second `--config -` is dropped silently.
#
# --config entries merge in the order given and later entries win. A repeated file
# entry is loaded again at its later position, so it wins:
#   --config a.yaml --config b.yaml --config a.yaml   ->  a.yaml wins
# A repeated "-" is skipped, so stdin keeps its first position:
#   --config - --config b.yaml --config -             ->  b.yaml wins, nothing is reported
#
# Self-contained: builds the CLI from the checkout this script lives in and uses
# only a temp dir. No network, no other binaries.
#
# Run from the repository root:  bash .review/pr-3810/F1-repeated-stdin-config.sh
# Exit: 0 = correct (the repeated "-" is rejected, reported, or wins by position),
#       1 = reproduced (the second --config - is dropped silently),
#       2 = inconclusive.
set -uo pipefail

ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel) || exit 2
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

echo "building ocm from $ROOT/bindings/go/cli ..."
(cd "$ROOT/bindings/go/cli" && go build -o "$T/ocm" .) || { echo "inconclusive: build failed"; exit 2; }

# ocm runs with an empty HOME and no auto configuration, so no local OCM or docker
# configuration takes part.
ocm() { HOME=$T OCM_DISABLE_AUTO_CONFIG=1 OCM_CONFIG= "$T/ocm" "$@"; }

# cfg <username>: one credential for the same consumer, so the merge order decides
# which username applies.
cfg() {
	cat <<EOF
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity: {type: OCIRegistry, hostname: example.com}
    credentials:
    - type: Credentials/v1
      properties: {username: $1, password: x}
EOF
}
cfg from-a >"$T/a.yaml"
cfg from-b >"$T/b.yaml"
cfg from-stdin >"$T/stdin.yaml"

# show <label> <args...>: run `get config`, print the merge order (lowest priority
# first, the last line wins) and the sources the debug log reports as loaded.
show() {
	local label=$1; shift
	echo
	echo "== $label"
	echo "   ocm get config ${*//$T\//} < stdin.yaml"
	ocm get config --loglevel debug --logformat text "$@" <"$T/stdin.yaml" >"$T/out" 2>"$T/err"
	local rc=$?
	echo "   exit code: $rc"
	echo "   loaded sources (debug log):"
	grep 'ocm config was loaded successfully' "$T/err" | grep -oE 'path=[^ ]+' | sed "s|$T/||; s|^|     |"
	echo "   merged credentials, lowest priority first (last one wins):"
	grep -oE 'from-[a-z]+' "$T/out" | sed 's/^/     /'
	WINNER=$(grep -oE 'from-[a-z]+' "$T/out" | tail -1)
	ERR=$(cat "$T/err")
	return $rc
}

show "control: a repeated file entry is loaded again and wins" \
	--config "$T/a.yaml" --config "$T/b.yaml" --config "$T/a.yaml"
[[ $WINNER == from-a ]] || { echo "inconclusive: control expected from-a, got '$WINNER'"; exit 2; }

show "control: stdin given last wins" \
	--config "$T/b.yaml" --config -
[[ $WINNER == from-stdin ]] || { echo "inconclusive: control expected from-stdin, got '$WINNER'"; exit 2; }

show "finding: the second --config - is dropped" \
	--config - --config "$T/b.yaml" --config -
rc=$?

echo
if ((rc != 0)); then
	if grep -qiE 'repeat|more than once|only once|twice' <<<"$ERR"; then
		echo "OK: a repeated --config - is rejected"
		exit 0
	fi
	echo "inconclusive: command failed for another reason"; echo "$ERR"; exit 2
fi
if [[ $WINNER == from-stdin ]]; then
	echo "OK: the last --config - wins, like a repeated file entry"
	exit 0
fi
if grep -qiE 'repeat|ignor|more than once|only once|twice' <<<"$ERR"; then
	echo "OK: the dropped --config - is reported"
	exit 0
fi
echo "REPRODUCED: stdin was loaded once, at its first position. The second --config - was dropped,"
echo "so b.yaml overrides stdin although --config - comes last, and nothing reports it."
exit 1
