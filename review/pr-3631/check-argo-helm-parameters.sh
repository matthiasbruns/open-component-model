#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
composition="$repo_root/bindings/go/kubernetes/controller/examples/helm-configuration-localization/crossplane-composition.yaml"

parameter_name() {
  local index="$1"
  yq -r "select(.kind == \"Composition\") | .spec.pipeline[0].input.resources | map(select(.name == \"argocdApplication\")) | .[0].base.spec.source.helm.parameters[$index].name" "$composition"
}

check_parameter() {
  local index="$1" expected="$2" actual
  actual="$(parameter_name "$index")"
  if [[ "$actual" != "$expected" ]]; then
    printf 'Helm parameter %s: expected name %q, got %q\n' "$index" "$expected" "$actual" >&2
    return 1
  fi
}

check_parameter 0 image.repository
check_parameter 1 ui.message
printf 'Argo CD Helm parameter names are present.\n'
