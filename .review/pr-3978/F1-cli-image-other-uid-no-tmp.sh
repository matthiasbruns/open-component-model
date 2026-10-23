#!/usr/bin/env bash
# PR 3978 / F1: the CLI images' /tmp and /.cache are no longer writable for a
# user ID other than 65532 (docker run -u, Kubernetes runAsUser, OpenShift
# arbitrary UIDs), so os.TempDir() users fail unless the caller mounts /tmp.
#
# Expected: /tmp in the slim CLI image is writable by any UID (1777, as on main).
# Actual on 9bcb427b0: /tmp is 0755 and owned by 65532; e.g. reading a CTF
#   archive as uid 1000 fails with "mkdir /tmp/ctf-...: permission denied".
#
# Contract: exit 0 = behaves correctly, exit 1 = reproduced, other = inconclusive.
#   REPO_ROOT=/path/to/checkout [OCM_BIN=/path/to/ocm-linux-<arch>] bash .review/pr-3978/F1-cli-image-other-uid-no-tmp.sh
# Needs Docker with buildx; pulls the digest-pinned Garden Linux image the Containerfile names.
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
WORK="$(mktemp -d)"
TAG="ocm-review-pr3978-f1:$$"
cid=""
trap 'rm -rf "$WORK"; [[ -n "$cid" ]] && docker rm "$cid" >/dev/null 2>&1; docker rmi -f "$TAG" >/dev/null 2>&1 || true' EXIT
setup_fail() { echo "SETUP FAILED: $*" >&2; exit 2; }

ARCH="$(docker version -f '{{.Server.Arch}}')" || setup_fail "docker"
mkdir -p "$WORK/ctx/tmp/bin"
if [[ -n "${OCM_BIN:-}" ]]; then
  cp "$OCM_BIN" "$WORK/ctx/tmp/bin/ocm-linux-$ARCH"
else
  (cd "$REPO_ROOT/bindings/go/cli" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -o "$WORK/ctx/tmp/bin/ocm-linux-$ARCH" .) ||
    setup_fail "cli build"
fi
cp "$REPO_ROOT/bindings/go/cli/Containerfile" "$WORK/ctx/Containerfile"
docker buildx build -q --load --target slim -t "$TAG" -f "$WORK/ctx/Containerfile" "$WORK/ctx" >/dev/null ||
  setup_fail "image build"

cid="$(docker create "$TAG")" || setup_fail "docker create"
listing="$(docker export "$cid" | tar -tvf - 2>/dev/null)" || setup_fail "docker export"
echo "$listing" | grep -E ' (tmp|\.cache|\.sigstore)/?$' || true

tmp_mode="$(awk '$NF ~ /^tmp\/?$/ {print $1}' <<<"$listing")"
[[ -n "$tmp_mode" ]] || { echo "REPRODUCED: image has no /tmp"; exit 1; }
# drwxrwxrwt: others may write (sticky bit set).
if [[ "${tmp_mode:8:1}" != "w" ]]; then
  echo "REPRODUCED: /tmp is $tmp_mode, not writable for UIDs other than its owner"; exit 1
fi
echo "OK: /tmp is $tmp_mode"
