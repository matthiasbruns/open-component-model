#!/usr/bin/env bash
# OCM-DS-0001 states "a build cannot pull a changed base image unnoticed".
# BuildKit also pulls images named by COPY --from=<image>, RUN --mount=from=<image>
# and the "# syntax=" frontend directive. Each fixture pins every FROM and pulls
# one image by tag through one of those routes.
# Exit 0: every indirect pull is rejected; 1: reproduced (accepted); 2: setup failure.
set -uo pipefail
root="$(git rev-parse --show-toplevel)"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
d="sha256:$(printf 'a%.0s' {1..64})"
mk() { mkdir -p "$tmp/$1"; printf '%s\n' "${@:2}" > "$tmp/$1/Dockerfile"; }
mk control "FROM alpine:3.22 AS base" "USER 65532"
mk copy-from "FROM alpine:3.22@$d AS base" "COPY --from=busybox:1.37 /bin/busybox /bin/busybox" "USER 65532"
mk mount-from "FROM alpine:3.22@$d AS base" "RUN --mount=type=bind,from=busybox:1.37,target=/b true" "USER 65532"
mk syntax "# syntax=docker/dockerfile:1" "FROM alpine:3.22@$d AS base" "USER 65532"
rc=0
for c in control copy-from mount-from syntax; do
  trivy config --quiet --skip-version-check --config-check "$root/.github/trivy-checks" \
    --check-namespaces user --format json --output "$tmp/$c.json" "$tmp/$c" || exit 2
  if jq -e 'any(.Results[]?.Misconfigurations[]?; .ID == "OCM-DS-0001")' "$tmp/$c.json" >/dev/null; then
    echo "$c: rejected by OCM-DS-0001"
  else
    echo "$c: ACCEPTED by OCM-DS-0001"
    [ "$c" = control ] && exit 2
    rc=1
  fi
done
exit $rc
