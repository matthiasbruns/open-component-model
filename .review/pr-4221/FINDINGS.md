# PR #4221 review evidence — `chore(deps)!: migrate oras-go from v2 to v3`

Head reviewed: `462312f42b1600549b72b07a4c6890c6db42009f`
Control commit: `c1f262431a` (the PR head's parent)
Toolchain: Go 1.27.4, golangci-lint v2.15.0 (the `.env` pin)

Run everything from `bindings/go`:

```bash
go test ./oci/credentials/ ./oci/repository/resource/ \
        ./oci/internal/remotestore/ ./oci/repository/provider/ ./oci/cache/ \
        -run TestReviewPR4221 -count=1 -v
```

## F1 — credential matching is case-sensitive against a lower-cased registry name

Status: **confirmed, introduced by this PR.** The two tests assert the correct
behaviour, so they fail on the head and pass once the comparison is fixed.

| test | file |
| --- | --- |
| `TestReviewPR4221_F1_CredentialFuncIsCaseSensitive` | `bindings/go/oci/credentials/review_pr4221_test.go` |
| `TestReviewPR4221_F1b_StaticCredentialFuncIsCaseSensitive` | `bindings/go/oci/repository/resource/review_pr4221_test.go` |

oras-go v2 handed the credential function `originalReq.Host` verbatim
(`registry/remote/auth/client.go` in v2.6.2). v3 hands it a
`properties.Resource` whose `Registry` has been lower-cased by
`auth.canonicalRegistry` (`registry/remote/auth/resource.go`). Both OCM call
sites compare that with `==` against a value that keeps the case the user
wrote:

- `oci/credentials/docker_config.go` — `identity.Hostname`, which reaches it
  through `runtime.ParseURLToIdentity` -> `url.URL.Hostname()`.
- `oci/repository/resource/resource_repository.go` — `StaticCredentialFunc(url.Host, ...)`,
  whose argument is compared against `resource.Host()`.

A registry host with any uppercase letter in `BaseUrl` therefore resolves no
credential. The request goes out unauthenticated and fails with
`basic credential not found`, and nothing logs that a configured credential was
dropped.

Results:

```text
head 462312f42b  oci/credentials          lower PASS  upper FAIL  mixed FAIL
base c1f262431a  oci/credentials          lower PASS  upper PASS  mixed PASS
head 462312f42b  oci/repository/resource  lower PASS  upper FAIL  mixed FAIL
base c1f262431a  oci/repository/resource  lower PASS  upper PASS  mixed PASS
```

A two-line fix: `strings.EqualFold` in `CredentialFunc`, and
`strings.ToLower(url.Host)` at the `StaticCredentialFunc` call site.

## Claims checked and found correct

These tests pass on the head. They are here because the migration either
removed the coverage that pinned the behaviour or never had any.

### The chunked-push redirect guard, and why `Clone()` is load-bearing

`TestRemoteStore_Push_DoesNotFollowPATCHRedirectToOtherHost` ships with the PR
and already tables both client types. Two mutations on the head confirm it is
not decorative:

- Removing the `noFollowRedirects(client)` call in `RemoteStore.do`: both
  sub-cases fail with `the redirect target must never be contacted`. So v3's own
  `redirectSafeClient` does **not** suppress the redirect on its own; OCM's
  guard is what stops the blob body reaching the redirect target.
- Replacing `client.Clone()` with the v2-style `copyClient := *client`: the
  `auth.Client` sub-case fails and the blob reaches the attacker host, while the
  `http.Client` sub-case still passes. `go vet` also reports
  `assignment copies lock value ... contains sync.Once`. The struct copy
  inherits the original client's already-built, redirect-following cached
  client, so the switch to `Clone()` is a security fix, not a tidy-up.

### `review_pr4221_untag_test.go` — restores the deleted `remote_store_test.go` tables

`remote_store_test.go` was deleted with `RemoteStore.Untag`. Both of its tables,
re-pointed at the now-native v3 `Untag`, still pass: 200/202/204 succeed, 404
maps to `errdef.ErrNotFound`, and all of `sha256:...`, `sha512:abc123def456`,
`""`, `not:a:tag` and `.badtag` are still rejected before any request is sent.

One behaviour did change: 405 no longer yields the named
`ErrTagDeletionDisabled` sentinel but
`response status code 405: Method Not Allowed`. No caller branched on the
sentinel, so this is message quality only.

### `review_pr4221_tokenflow_test.go` — what the token-flow pin buys

The PR pins the distribution-spec token flow with
`auth.NewCompositeTokenFetcher(..., true)` but ships no test for it; the
description calls the check a throwaway. With the pin the realm sees only
`[GET]`; without it, `[POST, GET]`. Both end up authenticated, because v3 falls
back when the OAuth2 endpoint answers 400, 401, 404 or 405. The pin saves a
round trip and avoids registries that mishandle the POST — it is not required
for correctness.

### `review_pr4221_refcache_test.go` — the snapshot format really is unchanged

The fixture is a `refs/<sha256(namespace)>.json` snapshot written verbatim by
`c1f262431a`, where the LRU key came from oras v2's
`registry.Reference.Reference`. The head reads every entry under the same key
with the same descriptor, and a tag+digest reference still resolves via the
digest.

### looseref rewrite is behaviour-preserving

Not included as a test — it was an equivalence probe over 26 reference strings
run on both commits and diffed. `GetReference()` and `String()` are identical
on both commits for every input that parses. The only difference:
`repo@v1.0.0` is now rejected, where `c1f262431a` parsed it and then silently
dropped the `@v1.0.0` from `String()`. The new behaviour is better, and the PR
documents it.

## Not covered here

- Integration tests were not run locally (they need Docker). CI's
  `Integration Tests` leg passes on this head.
- `git/*`, `testutils/git` and `oci/credentials`' docker-config tests fail in
  the review sandbox, identically on `c1f262431a`, because `~/.docker/config.json`
  and ssh-agent are not reachable there. `plugin/manager/registries/input`'s
  `TestPluginFlow` hangs for the same reason; no base control was run for it.
- `docs/adr/0022_streaming_resource_content.md` still states
  "We already depend on `oras.land/oras-go/v2`" as a live premise.
