# PR #3991 review evidence

Scope vs issue: the PR links no issue. Independently located ocm-project#1395 and #1333 establish reduced buffering, lazy fetch, and skip-existing as broader goals. The fused paths address intermediate buffering only; graph-native CTF transfer (#1318), full lazy fetching, skip-existing, and benchmarks remain separate work. This PR does not claim to close those issues.

Reviewed head: `0bae7be2f900e45d79c4d12c4cc50c25dbfc7225`.

## R1 — P2: release source streams on early target failure

Draft inline comment at `bindings/go/oci/transformer/transfer_local_blob.go:85-87`:

`src.open` returns an already-open source stream for ordinary local blobs. If `AddLocalResource` returns before reading it (for example, target store resolution fails), this branch returns without releasing the source. The previous Get → Add path consumed and closed it before attempting the target operation. Repeated failures can accumulate open source files or HTTP response bodies. Please give the fused operation explicit source-lifetime ownership and release the underlying reader on every exit, including before the target starts consuming it; ensure partial-read cleanup closes the underlying stream even when digest verification fails.

Reproduction from repository root:

```sh
sh .review/pr-3991/reproduce.sh
```

Equivalent command from `bindings/go`:

```sh
go test ./oci/transformer -run '^TestReviewPR3991_SourceClosedOnTargetFailure$' -count=1 -v
```

Observed: `previous_get_add` PASS; `fused` FAIL: `source reader must be closed when target fails before consuming it`.

The regression test uses the production DescriptorBlob with a tracked source reader and a target that fails before consuming data. It establishes ownership/cleanup behavior, not an HTTP load or file-descriptor exhaustion benchmark. The comparison exercises the retained Get/Add implementations, which have no diff from the PR base. A separate base checkout was not executed; the fused API is new in this PR.

## Existing discussions

Two unresolved threads were inspected. The outdated tutorial comment is addressed by current type names. The current source-access leakage concern is not supported by the built-in packing path: `updateArtifactAccess` constructs fresh destination access, setting its local digest and optional destination global access. No thread was replied to or resolved.

## Validation

- `task bindings/go:test -- -run 'Test(Transfer|BuildGraph|Graph|LocalBlob|OCI|Copy|Label|Default)'`: PASS, exit 0. This is a focused name-filtered suite, not all tests.
- `task tools:lint`: PASS, exit 0, both modules report 0 issues. Initial sandbox cache failures were retried with permission; evidence-only formatting was corrected.
- Regression command above: expected FAIL, exit 1, prior path comparison PASS.
- GitHub head CI inspected: unit/integration tests, lint, generation and builds successful. Some publishing/aggregate checks skipped; not represented as executed.
- Full diff reviewed across transformer runtime, graph wiring, type/schema generation, tests, and docs. Compared sibling Get/Add implementations, current storage/fetch/packing, OCM localBlob spec at cd21da0ebe190e4a0ed11c6d3de51998889f233a, and legacy localblob implementation at 4685558ce984b284768dcef49ed41fd65305f80a.

Not executed locally: integration/container tests, race tests, generation, separate merge-base checkout, exhaustive runtime tests of every source/target/policy combination, or disk/memory benchmarks. The skill's workspace-specific doc-xref script was not run; docs and schema changes were inspected directly.

Optional docs clarification: selection-examples.md says no buffering in a file; nested OCI materialization still uses scratch storage. Clarify that only the intermediate Get/Add file is eliminated. Static observation, not a separate reproduced finding.

Evidence branch: `review/pr-3991-reproductions` on `matthiasbruns/open-component-model`. Review comments remain local; publishing this evidence does not post a review.
