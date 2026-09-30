#!/usr/bin/env bash
# PR 3691 / F2: the published ocm CLI image (bindings/go/cli/Containerfile, FROM scratch) can no
# longer sign or verify with GPGSigningConfiguration, because the handler now needs gpg on PATH.
#
# Expected: `ocm sign cv` + `ocm verify cv` with a GPG signer succeed inside the CLI image,
#           as they do on the merge base.
# Actual on 1ceaa2fa: sign fails with `GPG signing requires the GnuPG "gpg" binary (>= 2.2.0) on PATH`.
#
# Builds the image exactly like `task cli:package/image` (same Containerfile, binary at
# tmp/bin/ocm-<os>-<arch>) but in a temp build context, for the Docker daemon's own arch.
# Needs Docker and a host gpg (only to generate the test key); pulls the Containerfile base images.
#
# Contract (verify.sh): exit 0 = behaves correctly, exit 1 = finding reproduced,
# anything else = setup problem (inconclusive). Run from any checkout:
#   REPO_ROOT=/path/to/checkout bash .review/pr-3691/F2-container-image-lacks-gpg.sh
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
# Short base: gpg-agent sockets live in the key-generation home (Unix socket path limit).
WORK="$(mktemp -d /tmp/ocm-f2.XXXXXX)"
TAG="ocm-review-pr3691-f2:$(basename "$WORK" | tr 'A-Z.' 'a-z-')"
cleanup() {
  gpgconf --homedir "$WORK/gnupg" --kill all 2>/dev/null || true
  docker rmi -f "$TAG" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

command -v docker >/dev/null || setup_fail "docker not found"
command -v gpg >/dev/null || setup_fail "host gpg not found (needed to generate the test key)"
ARCH="$(docker version --format '{{.Server.Arch}}')" || setup_fail "docker daemon not reachable"

# --- image ---------------------------------------------------------------------
mkdir -p "$WORK/ctx/tmp/bin"
(cd "$REPO_ROOT/bindings/go/cli" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -o "$WORK/ctx/tmp/bin/ocm-linux-$ARCH" .) \
  || setup_fail "cli build"
# Resolve $BUILDPLATFORM as BuildKit would; the legacy builder leaves it empty in FROM lines.
sed "s|\$BUILDPLATFORM|linux/$ARCH|g" "$REPO_ROOT/bindings/go/cli/Containerfile" > "$WORK/ctx/Containerfile"
docker build -q --platform "linux/$ARCH" --build-arg TARGETOS=linux --build-arg TARGETARCH="$ARCH" -t "$TAG" -f "$WORK/ctx/Containerfile" "$WORK/ctx" >/dev/null \
  || setup_fail "docker build"

# --- fixture -------------------------------------------------------------------
mkdir -m 700 "$WORK/gnupg"; mkdir -p "$WORK/data"
gpg --batch --homedir "$WORK/gnupg" --pinentry-mode loopback --passphrase '' \
  --quick-gen-key 'OCM Review <review@example.com>' ed25519 sign never 2>/dev/null || setup_fail "gpg keygen"
gpg --batch --homedir "$WORK/gnupg" --armor --export-secret-keys > "$WORK/data/private.asc"
gpg --batch --homedir "$WORK/gnupg" --armor --export > "$WORK/data/public.asc"
printf 'hello' > "$WORK/data/hello.txt"
cat > "$WORK/data/constructor.yaml" <<'YAML'
components:
- name: ocm.software/review/pr3691-f2
  version: v1.0.0
  provider:
    name: ocm.software
  resources:
  - name: hello
    type: plainText
    input:
      type: file
      path: /data/hello.txt
YAML
cat > "$WORK/data/ocmconfig.yaml" <<'YAML'
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: GPG/v1alpha1
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        privateKeyPGPFile: /data/private.asc
        publicKeyPGPFile: /data/public.asc
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: GPGSigningConfiguration/v1alpha1
  verifier:
    type: GPGSigningConfiguration/v1alpha1
YAML

ocm_in_image() {
  docker run --rm --platform "linux/$ARCH" -e HOME=/data -v "$WORK/data:/data" -w /data "$TAG" "$@"
}
ocm_in_image add component-version --repository /data/ctf --constructor /data/constructor.yaml >/dev/null 2>&1 \
  || ocm_in_image add component-version --repository /data/ctf /data/constructor.yaml >/dev/null \
  || setup_fail "ocm add component-version in image"

# --- exercise ------------------------------------------------------------------
REF="/data/ctf//ocm.software/review/pr3691-f2:v1.0.0"
set +e
sign_out="$(ocm_in_image sign cv "$REF" --config /data/ocmconfig.yaml 2>&1)"; sign_rc=$?
verify_out="$(ocm_in_image verify cv "$REF" --config /data/ocmconfig.yaml 2>&1)"; verify_rc=$?
set -e
echo "--- ocm sign cv (in image $TAG, linux/$ARCH): exit $sign_rc"; echo "$sign_out" | tail -5
echo "--- ocm verify cv: exit $verify_rc"; echo "$verify_out" | tail -5

# --- assert the CORRECT behavior -------------------------------------------------
if [[ $sign_rc -ne 0 || $verify_rc -ne 0 ]]; then
  echo "REPRODUCED: GPG sign/verify fails inside the CLI container image"; exit 1
fi
echo "OK: GPG sign and verify work inside the CLI container image"
