# PR 3635 regression evidence

Review baseline: `5d3e672f9ccaff8319ae24e261403881a829ae8a` in
[PR 3635](https://github.com/open-component-model/open-component-model/pull/3635).
Verified on macOS on 2026-09-24.

This is an **evidence branch, not a fix**. It adds only tests, isolated reproducers,
and this guide. The regression tests intentionally fail while the defects remain.
Do not interpret a failing test run on this branch as an implementation attempt.
DAG discovery cycle/deadlock issues covered by
[epic 724](https://github.com/open-component-model/ocm-project/issues/724) are excluded.
The comparator defect below is unrelated to DAG discovery.

## Isolated shell reproducers

Requirements: POSIX shell, Python 3, and the Go toolchain required by
`bindings/go/go.mod`. Existing module dependencies must be available or downloadable.
No Docker, Kubernetes cluster, registry credentials, or additional dependencies are
needed. Scripts create temporary fixtures, clean them up, and bound command runtime.
Run them from the repository root with `sh`, or invoke their absolute paths.

Exit codes:

- `0`: the checked correctness expectations hold (or an explicitly supported fix rejects invalid configuration).
- `1`: the reported defect was reproduced, with positive controls passing.
- `2`: setup, timeout, or unexpected behavior; not evidence of the reported defect.

The six follow-up scripts do not depend on the added Go review test files.
Five exercise actual `go run main.go` invocations from `bindings/go/cli`.
The controller script supplies an isolated Ginkgo suite through a temporary Go
`-overlay`, because the controller selection function is not CLI-reachable.
It does not create or replace files in the checkout.

### Previously confirmed: non-transitive comparison

```sh
sh review-pr3635-comparison-cycle.sh
sh review-pr3635-cli-ordering.sh
```

A numeric-or-text capture produces `2 < 10 < 1a < 2` because `compareGroup`
changes between numeric and lexical comparison depending on the pair.
The public registry returns three different latest values across permutations.
The CLI selects `1a` from `[10, 1a]`, but `10` from `[2, 10, 1a]`.
All six CLI insertion permutations produced the same result: insertion-order
variation was demonstrated at the API level, not at the CLI level.

Location: `bindings/go/runtime/versioning/versioning.go:583-589`.

### 1. Malformed constraints broaden listing and transfer selection

```sh
sh review-pr3635-malformed-constraints.sh
```

`>=` and `<` select every version. `>=2024.03.15 <` silently drops its upper
bound and selects both `2024.03.15` and `2024.10.01`. Confirmed with listings
and transfer dry-run graphs. Complete lower, upper, and combined bounds are
positive controls. Repository-helper tests cover both listing and transfer filters.

Location: `bindings/go/runtime/versioning/versioning.go:535-536`.

### 2. Custom constraints fail on mixed custom/semver histories

```sh
sh review-pr3635-mixed-constraints.sh
```

`>=build-100` works on custom-only history. Adding `1.0.0` and `2.0.0` causes
`parsing semantic version constraint failed: improper constraint: ">=build-100"`.
The same mixed history accepts semver constraints and retains custom versions,
so the problem is asymmetric foreign-constraint handling, not invalid fixtures.

Location: `bindings/go/runtime/versioning/versioning.go:372-378`.

### 3. Duplicate captures corrupt comparison and constraint matching

```sh
sh review-pr3635-duplicate-captures.sh
```

The accepted pattern `^(?:v(?P<n>\d+)|r(?P<n>\d+))$`, comparing group `n`,
makes `v2` equal to `v10`; `v2` incorrectly satisfies `>=v10`.
An equivalent single-capture pattern is the positive control.
Both the Go test and script accept explicit duplicate-capture rejection as a fix.

Locations: `bindings/go/configuration/versioning/v1alpha1/spec/config.go:195-204`
and `bindings/go/runtime/versioning/versioning.go:570-574`.

### 4. Invalid artifact versions upload content before rejection

```sh
sh review-pr3635-invalid-artifact-uploads.sh
```

For both resources and sources, `not-a-version` is rejected without publishing
a descriptor, but its input payload persists as a readable blob in the CTF.
Valid-version controls publish both descriptor and content.
The script inspects storage through `bindings/go/blob/filesystem` and public
CTF APIs, not by manufacturing or assuming CTF blob paths.

Location: `bindings/go/constructor/construct.go:235-244`.

### 5. Empty controller constraints implicitly select prereleases

```sh
sh review-pr3635-controller-empty-constraint.sh
```

The legacy semver parser rejects an empty constraint. The new selection path
returns `2.0.0-rc.1` for an empty constraint, whereas explicit `*` selects
`1.0.0`. Controls also check regexp filtering and preservation of original
version spelling. This is a controller-specific validation regression, not a
request to change the CLI's intentionally unrestricted empty filter.

Location: `bindings/go/kubernetes/controller/internal/ocm/ocm.go:128-142`.

### 6. Effective configuration omits the active versioning scheme

```sh
sh review-pr3635-effective-config.sh
```

Configured non-semver `build-10` is successfully published and retrieved with
a constraint, proving the scheme is active. `get config --output json` then
omits `versioning.config.ocm.software/v1alpha1` entirely.

Missing integration: `bindings/go/cli/cmd/get/config/cmd.go:108-179`.
Tutorial promise: `website/content/docs/tutorials/configure-versioning.md:122-128`.

## Go tests

From `bindings/go`, run the review tests (expected to fail on the baseline):

```sh
go test ./runtime/versioning ./configuration/versioning/v1alpha1/spec \
  ./cli/internal/repository/ocm ./cli/cmd/get/config ./constructor \
  ./repository/component/pathmatcher/v1alpha1 ./oci ./descriptor/v2 \
  -run TestReviewPR3635 -count=1 -v

go test ./kubernetes/controller/internal/ocm -count=1 \
  -ginkgo.focus='ReviewPR3635 version selection' -ginkgo.no-color
```

Review files use `review_pr3635*test.go` names. The constructor and controller
regressions assert correct behavior (no invalid-input upload; empty-constraint
rejection), rather than asserting that the defect persists.

Additional retained evidence from the review includes passing OCI round-trip
checks, a characterization of direct-write validation (not claimed as a newly
introduced regression), and routing probes. Some characterization probes assert
the observed baseline behavior and should be revised when fixing that behavior.

The legacy-schema comparison is opt-in and uses `gh api` to fetch a pinned public
schema. It requires GitHub access and is not needed for the six regressions:

```sh
OCM_REVIEW_EXTERNAL_AWSDATE=1 go test ./descriptor/v2 \
  -run '^TestReviewPR3635ExternalAWSDateCompatibility5d3e672f$' -count=1 -v
```

It confirms that legacy schema validation rejects `2024.03.15`, but accepts
`2024-03-15` as relaxed semver. This is evidence for an interoperability decision,
not a claim that every legacy OCM workflow rejects custom versions.

## Validation record

- All eight shell scripts: syntax checks passed; execution returned `1` with the
  reported defects reproduced and their controls passing. No setup failures.
- Focused Go regression tests: failed on the reported defects; positive controls passed.
- Controller: two control specs passed; empty-constraint regression failed.
- `task bindings/go:test -- -run TestReviewPR3635 -count=1`: completed with failure
  (Task exit 201); this is a filtered review-test run, not a full-suite pass.
- `task tools:lint`: zero issues.
- Full external integration/e2e suites were not run as part of this evidence commit.

The scripts and tests are intended to make review comments independently
reproducible. No production fixes or changes to the PR author's branch are included.
