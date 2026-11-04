# PR 4147 review evidence — `feat(config): add configurable RemotePolicy`

- PR: https://github.com/open-component-model/open-component-model/pull/4147
- Reviewed head: `fc2336733bf222a1519b02e2b14403b1db13d588`
- Merge base: `ec7fd61d4f696663fb46338f3b7e0a16ea441a58`
- Linked issue: https://github.com/open-component-model/ocm-project/issues/1400 (no parent epic)
- Evidence branch: https://github.com/matthiasbruns/open-component-model/tree/review/pr-4147-config-decode-leniency

## Verdict

Nothing blocks the PR. CI is green on `fc2336733b` (27 success, 3 skipped). The feature
works end to end: Helm value -> ConfigMap -> `--ocm-config` -> blob cache with `Always`.
Scope matches issue 1400, except that "Integration Test Suite updated" is only partially
met — `controller_integration_test.go` gained a `nil` argument but no test exercises the
new path.

The one finding below is a hardening gap that follows the existing house convention, so it
is recorded here for a separate framework issue rather than as a blocker.

## M1 — misspelled `remotePolicy` is dropped silently

- Location: https://github.com/open-component-model/open-component-model/blob/fc2336733bf222a1519b02e2b14403b1db13d588/bindings/go/oci/spec/config/cache/v1alpha1/config.go#L85
- Category: input validation / silent fallback on a security-relevant toggle
- Status: **reproduced on head**; origin **new-code following a pre-existing convention**

`LookupConfig` decodes each entry with `Scheme.Convert`, which silently drops any field
`Config` does not declare. A misspelled `remotePolicy` key is dropped, `LookupConfig`
returns no error, and the effective policy falls back to the permissive `IfNotPresent`
default with nothing in the logs.

```yaml
configurations:
  - type: oci.cache.config.ocm.software/v1alpha1
    remote-policy: Always   # dropped -> IfNotPresent
```

`encoding/json` still matches `remotepolicy` case-insensitively, so only some
misspellings bite:

```
remotePolicy  -> Always          remote-policy -> ""   (IfNotPresent)
remotepolicy  -> Always          remotePolicly -> ""   (IfNotPresent)
                                 remote_policy -> ""   (IfNotPresent)
                                 policy        -> ""   (IfNotPresent)
```

An empty `RemotePolicy` is defaulted to `IfNotPresent` by `cache.Options.applyDefaults`,
so a dropped field is a silent downgrade rather than a no-op. The test asserts that
precondition on `cache.Defaults().RemotePolicy` instead of taking it on trust.

### Why it matters here

`Always` is the only mitigation the new documentation points operators at for
cross-namespace blob cache sharing, and nothing else in the stack catches the typo:

- the chart types `manager.ocmConfig` as a bare `{"type": "object"}` in `values.schema.json`;
- the generated `Config.schema.json` carries `additionalProperties: false`, but it is
  never enforced at runtime — it is documentation and IDE tooling only;
- neither the CLI nor the controller logs the effective policy.

### Why it is not this PR's defect

Eight of the nine OCM config types decode this way. `transfer.config.ocm.software` is the
only one using `runtime.DecodeStrict`:

| Config type | Decode |
|---|---|
| `cli/internal/plugin/spec/config/v2alpha1` | `Scheme.Convert` |
| `cli/internal/versioncheck` | `Scheme.Convert` |
| `configuration/checksum/http/v1alpha1` | `Scheme.Convert` |
| `configuration/extract/v1alpha1` | `Scheme.Convert` |
| `configuration/filesystem/v1alpha1` | `Scheme.Convert` |
| `http/spec/config/v1alpha1` | `Scheme.Convert` |
| `oci/spec/config/cache/v1alpha1` (this PR) | `Scheme.Convert` |
| `signing/v1alpha1` | `Scheme.Convert` |
| `transfer/v1alpha1` | **`runtime.DecodeStrict`** |

Precedent: https://github.com/open-component-model/open-component-model/blob/fc2336733bf222a1519b02e2b14403b1db13d588/bindings/go/transfer/v1alpha1/spec/config.go#L99

So the general fix belongs in its own issue against the config framework — the same bucket
as the nil-entry validation already deferred on this PR.

### Candidate fix

One line at `config.go:85`:

```go
if err := runtime.DecodeStrict(entry, &config); err != nil {
```

## Reproduction

Test: https://github.com/matthiasbruns/open-component-model/blob/ef6672a081509dee2fe39be8f54b4813976e72b9/bindings/go/oci/spec/config/cache/v1alpha1/review_pr4147_test.go

```sh
git fetch https://github.com/matthiasbruns/open-component-model.git \
  review/pr-4147-config-decode-leniency && git checkout FETCH_HEAD
cd bindings/go && go test ./oci/spec/config/cache/v1alpha1/ -run TestReviewPR4147 -v
```

| Test | On head `fc2336733b` | With the `DecodeStrict` fix |
|---|---|---|
| `TestReviewPR4147_M1_MisspelledRemotePolicyMustNotBeSilentlyDropped` | FAIL (4 of 6 rows) | PASS |
| `TestReviewPR4147_UnknownFieldMustNotBeSilentlyDropped` | FAIL | PASS |
| `TestReviewPR4147_ConfigDecodeLeniencyAcrossTypes` | PASS | PASS |

Verified in both directions. With the fix the whole package is green, including the PR's
own `TestLookupConfig`, so the test is cherry-pickable as-is. The fix was reverted before
committing: this branch adds only the test file.

The third test records which sibling types are lenient today. The `oci.cache` row is
deliberately excluded from it, so it stays green after a fix instead of inverting into a
false failure.

## Non-blocking leftovers

1. The effective policy is never logged, in either binary. The CLI logs the resolved HTTP
   config at debug in `cli/cmd/setup/setup.go:90`; the cache lookup nine lines later logs
   nothing, and `kubernetes/controller/cmd/main.go:224` only logs on failure. One
   `setupLog.Info` would make M1 self-diagnosing.
2. No integration test covers the new path (see Verdict).

## Checked and correct — do not re-raise

- `since: "0.24"` in `website/scripts/register-docs-version.js` is right. `releases/v0.23`
  was branched on 2026-10-30, before this PR, so the change ships in 0.24. Driving
  `buildModuleBlocks` directly confirms `0.23` gets no cache-schema mount and `0.24` does;
  harmless, because 0.23's docs content comes from the same tag and carries no
  `schema-renderer` shortcode.
- The docs correction on the controller default is a genuine bug fix: the old table claimed
  `Always`, while the merge base hardcoded `IfNotPresent` in both the CLI and the controller.
- `NewBlobCache(dir, nil)` is behaviour-identical to the merge base — the empty
  `RemotePolicy` is defaulted to `IfNotPresent` by `Options.applyDefaults`.
- Documented limits (`100000` / `10000` / `20 MiB`), "the controller does not start if the
  cache dir fails", per-entry `Validate` before merge, `LoadStartupConfig`'s named return
  plus `errors.Join(err, file.Close())` (mirrors `cli/cmd/configuration.GetConfigFromPath`),
  `FilterWithRemainder` nil-safety and the ConfigMap name truncation helper all verified.

## Validation limits

- Local `golangci-lint` predates the go1.27 toolchain and fails on unrelated stdlib
  typecheck errors; CI's `golangci-lint (bindings/go)` passed on the head.
- Integration tests (testcontainers) and the Netlify deploy preview were not run locally.
- `go vet` clean. All five affected Go packages pass, as do all 72 website script tests
  (`node --test scripts/register-docs-version.test.js`, with `js-yaml` stubbed since
  `node_modules` is not installed).
