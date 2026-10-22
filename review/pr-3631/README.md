# Review evidence for open-component-model PR #3631

Base: PR head `0f10ba10b8d252b55c991c00b4ee4e407aa054fc`.

## Argo CD Helm localization

Run `bash review/pr-3631/check-argo-helm-parameters.sh` from this worktree. On the PR head it fails with `Helm parameter 0: expected name image.repository, got null`. The Crossplane Composition patches `spec.source.helm.parameters[0].value` and `[1].value`, but the Application base declares only `releaseName`. Argo CD Helm parameters need names to identify the chart values. This check asserts the intended names, `image.repository` and `ui.message`; it does not prove how Crossplane renders missing entries at runtime. The E2E suite checks localization on Kro deployments, not the Argo CD workload.

Relevant PR lines: `bindings/go/kubernetes/controller/examples/helm-configuration-localization/crossplane-composition.yaml:231-232,274-277`.

## Cross-namespace teardown

The [E2E CI job for head `0f10ba10`](https://github.com/open-component-model/open-component-model/actions/runs/39777207011/job/119820448455) reports `16 Passed | 1 Failed`: `k8s-manifest-cross-namespace` times out during `DeferCleanup` while deleting its Repository from `bootstrap.yaml`. The same log reports ApplySet prune skipped because the service account is gone. That manifest includes the NamespacedDeployer and its ServiceAccount/RBAC; `DeployResource` deletes them together with `kubectl delete --wait=true --timeout=180s -f bootstrap.yaml`.

Focused live reproduction, requiring the repository's local Kind E2E setup: `task bindings/go/kubernetes/controller:test/e2e -- --focus=k8s-manifest-cross-namespace --procs=1`. The focused command has not been run locally; the linked CI job is the observed failure. Teardown should keep the service account and RBAC available until the NamespacedDeployer finalizer completes, then remove the remaining bootstrap resources.

Relevant PR lines: `bindings/go/kubernetes/controller/test/utils/utils.go:94` and `bindings/go/kubernetes/controller/examples/k8s-manifest-cross-namespace/bootstrap.yaml:40-102`.
