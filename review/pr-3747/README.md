# PR #3747: FIPS review evidence

Baseline: `35a8fd76eb4f49e1156659a9d6a8b8a47a38da76`. This branch adds only
review tests, a runner, a source audit, and recorded results. No production fix.

Run from repository root:

```sh
sh review/pr-3747/run.sh
```

Requires Python 3 and the Go toolchain from `bindings/go/go.mod`; Go may need
to populate its normal module/build caches. The probe builds temporary binaries
which `testing.T.TempDir` cleans up. No real signing keys, tokens, or network
signing services are used. Existing cosign resolver tests use injected metadata
and a transport that blocks downloads; the new test reads actual build metadata.

## Evidence and interpretation

1. **Cosign guard accepts any frozen module.** The new Go test builds actual
   executables with `GOFIPS140=certified` and `GOFIPS140=v1.26.0`, then passes
   each to production `requireFIPSBuild`. Both are accepted. With Go 1.27.1,
   `certified` resolves to `v1.0.0-c2097c7c`; the explicit build uses `v1.26.0`.
   The probe is not cosign: it isolates the exact metadata predicate used by
   the resolver. It proves the predicate checks frozen-module metadata, not
   CMVP status. Existing resolver tests also accept mocked v1.26.0 metadata in
   strict mode. Suggested change: define which module versions are supported
   for regulated signing, validate against that policy, or narrow the guarantee.
   Certification status is external and must be rechecked when commenting:
   [Go documentation](https://go.dev/doc/security/fips140),
   [CMVP certificate 5247](https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/5247).

2. **Default external-tool behavior is permissive by design.** Existing
   cosign tests exercise acceptance of non-FIPS PATH binaries in `on`, an
   attempted upstream download in `on`, and rejection in `only`. Existing GPG
   tests exercise acceptance of non-FIPS libgcrypt in `on` and rejection in
   `only`. These use injected dependencies; they are not real GPG/Sigstore
   signing integration tests. The behavior is documented. The question is
   whether operator-supplied tools meet the “compliant out of the box” scope in
   [#1197](https://github.com/open-component-model/ocm-project/issues/1197) and
   the unresolved approach in [#1327](https://github.com/open-component-model/ocm-project/issues/1327).

3. **Digest documentation contradicts runtime behavior.** The new digest test
   runs with the actual process modes `on` and `only`, without overriding the
   mode detector. An MD5 reference passes `IsSafelyDigestible` in `on` and fails
   in `only`. The source audit confirms that the opening FIPS support table
   instead says SHA-256/SHA-512 is required in both modes; the later table agrees
   with the code. Suggested change: correct the opening table's default column.

4. **Version update policy differs from the issue.** Source audit confirms
   `.env` selects `certified`; real build metadata records its resolved version.
   [#1310](https://github.com/open-component-model/ocm-project/issues/1310) asks
   for a pinned version and deliberate updates. This is a requirements decision,
   not evidence of broken builds. Either pin an explicit version or agree to
   the toolchain-managed certified selector and update the issue.

5. **CLI startup status is debug-only.** Source audit confirms the FIPS startup
   statement uses `slog.DebugContext`. This observation is not a CLI end-to-end
   logging test. If #1310 requires a normally visible boot-time assertion, use
   an appropriate visible level; otherwise clarify the requirement.

6. **Strict-mode CI coverage is absent.** Source audit confirms `ci.yml` runs
   `task bindings/go:test` and contains no `fips140=only` run. The focused digest
   tests here run both modes; this does not claim the entire strict-mode suite
   passes. A unit-test matrix can provide continuing coverage.

## Claims deliberately not made

- Acceptance of metadata does not establish deployment compliance or validate
  a module. CMVP status, operating environment, and supported services matter.
- The mode probe shows that `GODEBUG` changes execution while metadata remains
  unchanged. It does **not** demonstrate that OCM launches a child in a weaker
  mode than its own; handlers pass the environment to cosign.
- Default `on` is not itself a defect. The issues request permissive production
  mode, and Go documents `only` as an assessment/debugging mode.
- No whole-suite, real GPG signing, real Sigstore signing, controller image,
  or release-pipeline compliance claim follows from these focused checks.

See `results.txt` for the captured local run.

## Local validation (2026-10-05)

- `sh review/pr-3747/run.sh`: passed; `results.txt` records the focused checks.
- `task bindings/go:test`: passed; `unit-suite.txt` records the normal-mode
  complete unit suite. This is not a complete strict-mode suite run.
- `task tools:lint`: passed, zero issues; see `lint.txt`.
- `git diff --check`: passed.

All tests ran on Go 1.27.1, darwin/arm64. The PR head was rechecked before
publication and still matched the baseline above.
