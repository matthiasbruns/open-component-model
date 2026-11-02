#!/usr/bin/env bash
# The default chart grants the controller "impersonate" on every serviceaccount
# cluster-wide (namespaceddeployer-impersonation-role). Checks whether either
# benchmark reports that grant as an open finding, using the pinned scanners.
# Exit 0: reported; 1: reproduced (not reported); 2: setup failure.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
render "$tmp/scan" || exit 2
role="$tmp/scan/chart/templates/rbac/namespaceddeployer-impersonation-role.yaml"
grep -q impersonate "$role" || exit 2
TRIVY_CHECKS_BUNDLE_REPOSITORY="$(grep -E '^TRIVY_CHECKS_IMAGE=' "$root/.env" | cut -d= -f2)" \
  trivy config --quiet --skip-version-check --include-non-failures --format json --output "$tmp/trivy.json" "$role" || exit 2
echo "Trivy on the impersonation role:"
jq -r '.Results[].Misconfigurations[] | select(.ID == "KSV-0043" or .Status == "FAIL") | "  \(.ID) \(.Status) \(.Title)"' "$tmp/trivy.json"
trivy_fail=$(jq '[.Results[].Misconfigurations[] | select(.Status == "FAIL")] | length' "$tmp/trivy.json")
cp "$root/.github/benchmarks/kubescape-exceptions.json" "$tmp/"
eval "$(grep -E '^(KUBESCAPE_IMAGE|KUBESCAPE_CONTROLS_VERSION)=' "$root/.env")"
docker run --rm -e HOME=/tmp -e KS_SKIP_UPDATE_CHECK=true -v "$tmp:/work" -w /work "$KUBESCAPE_IMAGE" \
  scan framework nsa,mitre scan --controls-version "$KUBESCAPE_CONTROLS_VERSION" \
  --exceptions kubescape-exceptions.json --format json --output /work/ks.json >/dev/null 2>&1 || exit 2
echo "Kubescape: C-0065 (No impersonation) evaluated: $(jq '.summaryDetails.controls | has("C-0065")' "$tmp/ks.json")"
echo "Kubescape failures on the impersonation binding:"
jq -r '.results[]? | select(.resourceID | test("impersonation")) | .controls[]? | select(.status.status == "failed") | "  \(.controlID) \(.status.subStatus // "OPEN")"' "$tmp/ks.json"
ks_open=$(jq '[.results[]? | select(.resourceID | test("impersonation")) | .controls[]? | select(.status.status == "failed" and .status.subStatus != "w/exceptions")] | length' "$tmp/ks.json")
if [ "$trivy_fail" -eq 0 ] && [ "$ks_open" -eq 0 ]; then
  echo "FAIL: cluster-wide serviceaccount impersonation is not an open finding in either benchmark"
  exit 1
fi
exit 0
