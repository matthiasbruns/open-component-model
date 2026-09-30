#!/usr/bin/env bash
# PR 3702 / D3: migrated docs replace --copy-resources/--upload-as with `--config ocmconfig.yaml`
# (uploader entries only). --config REPLACES the default config lookup ($HOME/.ocmconfig, docker
# autoconfig), so credentials/resolvers the same pages rely on are silently dropped.
#
# Expected: every doc page that passes an uploader-only ocmconfig.yaml via --config either puts
#           credentials into that file, or tells the reader to merge with the existing config
#           (e.g. `--config ~/.ocmconfig --config ocmconfig.yaml`, or add the entries to .ocmconfig).
# Actual on dc65b8520: several pages do neither (air-gap-transfer.md even says credentials must be
#           in `.ocmconfig` right above the command).
#
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

# Precondition: --config replaces the default lookup (no CLI build needed; unit-level check).
PRE_TEST="$REPO_ROOT/bindings/go/cli/cmd/configuration/review_pr3702_docs_test.go"
if [[ -f "$PRE_TEST" ]]; then
  (cd "$REPO_ROOT/bindings/go" && go test ./cli/cmd/configuration/ -count=1 \
     -run '^TestReviewPR3702_D3_Precondition_ConfigFlagReplacesDefaultLookup$' >/dev/null) \
    || { echo "precondition does not hold: --config merges with the default lookup -> docs are fine"; exit 0; }
else
  grep -q 'will be used instead of the lookup above' "$REPO_ROOT/bindings/go/cli/cmd/configuration/ocm_config.go" \
    || setup_fail "cannot establish --config semantics"
fi

DOCS="$REPO_ROOT/website/content/docs"
[[ -d "$DOCS" ]] || setup_fail "no website/content/docs"

offenders=()
while IFS= read -r f; do
  # File writes / passes a config that contains credentials -> fine.
  grep -q 'credentials.config.ocm.software' "$f" && continue
  # File tells the reader to combine with the existing config -> fine.
  grep -Eq -- '--config (~|\$HOME)/\.ocmconfig|\.ocmconfig --config|existing (OCM )?config(uration)?|add (these|the) entr(y|ies) to your' "$f" && continue
  offenders+=("${f#"$REPO_ROOT"/}")
done < <(grep -rlE -- '--config (\./)?ocmconfig\.yaml' "$DOCS" | sort)

# The migration guide's instruction for the logged config.
MIG="$DOCS/how-to/migrate-from-upload-as.md"
if [[ -f "$MIG" ]] && grep -q 'Copy that value into a file and pass it with `--config`, then drop' "$MIG"; then
  offenders+=("website/content/docs/how-to/migrate-from-upload-as.md (Deprecated flags: 'Copy that value into a file and pass it with --config')")
fi

if (( ${#offenders[@]} > 0 )); then
  echo "REPRODUCED: pages pass an uploader-only config via --config without keeping the default config (credentials/resolvers):"
  printf '  %s\n' "${offenders[@]}"
  grep -n 'credentials configured' "$DOCS/how-to/air-gap-transfer.md" 2>/dev/null | sed 's/^/  air-gap-transfer.md:/' || true
  exit 1
fi
echo "OK: docs keep the default configuration when adding uploader entries"
