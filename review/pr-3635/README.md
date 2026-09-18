# Review artifacts for PR #3635

Reproductions for the review of
[#3635 — feat(versioning): pluggable, config-driven versioning schemes](https://github.com/open-component-model/open-component-model/pull/3635).

This branch is `4ac17e67e` (the PR head) plus this directory and two test files.
It is **not** a merge candidate and is **not** rebased onto `main` on purpose:
the repro has to pin the exact commit under review.

## The finding

A configured versioning scheme is honoured on write and ignored on read.
`ocm add cv` stores the component version, `ocm get cv` silently omits it, and
`ocm get cv --latest` returns a wrong answer. Both `get` commands exit 0 with
empty stderr.

Cause: `oci/repository.go` still hardcodes
`lister.SortPolicyLooseSemverDescending`, whose sort loop skips every candidate
`semver.NewVersion` cannot parse. `lister.Options.Comparator`, added by this PR
to plumb the versioning registry down to that spot, is never assigned.

On `main` this state was unreachable — the constructor JSON schema rejected such
a version at parse time. This PR relaxes that pattern to `^.+$` and adds a
registry check that a configured scheme passes, so the write side now admits
versions the read side cannot return.

## Verification that this is really the cause

Three checks were run before filing:

1. **Attribution.** Patching only the `SortPolicyLooseSemverDescending` branch in
   `bindings/go/oci/internal/lister/lister.go` to keep candidates instead of
   `continue`-ing on them, changing nothing else, makes both versions appear —
   and the CLI-layer registry sort puts `2024.03.15.7` first, correctly. So the
   rest of the plumbing this PR adds already works; only the lister is unwired.
2. **Not a bad scheme.** The config uses a single scheme that claims *both*
   versions and orders them by the same capture groups. The version still
   disappears.
3. **Not a flag.** `get cv` has no flag affecting this. Its `--semver-constraint`
   defaults to `> 0.0.0-0`, so `Registry.Filter` does run, and it retains the
   version — the loss happens earlier, in the lister.

The data is intact throughout: `get cv ./ctf//acme.org/svc:2024.03.15.7`
retrieves the component version fine. It is purely the listing.

## Running it

```sh
sh review/pr-3635/repro.sh        # from the repository root
```

Builds the CLI from the branch into `/tmp/ocm-repro-cli` and works in
`/tmp/ocm-repro`; nothing is written into the repository.

Expected output: two versions added and present in `ctf/artifact-index.json`,
one of them missing from `get cv`, and `--latest` reporting the older one.

## Go tests

Both assert the behaviour that *should* hold, so they fail on this commit.

| File | Status here | Status on `main` |
|---|---|---|
| [`bindings/go/oci/pr3635_roundtrip_test.go`](../../bindings/go/oci/pr3635_roundtrip_test.go) | fails | unreachable (schema rejects the input) |
| [`bindings/go/runtime/versioning/pr3635_filter_test.go`](../../bindings/go/runtime/versioning/pr3635_filter_test.go) | `FilterDropsVersionsNoSchemeClaims` fails | passes |

```sh
go test ./oci/ -run TestPR3635 -v                 # from bindings/go
go test ./runtime/versioning/ -run TestPR3635 -v
```

`TestPR3635_FilterKeepsGenuineNonSemverScheme` passes on both and is the guard
rail: a fix must keep calver histories from being filtered out by a semver
constraint.
