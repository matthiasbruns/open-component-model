#!/usr/bin/env bash
# End-to-end OCM v1 -> OCM v2 check for jsonNormalisation/v1:
# each scenario is built and signed by the OCM v1 CLI (ocm.software/ocm v0.52.0) with
# `sign cv -N jsonNormalisation/v1`, sanity-verified by OCM v1, then verified by the
# OCM v2 CLI built from this checkout (bindings/go/cli).
# usage: .review/pr-3887/e2e/e2e.sh            (run from the repository root)
# exit 0 = OCM v2 verifies every scenario, 1 = at least one OCM v2 verification failed, 2 = setup failure
set -uo pipefail
D=$(cd "$(dirname "$0")" && pwd); ROOT=$(cd "$D/../../.." && pwd)
W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
(cd "$D/../ocmv1-oracle" && GOWORK=off GOFLAGS=-mod=mod go build -o "$W/ocm1" ocm.software/ocm/cmds/ocm) || exit 2
(cd "$ROOT/bindings/go/cli" && GOWORK=off go build -o "$W/ocm2" .) || exit 2
(cd "$W" && ./ocm1 create rsakeypair key.priv >/dev/null) || exit 2
echo hello > "$W/data.txt"
cat > "$W/v2config.yaml" <<CFG
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity: {type: RSA/v1alpha1, algorithm: RSASSA-PKCS1-V1_5, signature: default}
    credentials:
    - type: Credentials/v1
      properties: {publicKeyPEMFile: $W/key.pub}
CFG
rc=0
for s in "$D"/scenarios/*.yaml; do
  n=$(basename "$s" .yaml); y="$W/$n.yaml"; sed "s#__DATA__#$W/data.txt#" "$s" > "$y"
  c=$(sed -n 's/^- name: //p' "$y"); ctf="$W/$n-ctf"
  "$W/ocm1" add cv --create --file "$ctf" "$y" > "$W/$n.log" 2>&1 &&
  "$W/ocm1" sign cv -N jsonNormalisation/v1 -K "$W/key.priv" -s default --repo "$ctf" "$c:1.0.0" >> "$W/$n.log" 2>&1 || { echo "$n: setup failed"; tail -3 "$W/$n.log"; exit 2; }
  "$W/ocm1" verify cv -k "$W/key.pub" -s default --repo "$ctf" "$c:1.0.0" >> "$W/$n.log" 2>&1 && v1=ok || v1=FAIL
  if "$W/ocm2" verify cv --config "$W/v2config.yaml" "ctf::$ctf//$c:1.0.0" > "$W/$n.v2.log" 2>&1; then v2=ok; msg=""; else v2=FAIL; rc=1; msg=$(grep -o 'SIGNATURE VERIFICATION FAILED.*' "$W/$n.v2.log" | head -1 | cut -c1-120); fi
  printf '%-28s ocm-v1-verify:%-4s ocm-v2-verify:%-4s %s\n' "$n" "$v1" "$v2" "$msg"
done
exit $rc
