#!/usr/bin/env bash
# Evidence for OCM PR #3631: compare `additionalStatusFields` of the OCM Resource(s)
# between each example's kro ResourceGraphDefinition and its new Crossplane Composition.
# Usage: example-parity-kro-vs-crossplane.sh <path to bindings/go/kubernetes/controller>
set -uo pipefail
cd "${1:-.}"
for d in helm-simple helm-nested helm-nested-signed helm-signing \
         helm-simple-nested-status helm-configuration-localization \
         kustomize-simple kustomize-configuration-localization; do
  kro=$(yq -o=json -I0 '[.spec.resources[].template | select(.kind=="Resource") | .spec.additionalStatusFields]' "examples/$d/rgd.yaml" | grep -v '^\[\]$')
  xp=$(yq -o=json -I0 'select(.kind=="Composition") | [.spec.pipeline[].input.resources[]?.base | select(.kind=="Resource") | .spec.additionalStatusFields]' "examples/$d/crossplane-composition.yaml" | grep -v '^\[\]$')
  if [ "$kro" = "$xp" ]; then
    echo "MATCH    $d"
  else
    echo "DIVERGES $d"
    echo "    kro: $kro"
    echo "    xp : $xp"
  fi
done
echo
echo "references to the nested additional-status object (status.additional.oci.*):"
echo "  rgd.yaml:"; grep -rn "status.additional.oci" examples/*/rgd.yaml | sed 's/^/    /'
echo "  crossplane-composition.yaml:"; grep -rn "status.additional.oci" examples/*/crossplane-composition.yaml | sed 's/^/    /' || echo "    (none)"

echo
echo "verificationPolicy on the OCM Resource(s) (main #4109 added it to rgd.yaml):"
for d in helm-simple helm-nested helm-nested-signed helm-signing \
         helm-simple-nested-status helm-configuration-localization \
         kustomize-simple kustomize-configuration-localization; do
  kro=$(yq -o=json -I0 '[.spec.resources[].template | select(.kind=="Resource") | .spec.verificationPolicy // "<default Always>"]' "examples/$d/rgd.yaml" | grep -v '^\[\]$')
  xp=$(yq -o=json -I0 'select(.kind=="Composition") | [.spec.pipeline[].input.resources[]?.base | select(.kind=="Resource") | .spec.verificationPolicy // "<default Always>"]' "examples/$d/crossplane-composition.yaml" | grep -v '^\[\]$')
  [ "$kro" = "$xp" ] && echo "  MATCH    $d  $kro" || echo "  DIVERGES $d  kro=$kro xp=$xp"
done
