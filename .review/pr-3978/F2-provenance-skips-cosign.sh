#!/usr/bin/env bash
# PR 3978 / F2: the "Image provenance (FIPS module, CA bundle)" step checks the
# Go FIPS build settings of the entrypoint only, not of /usr/local/bin/cosign,
# although tailoring.xml deselects the FIPS-module SRG rules (SV-203649,
# SV-203739, SV-203750, SV-203751, SV-203776) on the grounds that this step
# covers them, and cosign is the binary that does the Sigstore cryptography.
#
# Expected: the step fails when an executable in the image (here cosign) is not
#           built with GOFIPS140 / DefaultGODEBUG=fips140=on.
# Actual on 9bcb427b0: the step passes.
#
# Contract: exit 0 = behaves correctly, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout bash .review/pr-3978/F2-provenance-skips-cosign.sh
# Runs the step's own script from image-scan.yml in ubuntu:24.04 (the runner OS,
# for GNU grep -P); docker inside the step is replaced by a shim. Needs Docker, yq, go.
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

WF="$REPO_ROOT/.github/workflows/image-scan.yml"
yq -e '.jobs[].steps[] | select(.name == "Image provenance (FIPS module, CA bundle)") | .run' "$WF" > "$WORK/step.sh" 2>/dev/null ||
  setup_fail "no provenance step in $WF"

ARCH="$(docker version -f '{{.Server.Arch}}')" || setup_fail "docker"

# --- fixture: an image rootfs with a FIPS entrypoint and a non-FIPS cosign -------
mkdir -p "$WORK/src" "$WORK/tmp/rootfs/usr/local/bin" "$WORK/tmp/rootfs/etc/ssl/certs" "$WORK/shim"
printf 'module fixture\n\ngo 1.24\n' > "$WORK/src/go.mod"
printf 'package main\n\nfunc main() {}\n' > "$WORK/src/main.go"
(cd "$WORK/src" &&
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" GOFIPS140=certified go build -o "$WORK/tmp/rootfs/ocm" . &&
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -o "$WORK/tmp/rootfs/usr/local/bin/cosign" .) ||
  setup_fail "fixture build"
echo "fixture CA bundle" > "$WORK/tmp/rootfs/etc/ssl/certs/ca-certificates.crt"
cp "$WORK/tmp/rootfs/etc/ssl/certs/ca-certificates.crt" "$WORK/base-ca.crt"
cp "$REPO_ROOT/bindings/go/cli/Containerfile" "$WORK/Containerfile"
# The step pulls the certs stage with `docker run ... cat`; serve the fixture bundle.
cat > "$WORK/shim/docker" <<'SH'
#!/bin/sh
case "$1" in
  version) uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/' ;;
  run) cat /w/base-ca.crt ;;
  *) exit 99 ;;
esac
SH
chmod +x "$WORK/shim/docker"

# --- exercise: the PR's step, as GitHub runs it (bash -e -o pipefail) ------------
set +e
out="$(docker run --rm -v "$WORK:/w" -w /w -e ENTRYPOINT=/ocm -e CONTAINERFILE=Containerfile \
  -e PATH=/w/shim:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  ubuntu:24.04 bash --noprofile --norc -e -o pipefail /w/step.sh 2>&1)"
rc=$?
set -e
echo "$out"
docker run --rm -v "$WORK:/w" ubuntu:24.04 sh -c 'grep -caP "^build\tGOFIPS140=" /w/tmp/rootfs/usr/local/bin/cosign' |
  sed 's/^/GOFIPS140 records in fixture cosign: /' || true

if [[ $rc -eq 0 ]]; then
  echo "REPRODUCED: provenance step passed although /usr/local/bin/cosign is not a FIPS build"; exit 1
fi
grep -q 'cosign' <<<"$out" || { echo "INCONCLUSIVE: step failed, but not on cosign (exit $rc)"; exit 2; }
echo "OK: step rejected the non-FIPS cosign"
