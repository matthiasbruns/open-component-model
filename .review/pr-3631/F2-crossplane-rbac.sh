#!/usr/bin/env bash
# F2: the Crossplane examples only work with RBAC that lives in test/e2e/hacks/setup.sh.
#
# Runs the helm-simple Crossplane example on the ocm-e2e kind cluster (task test/e2e/setup/local)
# under one of three RBAC setups for the crossplane service account:
#   e2e         as setup.sh leaves it (ClusterRoleBinding crossplane-ocm-resources-e2e present)
#   stock       that binding removed: what a user following examples/README.md gets on a stock Crossplane install
#   aggregated  binding removed, plus a ClusterRole labelled rbac.crossplane.io/aggregate-to-crossplane=true
#               (https://docs.crossplane.io/latest/composition/compositions/)
#
# Contract: exit 0 = XR Ready, 1 = XR not Ready (reproduced), 2 = inconclusive.
# The removed binding is restored on exit.
set -uo pipefail

MODE="${1:?usage: F2-crossplane-rbac.sh <e2e|stock|aggregated> [repo-root]}"
ROOT="${2:-$(git rev-parse --show-toplevel)}"
EX="$ROOT/bindings/go/kubernetes/controller/examples/helm-simple"
OCM="${OCM_CLI:-ocm}"
REGISTRY="${IMAGE_REGISTRY:-http://ocm-e2e-image-registry:5000}"
TIMEOUT="${XR_TIMEOUT:-180s}"
CTX=kind-ocm-e2e
BINDING=crossplane-ocm-resources-e2e
AGG=crossplane-ocm-resources-aggregate-review
XR=helmsimplecrossplanes.examples.ocm.software/helm-simple-crossplane
k() { kubectl --context "$CTX" "$@"; }

k get ns crossplane-system >/dev/null 2>&1 || { echo "no ocm-e2e cluster with crossplane"; exit 2; }
TMP="$(mktemp -d)"
k get clusterrolebinding "$BINDING" -o json \
  | jq 'del(.metadata.resourceVersion,.metadata.uid,.metadata.creationTimestamp,.metadata.managedFields,.metadata.annotations)' \
  > "$TMP/binding.json" || { echo "binding $BINDING missing; run setup.sh first"; exit 2; }

cleanup() {
  echo "--- cleanup"
  k delete -f "$EX/crossplane-instance.yaml" --ignore-not-found --wait=true --timeout=180s >/dev/null 2>&1
  k delete -f "$EX/bootstrap.yaml" --ignore-not-found --wait=true --timeout=300s >/dev/null 2>&1
  k delete clusterrole "$AGG" --ignore-not-found >/dev/null 2>&1
  k apply -f "$TMP/binding.json" >/dev/null && echo "restored $BINDING"
  rm -rf "$TMP"
}
trap cleanup EXIT

echo "--- transfer helm-simple component to $REGISTRY"
(cd "$EX" && "$OCM" add cv --repository "$TMP/ctf" --constructor component-constructor.yaml >/dev/null) || exit 2
"$OCM" transfer cv "ctf::$TMP/ctf//ocm.software/ocm-k8s-toolkit/examples/helm-simple" "$REGISTRY" >/dev/null 2>&1 \
  || "$OCM" get cv "$REGISTRY//ocm.software/ocm-k8s-toolkit/examples/helm-simple:1.0.0" >/dev/null || exit 2

case "$MODE" in
  e2e) ;;
  stock) k delete clusterrolebinding "$BINDING" ;;
  aggregated)
    k delete clusterrolebinding "$BINDING"
    k apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: $AGG
  labels:
    rbac.crossplane.io/aggregate-to-crossplane: "true"
rules:
  - apiGroups: ["delivery.ocm.software"]
    resources: ["resources"]
    verbs: ["create","delete","get","list","patch","update","watch"]
  - apiGroups: ["source.toolkit.fluxcd.io"]
    resources: ["ocirepositories"]
    verbs: ["create","delete","get","list","patch","update","watch"]
  - apiGroups: ["helm.toolkit.fluxcd.io"]
    resources: ["helmreleases"]
    verbs: ["create","delete","get","list","patch","update","watch"]
  - apiGroups: ["argoproj.io"]
    resources: ["applications"]
    verbs: ["create","delete","get","list","patch","update","watch"]
EOF
    ;;
  *) echo "unknown mode $MODE"; exit 2 ;;
esac

echo "--- mode=$MODE: apply bootstrap, wait for XRD, apply XR"
k apply -f "$EX/bootstrap.yaml" >/dev/null || exit 2
k wait --for=create --timeout=180s compositeresourcedefinitions.apiextensions.crossplane.io/helmsimplecrossplanes.examples.ocm.software >/dev/null || exit 2
k wait --for=condition=Established=true --timeout=180s compositeresourcedefinitions.apiextensions.crossplane.io/helmsimplecrossplanes.examples.ocm.software >/dev/null || exit 2
k apply -f "$EX/crossplane-instance.yaml" >/dev/null || exit 2

if k wait --for=condition=Ready=true --timeout="$TIMEOUT" "$XR"; then
  k get "$XR" -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}{"\n"}{end}'
  echo "RESULT mode=$MODE: XR Ready"
  exit 0
fi
echo "--- XR conditions"
k get "$XR" -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}: {.message}{"\n"}{end}'
echo "--- XR events"
k get events -A --field-selector involvedObject.name=helm-simple-crossplane -o custom-columns=REASON:.reason,MSG:.message --no-headers | sort -u | head -10
echo "RESULT mode=$MODE: XR not Ready within $TIMEOUT"
exit 1
