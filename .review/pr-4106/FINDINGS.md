# Review evidence — PR #4106

Pull request: <https://github.com/open-component-model/open-component-model/pull/4106>
Reviewed head: `5ecc72904801f031d5147661bc76dca5249d9b1e`
Merge base: `6dd848caa3e01b07c83cc741d40612ee510605ec`

Review-only evidence. Everything here is additive; no file the PR touches is modified.

| File | Purpose |
| --- | --- |
| `bindings/go/git/repository/review_pr4106_test.go` | The reproductions, as tests in the package under review |
| `.review/pr-4106/suggested-fix.patch` | A candidate fix for F2, not applied |

Run from `bindings/go`:

```sh
go test ./git/repository/ -run 'TestReviewPR4106|TestUploadTagToEmptyRepositoryWarns' -race -v
```

## F2 (confirmed) — the new guard is suppressed in the state it exists to flag

`bindings/go/git/repository/upload.go:159` warns on:

```go
if targetRef.IsTag() && !hasBranch(refs) {
```

The condition keys off *"does any branch exist"*. It never asks whether the target's HEAD
resolves — although the PR's own hint and the `GitUploaderConfig` doc comment both name HEAD as
the real condition ("push the branch the target's HEAD points at").

So: seed any branch whose name differs from the target's HEAD, then push a tag. A branch exists,
`hasBranch(refs)` is true, the warning is suppressed. HEAD still dangles, so Git hosts still
render the repository as empty and `git clone` still checks out no working tree — the exact
symptom the warning was added for.

This is the fixture of the PR's own passing case `tag into repo with a branch is silent`
(`bindings/go/git/repository/upload_warn_test.go:74`): go-git's `PlainInit` points HEAD at
`refs/heads/master` (`go-git/v6@v6.0.0-alpha.5` `repository.go:103`, `defaultBranch: plumbing.Master`)
while the test pushes `refs/heads/main`. The case was added in `b8b1ab2` for mutation coverage and
inadvertently asserts silence on a broken target.

Repro: `TestReviewPR4106_F2_TagSuppressedWhileHeadDangles`.

```text
--- FAIL: TestReviewPR4106_F2_TagSuppressedWhileHeadDangles
    Error:    Should NOT be empty, but was []
    Messages: a tag push into a target whose HEAD does not resolve must warn; hasBranch(refs)
              suppressed it because an unrelated branch exists. Joined messages: ""
```

Before that failing assertion the test establishes, as preconditions, that a branch exists, that
`repo.Head()` fails, and that real `git clone` prints `remote HEAD refers to nonexistent ref`.

### A precise fix costs nothing extra

`TestReviewPR4106_Probe_AdvertisedHeadRevealsDanglingHead` (passes) records the protocol fact:
the refs `push` has **already** fetched via `remote.ListContext` carry HEAD as a *symbolic*
reference when HEAD resolves, and omit HEAD entirely when it dangles.

```text
head dangles  (HEAD=master, pushed main): refs/heads/main  type=hash-reference
head resolves (HEAD=main,   pushed main): HEAD             type=symbolic-reference target="refs/heads/main"
                                          refs/heads/main  type=hash-reference
```

A server advertises HEAD only when it resolves, so `len(refs) > 0 && no HEAD in refs` means
"HEAD dangles" with no extra round-trip. `suggested-fix.patch` implements that. With it applied:

* `TestReviewPR4106_F2_TagSuppressedWhileHeadDangles` — passes
* `TestReviewPR4106_FixDoesNotOverfire` — passes (a healthy target stays silent)
* `TestUploadTagToEmptyRepositoryWarns/tag_into_repo_with_a_branch_is_silent` — **fails**, which
  is the correct outcome: that fixture needs its target HEAD set to `refs/heads/main` to be the
  healthy state it claims to assert.

## F1 (confirmed symptom, bounded cause) — the branch path asserts the wrong invariant

`TestReviewPR4106_F1_BranchPushLeavesHeadUnresolved` shows a *branch* push producing the same
broken end state, silently: `refs/heads/main` into a fresh repo whose HEAD is `refs/heads/master`.
`git clone` reports the same broken HEAD.

This instance is **not** fixable before the push and still fails with the patch applied: an empty
remote advertises nothing, so the uploader cannot learn which branch HEAD names. The actionable
half is that `branch into empty repo is silent`
(`bindings/go/git/repository/upload_warn_test.go:73`) encodes "branch push ⇒ healthy", which its
own fixture disproves. Worth either renaming or documenting as a protocol limit.

## Gates run on the reviewed head

| Gate | Result |
| --- | --- |
| `go test -race ./git/repository/` (PR tests only, evidence removed) | pass |
| `go test -race ./git/...` | pass except `git/integration` |
| `git/integration` | `rootless Docker not found` — environmental, not the PR |
| `golangci-lint run ./git/repository/... ./transfer/v1alpha1/spec/...` | `0 issues` |
| `gofmt -l` on changed files | clean |

Not checked: the live GitLab/SSH path from the PR's evidence section (no credentials), and the
`transfer` end-to-end uploader wiring beyond the doc and schema change.
