#!/usr/bin/env bash
# usage: <script> <ocm-binary> <label>   exit 0 = correct, 1 = reproduced, 2 = inconclusive
# Signed Helm chart (HTTP repo, with .prov) -> `ocm transfer cv --copy-resources` into a plain-HTTP
# registry (default local blob uploader). Then resolves the reference the controller's toOCI()
# builds for a localBlob (<base>/component-descriptors/<component>@<localReference>) the way Flux
# OCIRepository does: as a manifest.
set -u
OCM=$1; LABEL=$2
CHARTS=$(cd "$(dirname "$0")/../../bindings/go/helm/testdata/provenance" && pwd)
REG=${REGISTRY_BIN:-registry}; CRANE=${CRANE_BIN:-crane}
command -v "$REG" >/dev/null && command -v "$CRANE" >/dev/null || { echo "INCONCLUSIVE: set REGISTRY_BIN/CRANE_BIN (go install github.com/google/go-containerregistry/cmd/{registry,crane}@v0.20.6)"; exit 2; }
WORK=$(mktemp -d)
cleanup() { kill "${HPID:-}" "${RPID:-}" 2>/dev/null; rm -rf "$WORK"; }
trap cleanup EXIT

mkdir -p "$WORK/charts"
cp "$CHARTS/mychart-0.1.0.tgz" "$CHARTS/mychart-0.1.0.tgz.prov" "$WORK/charts/"
HPORT=$((20000 + RANDOM % 10000)); RPORT=$((30000 + RANDOM % 10000))
DIG=$(shasum -a 256 "$WORK/charts/mychart-0.1.0.tgz" | cut -d' ' -f1)
cat > "$WORK/charts/index.yaml" <<EOF
apiVersion: v1
entries:
  mychart:
    - urls: [http://127.0.0.1:$HPORT/mychart-0.1.0.tgz]
      name: mychart
      version: 0.1.0
      digest: "sha256:$DIG"
      apiVersion: v2
EOF
python3 -m http.server "$HPORT" --bind 127.0.0.1 --directory "$WORK/charts" >/dev/null 2>&1 & HPID=$!
"$REG" -port "$RPORT" >"$WORK/registry.log" 2>&1 & RPID=$!
sleep 1

openssl genrsa -traditional -out "$WORK/key.pem" 2048 2>/dev/null
openssl rsa -in "$WORK/key.pem" -RSAPublicKey_out -out "$WORK/pub.pem" 2>/dev/null
indent() { sed 's/^/          /' "$1"; }
cat > "$WORK/ocmconfig.yaml" <<EOF
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: |
$(indent "$WORK/pub.pem")
        private_key_pem: |
$(indent "$WORK/key.pem")
- type: http.config.ocm.software/v1alpha1
  plainHTTP: true
EOF
cat > "$WORK/constructor.yaml" <<EOF
components:
- name: ocm.software/review-4393
  version: v1.0.0
  provider: {name: ocm.software}
  resources:
  - name: mychart
    version: 0.1.0
    type: helmChart
    access: {type: helm/v1, helmRepository: "http://127.0.0.1:$HPORT", helmChart: "mychart:0.1.0"}
EOF
C="--config $WORK/ocmconfig.yaml"
SRC="ctf::$WORK/src//ocm.software/review-4393:v1.0.0"
TGT="http://127.0.0.1:$RPORT/t1"
"$OCM" add cv --repository "ctf::$WORK/src" --constructor "$WORK/constructor.yaml" $C >/dev/null 2>&1 || { echo "INCONCLUSIVE add cv"; exit 2; }
"$OCM" sign cv "$SRC" $C >/dev/null 2>&1 || { echo "INCONCLUSIVE sign"; exit 2; }
if [ "${MUTATE:-}" = 1 ]; then
  # upstream chart repo republishes mychart 0.1.0 with other bytes after signing
  (cd "$WORK" && helm create mychart >/dev/null && sed -i '' 's/^description:.*/description: republished/' mychart/Chart.yaml && helm package mychart -d "$WORK/charts" >/dev/null)
  NEWDIG=$(shasum -a 256 "$WORK/charts/mychart-0.1.0.tgz" | cut -d' ' -f1); sed -i '' "s/$DIG/$NEWDIG/" "$WORK/charts/index.yaml"; rm -f "$WORK/charts/mychart-0.1.0.tgz.prov"
  echo "-- upstream chart replaced after signing: $DIG -> $NEWDIG"
fi
echo "== [$LABEL] transfer --copy-resources -> $TGT"
"$OCM" transfer cv "$SRC" "$TGT" --copy-resources ${EXTRA:-} $C >"$WORK/transfer.log" 2>&1; echo "transfer exit=$?"; grep -i 'error\|mismatch' "$WORK/transfer.log" | head -3
"$OCM" verify cv "$TGT//ocm.software/review-4393:v1.0.0" $C >"$WORK/verify.log" 2>&1; echo "verify exit=$? ($(grep -o 'digest mismatch\|SIGNATURE VERIFICATION [A-Z]*\|verified[^\"]*' "$WORK/verify.log" | head -1))"
"$OCM" get cv "$TGT//ocm.software/review-4393:v1.0.0" -o yaml $C > "$WORK/target.yaml" 2>/dev/null
echo "-- target resource:"; awk '/resources:/,0' "$WORK/target.yaml" | sed -n '1,30p'
LREF=$(grep -m1 'localReference:' "$WORK/target.yaml" | awk '{print $2}')
REF="127.0.0.1:$RPORT/t1/component-descriptors/ocm.software/review-4393@$LREF"
echo "-- toOCI() reference: $REF"
echo "-- crane manifest (what Flux OCIRepository pulls):"
"$CRANE" manifest --insecure "$REF" 2>&1 | head -c 900; echo
echo "-- prov layer anywhere in the target repo?"
for repo in $("$CRANE" catalog --insecure "127.0.0.1:$RPORT" 2>/dev/null); do
  for tag in $("$CRANE" ls --insecure "127.0.0.1:$RPORT/$repo" 2>/dev/null); do
    "$CRANE" manifest --insecure "127.0.0.1:$RPORT/$repo:$tag" 2>/dev/null | grep -q 'helm.chart.provenance' && echo "   prov layer in $repo:$tag"
  done
  "$CRANE" manifest --insecure "$REF" 2>/dev/null | grep -q 'helm.chart.provenance' && echo "   prov layer in toOCI ref"
done

"$CRANE" manifest --insecure "$REF" >/dev/null 2>&1 && exit 0 || exit 1
