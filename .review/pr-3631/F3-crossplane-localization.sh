#!/usr/bin/env bash
# F3: the e2e suite does not assert localization/configuration on the Crossplane-managed pods.
#
# Runs the helm-configuration-localization e2e spec on the ocm-e2e kind cluster
# (task test/e2e/setup/local) with optional patches from this directory:
#   F3-mutation-drop-crossplane-localization.patch      removes every image/ui.message patch from the
#                                                       Crossplane composition (FluxCD + ArgoCD)
#   F3-regression-assert-crossplane-localization.patch  adds the kro-equivalent localization/config
#                                                       assertions for <example>-crossplane-podinfo
#
#   F3-crossplane-localization.sh mutated               expect PASS on PR head  -> the gap (reproduced)
#   F3-crossplane-localization.sh mutated+regression    expect FAIL             -> the new assertions catch it
#   F3-crossplane-localization.sh regression            expect PASS             -> the new assertions hold on PR head
#
# Contract: exit 0 = suite verdict as expected for a correct test suite, 1 = reproduced, 2 = inconclusive.
#   mutated:            suite PASS => gap reproduced (exit 1); FAIL => gap closed (exit 0)
#   mutated+regression: suite FAIL => exit 0; PASS => exit 1
#   regression:         suite PASS => exit 0; FAIL => exit 1
set -uo pipefail

VARIANT="${1:?usage: F3-crossplane-localization.sh <mutated|mutated+regression|regression> [repo-root]}"
ROOT="${2:-$(git rev-parse --show-toplevel)}"
HERE="$(cd "$(dirname "$0")" && pwd)"
CTRL="$ROOT/bindings/go/kubernetes/controller"
MUT="$HERE/F3-mutation-drop-crossplane-localization.patch"
REG="$HERE/F3-regression-assert-crossplane-localization.patch"

patches=()
case "$VARIANT" in
  mutated) patches=("$MUT") ;;
  mutated+regression) patches=("$MUT" "$REG") ;;
  regression) patches=("$REG") ;;
  *) echo "unknown variant $VARIANT"; exit 2 ;;
esac

kubectl --context kind-ocm-e2e get ns crossplane-system >/dev/null 2>&1 || { echo "no ocm-e2e cluster"; exit 2; }
for p in "${patches[@]}"; do git -C "$ROOT" apply --check "$p" || { echo "patch does not apply: $p"; exit 2; }; done
revert() { for p in "${patches[@]}"; do git -C "$ROOT" apply -R "$p"; done; }
for p in "${patches[@]}"; do git -C "$ROOT" apply "$p"; done
trap revert EXIT

# The controller caches resolved component versions by name:version. Every variant re-transfers 1.0.0
# with a different crossplane-xrd blob, so restart it to drop the cached descriptor from the previous run.
kubectl --context kind-ocm-e2e -n ocm-k8s-toolkit-system rollout restart deployment/ocm-k8s-toolkit-controller-manager >/dev/null || exit 2
kubectl --context kind-ocm-e2e -n ocm-k8s-toolkit-system rollout status deployment/ocm-k8s-toolkit-controller-manager --timeout=180s >/dev/null || exit 2

cd "$CTRL" || exit 2
task test/e2e -- --procs=1 --focus='example helm-configuration-localization$'
rc=$?
echo "suite exit=$rc variant=$VARIANT"
case "$VARIANT" in
  mutated)            [[ $rc -eq 0 ]] && { echo "REPRODUCED: broken Crossplane localization passes e2e"; exit 1; }; exit 0 ;;
  mutated+regression) [[ $rc -ne 0 ]] && { echo "OK: new assertions catch the broken localization"; exit 0; }; exit 1 ;;
  regression)         [[ $rc -eq 0 ]] && { echo "OK: new assertions pass on PR head"; exit 0; }; exit 1 ;;
esac
