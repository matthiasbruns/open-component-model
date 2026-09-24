# PR 3635 regression evidence

Review baseline: `5d3e672f9ccaff8319ae24e261403881a829ae8a` in
[PR 3635](https://github.com/open-component-model/open-component-model/pull/3635).
Verified on macOS on 2026-09-24.

This is an **evidence branch, not a fix**. It adds only tests, isolated reproducers,
and this guide. The confirmed-defect tests intentionally fail while those defects remain.
The constructor and controller probes are observations, not failing bug assertions.
Do not interpret a failing test run on this branch as an implementation attempt.
DAG discovery cycle/deadlock issues covered by
[epic 724](https://github.com/open-component-model/ocm-project/issues/724) are excluded.
The comparator defect below is unrelated to DAG discovery.

## Reassessment against author intent

The PR head is still `5d3e672f9`. Review of the author discussion and current
reference documentation leaves **four correctness defects**, **one documented
configuration gap**, and **two non-blocking design/improvement questions**:

- Keep: non-transitive comparison, operandless constraint terms, asymmetric
  foreign-constraint handling, and accepted duplicate captures losing their values.
- Keep as an integration/documentation gap: effective configuration omits the new type.
- Reclassify: uploads before rejection are an observed side effect of intentional
  post-processing validation, not a demonstrated violation of an atomicity contract.
- Reclassify: empty controller constraints are a compatibility question, not a
  proven bug. Preserving legacy rejection was an assumption in the original test.

Relevant author decisions:

- [Post-processing validation is intentional](https://github.com/open-component-model/open-component-model/pull/3635#discussion_r4064733362):
  defaulting and populated accesses require a processed descriptor. A separate
  preflight of explicit versions remains worth discussing, not moving all validation.
- [Controller APIs are deferred to a GA follow-up](https://github.com/open-component-model/open-component-model/pull/3635#discussion_r4064728736).
  This does not explicitly decide empty constraints; ask rather than infer.
- [Unknown-history retention is intentional; strict mode is a follow-up](https://github.com/open-component-model/open-component-model/pull/3635#discussion_r4064736315).
  Neither retention nor foreign operands alone are reported as bugs here.
- [The CLI flag rename was requested and implemented](https://github.com/open-component-model/open-component-model/pull/3635#discussion_r4064724967).
  Do not report it as an accidental regression.
- [Semver coercion is deferred](https://github.com/open-component-model/open-component-model/pull/3635#discussion_r4064736944).

The PR description is stale on automatic semver fallback and semver-only constraints.
The current reference and implementation explicitly support custom relational
constraints and require an explicit semver fallback once custom schemes are listed.
Those current contracts, not outdated description text, ground the retained findings.

## Isolated shell reproducers

Requirements: POSIX shell, Python 3, and the Go toolchain required by
`bindings/go/go.mod`. Existing module dependencies must be available or downloadable.
No Docker, Kubernetes cluster, registry credentials, or additional dependencies are
needed. Scripts create temporary fixtures, clean them up, and bound command runtime.
Run them from the repository root with `sh`, or invoke their absolute paths.

Exit codes:

For the confirmed-defect and configuration-gap scripts:

- `0`: the checked expectations hold (or an explicitly supported fix rejects invalid configuration).
- `1`: the reported defect/gap was reproduced, with positive controls passing.
- `2`: setup, timeout, or unexpected behavior; not evidence of the reported defect.

The constructor-upload and controller-empty-constraint scripts instead return `0`
when the observation and controls complete, and `2` for an unexpected outcome or
setup failure. They no longer label the observed behavior a confirmed bug.

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
This finding is specifically about missing operands becoming satisfied predicates,
not the intentional treatment of unrecognized operands as foreign grammar.

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
Duplicate-name support is not promised; rejecting ambiguous configuration is enough.
The finding is acceptance followed by silent corruption, not a requirement to merge captures.

Locations: `bindings/go/configuration/versioning/v1alpha1/spec/config.go:195-204`
and `bindings/go/runtime/versioning/versioning.go:570-574`.

### 4. Improvement question: preflight explicit versions before uploading

```sh
sh review-pr3635-invalid-artifact-uploads.sh
```

For both resources and sources, `not-a-version` is rejected without publishing
a descriptor, but its input payload persists as a readable blob in the CTF.
Valid-version controls publish both descriptor and content.
The author explicitly intends post-processing validation for defaulted versions and
populated accesses. We have not established a side-effect-free failure contract.
Ask whether explicit versions can be checked separately before processing, retaining
post-processing validation. This evidence supports that improvement discussion, not
an unconditional bug verdict or a demand to move all validation earlier.
The script inspects storage through `bindings/go/blob/filesystem` and public
CTF APIs, not by manufacturing or assuming CTF blob paths.

Location: `bindings/go/constructor/construct.go:235-244`.

### 5. Compatibility question: intended empty-controller-constraint semantics

```sh
sh review-pr3635-controller-empty-constraint.sh
```

The legacy semver parser rejects an empty constraint. The new selection path
returns `2.0.0-rc.1` for an empty constraint, whereas explicit `*` selects
`1.0.0`. Controls also check regexp filtering and preservation of original
version spelling. The registry deliberately treats an empty filter as unrestricted.
Whether the controller should inherit that policy or retain legacy rejection needs
clarification, particularly given the planned controller API follow-up. This is
not classified as a confirmed bug. The test records the reviewed baseline without
claiming that legacy rejection is required.

Location: `bindings/go/kubernetes/controller/internal/ocm/ocm.go:128-142`.

### 6. Integration/documentation gap: effective config omits versioning

```sh
sh review-pr3635-effective-config.sh
```

Configured non-semver `build-10` is successfully published and retrieved with
a constraint, proving the scheme is active. `get config --output json` then
omits `versioning.config.ocm.software/v1alpha1` entirely. This contradicts the
new tutorial's verification step, but does not mean versioning itself is inactive.
Either include the type in effective-config output or correct the promised behavior.

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

Review files use `review_pr3635*test.go` names. Confirmed-defect tests assert
correctness. Constructor/controller tests now characterize behavior without
asserting that side-effect-free failure or legacy empty-constraint rejection is
required. A future controller policy change may require updating its baseline probe.

Additional retained evidence from the review includes passing OCI round-trip
checks, a characterization of direct-write validation (not claimed as a newly
introduced regression), and routing probes. Some characterization probes assert
the observed baseline behavior and should be revised when fixing that behavior.

The legacy-schema comparison is opt-in and uses `gh api` to fetch a pinned public
schema. It requires GitHub access and is not needed for the local reproducers:

```sh
OCM_REVIEW_EXTERNAL_AWSDATE=1 go test ./descriptor/v2 \
  -run '^TestReviewPR3635ExternalAWSDateCompatibility5d3e672f$' -count=1 -v
```

It confirms that legacy schema validation rejects `2024.03.15`, but accepts
`2024-03-15` as relaxed semver. This is evidence for an interoperability decision,
not a claim that every legacy OCM workflow rejects custom versions.

## Validation record

- Original run: all eight scripts returned `1`, but two encoded unconfirmed
  requirements. That was insufficient to classify all observations as defects.
- Reassessment: constructor/controller probes were changed to observations;
  the remaining six scripts still cover four correctness defects and one config gap
  (the comparator has two scripts).
- Re-run after reassessment: all eight syntax checks passed; six defect/gap scripts
  returned `1`, and the two observation scripts returned `0`. All controls passed.
- Original focused Go tests failed on both confirmed defects and assumed requirements.
  Constructor/controller assertions have since been corrected to remove those assumptions;
  both focused Go suites now pass while still demonstrating the same observed behavior.
- `task bindings/go:test -- -run TestReviewPR3635 -count=1`: completed with failure
  (Task exit 201); this is a filtered review-test run, not a full-suite pass.
- `task tools:lint`: zero issues.
- Full external integration/e2e suites were not run as part of this evidence commit.

The scripts and tests are intended to make review comments independently
reproducible. No production fixes or changes to the PR author's branch are included.
