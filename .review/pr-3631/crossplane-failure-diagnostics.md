# PR #3631 — the Crossplane e2e path has no failure diagnostics

Observed on PR head `9b3df64c4855e184c0cef2b5611ebf9ecfc6c48e`, pipeline run
[42029676350](https://github.com/open-component-model/open-component-model/actions/runs/42029676350):

```
E2E / E2E Tests (arm64) :: failure
Ran 17 of 17 Specs — 16 Passed | 1 Failed
[FAIL] controller examples [It] should deploy the example kustomize-simple
  bindings/go/kubernetes/controller/test/e2e/e2e_examples_test.go:152
  kubectl wait --for=condition=Ready=true -f .../kustomize-simple/crossplane-instance.yaml --timeout=180s
  error: timed out waiting for the condition on kustomizesimplecrossplanes/kustomize-simple-crossplane
```

Timings, from the spec timelines:

| head | "creating the Crossplane XR instance" | XR Ready |
|---|---|---|
| `f0de604035` (green, run 42007598279) | 10:13:42.438 | ~10:14:43 (~61 s) |
| `9b3df64c48` (red, run 42029676350) | 12:53:44.421 | never — timed out 12:56:44.641 |

So the XR did not merely overrun the timeout; it took >3x longer and never became Ready.

## Why the cause is not determinable from the run output

`test/e2e/e2e_examples_test.go:45-46` dumps only:

```go
utils.DumpLogs("kro", "rgd")
utils.DumpLogs("argocd", "applications.argoproj.io")
```

and `utils.DumpLogs` (`test/utils/utils.go:372-399`) is hardcoded to kro:

```go
logCmd("kro-pods",       "kubectl", "get", "pods", "-n", namespace, "-o", "wide")
logCmd("kro-events",     "kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
logCmd("rgd-conditions", "kubectl", "get", resourceType, "-o", "custom-columns=...")
logCmd("kro-logs",       "kubectl", "logs", "-n", namespace, "--all-containers", "--tail=100",
                         "-l", "app.kubernetes.io/name=kro")
```

`utils.go` is otherwise untouched by this PR (the only change is the `DeployResource`
cleanup command). Consequently the red run carries:

- no XR status or conditions,
- no composed-resource conditions (the OCM `Resource`, Flux `GitRepository` / `Kustomization`),
- no `crossplane-system` pod logs,
- an OCM controller log tail that only reaches back to cleanup — the first entry for
  `kustomize-simple-crossplane-resource-kustomization` is at 12:56:45, "resource is marked for deletion".

Checks that came back clean on the red run, for the record:

```
$ grep -ci "forbidden\|cannot get resource\|cannot create resource" <run log>     # 0
$ grep -oE "\[(OK|FAIL)\] [a-z-]+" <run log> | sort -u
[OK] argocd
[OK] crossplane
[OK] flux
[OK] image-registries
[OK] kro
```

The ArgoCD diagnostics do show both `application/kustomize-simple` and
`application/kustomize-simple-crossplane` oscillating `Synced -> OutOfSync` with repeated
"Partial sync operation ... succeeded".
