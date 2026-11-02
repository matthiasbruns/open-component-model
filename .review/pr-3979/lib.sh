# Shared helpers: render the chart like image-scan.yml job "chart" and run the
# same Trivy config scan (pinned checks bundle, OCM checks, data, ignorefile).
root="$(git rev-parse --show-toplevel)"
render() { # <outdir> [helm --set args...]
  local out=$1; shift
  helm template ocm-k8s-toolkit "$root/bindings/go/kubernetes/controller/chart" \
    --namespace ocm-k8s-toolkit-system --set manager.networkPolicy.enabled=true "$@" \
    --output-dir "$out" >/dev/null || return 2
  mkdir -p "$out/build/cli" "$out/build/controller"
  cp "$root/bindings/go/cli/Containerfile" "$out/build/cli/Containerfile"
  cp "$root/bindings/go/kubernetes/controller/Dockerfile" "$out/build/controller/Dockerfile"
}
scan() { # <dir> <report.json>
  TRIVY_CHECKS_BUNDLE_REPOSITORY="$(grep -E '^TRIVY_CHECKS_IMAGE=' "$root/.env" | cut -d= -f2)" \
  trivy config --quiet --skip-version-check \
    --config-check "$root/.github/trivy-checks" --check-namespaces user \
    --config-data "$root/.github/benchmarks/trivy-data" \
    --ignorefile "$root/.github/benchmarks/trivyignore.yaml" \
    --format json --output "$2" "$1"
}
failed() { jq -r '.Results[]? | .Target as $t | .Misconfigurations[]? | "\($t) \(.ID) \(.Message)"' "$1"; }
