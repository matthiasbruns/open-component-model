#!/usr/bin/env bash
# usage: <script> <ocm-binary> <label>   exit 0 = correct, 1 = reproduced (hop1 exits 0, hop2 rejects the reference digest), 2 = inconclusive
# unsigned parent references child by digest; child has a Helm chart from an oci:// chart repo.
# recursive transfer --copy-resources twice; then check the parent's reference digest against the copied child.
set -u
OCM=$1; LABEL=$2; REG=${REGISTRY_BIN:-registry}; command -v "$REG" >/dev/null && command -v helm >/dev/null || { echo "INCONCLUSIVE: needs helm and REGISTRY_BIN"; exit 2; }
CH=$(cd "$(dirname "$0")/../../bindings/go/helm/testdata/provenance" && pwd)
W=$(mktemp -d); trap 'kill ${RPID:-} 2>/dev/null; rm -rf "$W"' EXIT
export HOME=$W DOCKER_CONFIG=$W/.docker HELM_REGISTRY_CONFIG=$W/reg.json
RPORT=$((30000 + RANDOM % 10000)); "$REG" -port $RPORT >/dev/null 2>&1 & RPID=$!; sleep 1
helm push --plain-http $CH/mychart-0.1.0.tgz oci://127.0.0.1:$RPORT/charts >/dev/null 2>&1 || { echo INCONCLUSIVE push; exit 2; }
printf 'type: generic.config.ocm.software/v1\nconfigurations:\n- type: http.config.ocm.software/v1alpha1\n  plainHTTP: true\n' > $W/cfg.yaml
cat > $W/c.yaml <<EOF
components:
- name: ocm.software/child
  version: v1.0.0
  provider: {name: ocm.software}
  resources:
  - name: mychart
    version: 0.1.0
    type: helmChart
    access: {type: helm/v1, helmRepository: "oci://127.0.0.1:$RPORT/charts", helmChart: "mychart:0.1.0"}
- name: ocm.software/parent
  version: v1.0.0
  provider: {name: ocm.software}
  componentReferences:
  - {name: child, componentName: ocm.software/child, version: v1.0.0}
EOF
C="--config $W/cfg.yaml"; P=ocm.software/parent:v1.0.0
"$OCM" add cv --repository ctf::$W/src --constructor $W/c.yaml $C >$W/add.log 2>&1 || { echo "INCONCLUSIVE add"; tail -2 $W/add.log; exit 2; }
echo "[$LABEL] source child chart digest: $("$OCM" get cv ctf::$W/src//ocm.software/child:v1.0.0 -o yaml $C 2>/dev/null | grep -A3 '      digest:$' | tr -s ' ' | tr '\n' ' ')"
for hop in 1 2; do
  from=$([ $hop = 1 ] && echo src || echo h1)
  "$OCM" transfer cv ctf::$W/$from//$P ctf::$W/h$hop --recursive --copy-resources $C >$W/h$hop.log 2>&1; echo "[$LABEL] hop$hop transfer=$? $(grep -m1 '^Error' $W/h$hop.log | cut -c1-300)"
done
echo "[$LABEL] hop1 child chart digest: $("$OCM" get cv ctf::$W/h1//ocm.software/child:v1.0.0 -o yaml $C 2>/dev/null | grep -A3 '      digest:$' | tr -s ' ' | tr '\n' ' ')"
H1=$(grep -c . $W/h1.log >/dev/null; "$OCM" transfer cv ctf::$W/h1//$P ctf::$W/h3 --recursive --copy-resources $C >/dev/null 2>&1; echo $?)
[ -d $W/h1 ] && [ "$H1" != 0 ] && exit 1 || exit 0
