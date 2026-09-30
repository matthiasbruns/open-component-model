#!/usr/bin/env bash
# PR 3702 / D5: migrated docs put shell commands (`cat > ocmconfig.yaml << 'EOF' ... EOF`) inside
# ```yaml fences, so they are highlighted/linted as YAML and the "copy" snippet is not valid YAML.
#
# Expected: heredoc commands that the PR adds live in ```bash fences.
# Actual on dc65b8520: several PR-added blocks use ```yaml.
# Contract: exit 0 = correct, exit 1 = reproduced, other = inconclusive.
set -euo pipefail
REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
cd "$REPO_ROOT/website/content/docs" 2>/dev/null || { echo "SETUP FAILED: no docs"; exit 2; }
# Only the uploader heredocs introduced by the PR (they write uploader entries).
hits="$(grep -rl 'uploader.transfer.config.ocm.software' . | sort | while read -r f; do
  awk -v F="$f" '/^```yaml/{y=1; h=0; s=NR; next} /^```/{y=0; h=0} y && /^cat > .*<< ?.EOF/{h=1} y && h && /uploader\.transfer\.config/{print F":"s; h=0}' "$f"
done | sort -u)"
if [[ -n "$hits" ]]; then
  echo "REPRODUCED: shell heredoc inside \`\`\`yaml fence:"; echo "$hits"; exit 1
fi
echo "OK"
