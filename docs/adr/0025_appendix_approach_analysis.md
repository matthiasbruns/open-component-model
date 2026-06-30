# ADR-0025 Appendix: External Approach Analysis

**THIS FILE WILL NOT LAND IN THE OCM REPO**

Reference for approaches evaluated in the context of [ADR-0025](0025_bindings_ci_and_release_strategy.md) and
[PR #115](https://github.com/matthiasbruns/open-component-model/pull/115). Covers external tooling, open workflow
questions, and concrete improvement candidates.

---

## 1. Kubernetes Release Process (`kubernetes/release`)

**Source:** 
- [https://github.com/kubernetes/release](https://github.com/kubernetes/release)
- [https://github.com/kubernetes/kubernetes/tree/master/staging](https://github.com/kubernetes/release)

### What they do

Kubernetes staging modules (`k8s.io/client-go`, `k8s.io/apimachinery`, etc.) live in
`staging/src/k8s.io/<module>` inside the monorepo but are published to *separate read-only mirror repositories*
via [`publishing-bot`](https://github.com/kubernetes/publishing-bot), which uses `git filter-branch` to distill
relevant commits and writes them to the target repo. Tags like `v0.29.0` are placed on those separate repos directly.

Dependency ordering is declared manually in
[`staging/publishing/rules.yaml`](https://github.com/kubernetes/kubernetes/blob/master/staging/publishing/rules.yaml)
via a `dependencies:` field per module. The publishing-bot respects this DAG to sequence publications.

`go.mod` management uses `replace` directives during development
(added by [`hack/update-vendor.sh`](https://github.com/kubernetes/kubernetes/blob/master/hack/update-vendor.sh)):

```sh
go mod edit -require k8s.io/${X}@v0.0.0
go mod edit -replace k8s.io/${X}=./staging/src/k8s.io/${X}
```

When publishing-bot publishes a module it strips the replace directives and substitutes real versioned references.
All staging modules are versioned in lockstep (every module gets the same `v0.X.0` at each k8s release).

### Verdict: not applicable structurally

K8s's separate-repo mirror approach exists because their module paths (`k8s.io/client-go`) don't match the
monorepo location. This complexity is institutional baggage irrelevant to us. Our path-scoped tags
(`bindings/go/oci/v0.1.0`) on the single monorepo are idiomatic Go and strictly simpler.

Our auto-derived dependency ordering (from `go.mod` parsing via `go mod edit -json`) is better than their
manually maintained YAML DAG. Our per-module breaking-change detection is more sophisticated than k8s's
lockstep versioning.

### Improvements worth stealing

None. Both patterns initially identified from the K8s approach were rejected on further analysis (see §5).

- **`replace` directives:** K8s uses them because their module paths don't match the monorepo location. We have
  `go.work`, which already provides the same local resolution without touching `go.mod`. Adding `replace`
  directives would be redundant during development and dangerous at publish time — `go mod edit -require` does NOT
  strip `replace` directives, so a local-path replace would reach the published `go.mod`, causing the module proxy
  to reject the module entirely.
- **`go mod edit -fmt`:** Redundant — `go mod tidy` runs immediately after each `go mod edit -require` in `pinDeps`
  and already canonicalizes `go.mod` formatting.

---

## 2. OpenTelemetry `multimod` (`open-telemetry/opentelemetry-go-build-tools`)

**Source:** [https://github.com/open-telemetry/opentelemetry-go-build-tools](https://github.com/open-telemetry/opentelemetry-go-build-tools)

### What it does

`multimod` is a CLI for releasing Go multi-module monorepos. Modules are grouped into named *module sets* in a
declarative `versions.yaml`:

```yaml
module-sets:
  stable-v1:
    version: v1.2.0
    modules:
      - go.opentelemetry.io/otel
      - go.opentelemetry.io/otel/trace
  experimental:
    version: v0.40.0
    modules:
      - go.opentelemetry.io/otel/bridge/opencensus
```

The release flow has two commands:

- **`multimod prerelease`** — bumps versions in `versions.yaml`, rewrites `require` entries in all `go.mod` files
  via **regex** (documented as a TODO; edge cases can fail silently), and commits the changes to a new branch.
- **`multimod tag`** — creates git tags for all modules in the named set at the version in `versions.yaml`.

A companion tool **`crosslink`** manages `replace` directives across the monorepo for local development (equivalent
to what K8s does with `hack/update-vendor.sh`). It performs a topological sort derived from `go.mod` parsing — the
only part of the toolchain that does what our `buildGraph` phase does.

### Key differences from our approach

| Dimension                    | `multimod`                                                                                                                            | Our `release-bindings.js`                                                                           |
|------------------------------|---------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------|
| Version source               | Declarative `versions.yaml` (operator edits manually)                                                                                 | Derived from git tags + Conventional Commits                                                        |
| Dependency ordering          | `crosslink` (separate tool, topo-sort from `go.mod`)                                                                                  | `buildGraph` (built-in, Kahn's algorithm)                                                           |
| Breaking change detection    | None — operator decides the bump                                                                                                      | Automatic via `feat!:` / `BREAKING CHANGE`                                                          |
| `go.mod` rewriting           | Regex replacement (fragile)                                                                                                           | `go mod edit -require` (toolchain-authoritative)                                                    |
| First-time modules           | Operator puts version in `versions.yaml`; tagged normally                                                                             | Falls through to pseudo-version (gap — see §3a)                                                     |
| Pseudo-version / commit pins | Not supported — uses `replace` sentinel only                                                                                          | `go get @commit` for untagged deps                                                                  |
| Human gate                   | None built in                                                                                                                         | GitHub environment approval before any tags push                                                    |
| Idempotency                  | `multimod tag` fails on already-existing tags (bug [#205](https://github.com/open-telemetry/opentelemetry-go-build-tools/issues/205)) | Explicit idempotency check in `publish` (skips if tag at HEAD, fails loudly if at different commit) |

### Open bugs (as of 2026-06)

| Issue                                                                               | Status                | Impact                                                                                                                             |
|-------------------------------------------------------------------------------------|-----------------------|------------------------------------------------------------------------------------------------------------------------------------|
| [#905](https://github.com/open-telemetry/opentelemetry-go-build-tools/issues/905)   | Open                  | `prerelease` writes `require` entries for excluded modules at non-existent proxy versions — silent breakage for external consumers |
| [#205](https://github.com/open-telemetry/opentelemetry-go-build-tools/issues/205)   | Open (stalled fix PR) | `multimod tag` is not idempotent — re-running after partial failure errors on existing tags                                        |
| [#47](https://github.com/open-telemetry/opentelemetry-go-build-tools/issues/47)     | Open                  | `commitChangesToNewBranch` crashes if the branch was checked out via `gh pr checkout` (go-git rejects merge refs)                  |
| [#1169](https://github.com/open-telemetry/opentelemetry-go-build-tools/issues/1169) | Fixed Sep 2025        | `v2+` subdir modules got double-suffixed tags (`foo/v2/v2.0.0` instead of `foo/v2.0.0`)                                            |

### Verdict: do not adopt

`multimod` requires a manually maintained `versions.yaml` — we'd trade automatic version derivation for operator
overhead on every release. Its `go.mod` rewriting is regex-based and fragile versus our `go mod edit -require`.
The non-idempotent `tag` command conflicts with our retry-safe workflow design. The open bugs affect core release
paths.

The one sub-tool worth knowing about is **`crosslink`** — it implements the same `go.mod`-derived topo-sort that
our `buildGraph` already does. No adoption needed; it confirms our approach is correct.

**One pattern to adopt:** `multimod` uses the Go sentinel pseudo-version
`v0.0.0-00010101000000-000000000000` as a marker for `replace`-directed local deps rather than real version
strings. If we add `replace` directives for local development (see §1), this is the correct placeholder version
to use alongside them.

---

## 3. Open Workflow Questions

### 3a. Can we add a new binding and use it in the same PR?

**Short answer: yes for CI, with caveats at release time.**

**CI (what works):**

- `task init/go.work` uses `find` over `go.mod` files, so the new module is picked up automatically in go.work.
- `discoverModules` uses `task go_modules` (same find-based mechanism) — new binding appears in the matrix.
- Workspace-level dependency resolution works: any binding that imports the new one resolves via go.work.

**Gap 1 — silent test exclusion:**
`splitTestModules` probes each module's Taskfile via `task -d <module> -aj`. If the new binding has no
`Taskfile.yml` with a `test` target, it is silently excluded from the CI test matrix — no warning, no error.
**Fix:** emit a warning when a `bindings/go/*` module has no `test` task, or document the requirement in
CONTRIBUTING.

**Gap 2 — no first-tag bootstrap at release:**
`planRelease` sees no previous tag → `computeNextTag` returns `null` → module goes into "pinned by commit"
path. `pinDeps` runs `GOPROXY=direct go get <module>@<headCommit>`, which produces a pseudo-version
(`v0.0.0-<timestamp>-<sha>`) in every dependent's `go.mod`. This pseudo-version is opaque to external
consumers and never automatically graduates to a real semver tag — the pipeline has no bootstrap step.

**Fix options:**

- Add a bootstrap path in `planRelease`: if a module has no previous tag and has code (not just a scaffold),
  assign it `v0.0.1` as the initial tag instead of leaving it pseudo-versioned.
- Or document that a developer must manually tag a new binding once (`bindings/go/newbinding/v0.0.1`) before
  the first bulk release, after which the pipeline takes over.

### 3b. What happens if a binding is renamed?

**Short answer: hard external break, internal CI catches missed updates loudly.**

**Internal (CI):**

- Any `go.mod` file that still references the old module path will fail the workspace build immediately — loud,
  hard error. Good.
- The new binding directory is picked up by go.work and module discovery automatically.
- **Gap:** if `Taskfile.yml` has an explicit include list for the old name (not a glob), the new binding needs
  a manual entry or `task <module>:test` won't work locally (CI still works via `task -d`).

**External consumers:**

- The old module path no longer exists. Any external `go.mod` with `require ocm.software/.../oldname` breaks
  immediately — the Go module system has no redirect mechanism for path renames.
- Old tags (`bindings/go/oldname/v0.1.0`) survive in git as dead weight; they are permanently orphaned.
- The new binding starts from zero (pseudo-version, same bootstrap gap as §3a).

**Recommendation:** treat binding renames as a hard breaking change. Document in CONTRIBUTING that renames
require: (1) updating all internal `go.mod` files, (2) adding a Taskfile.yml entry if the list is explicit,
(3) communicating the path change to external consumers, and (4) manually tagging the new path once before
the first release.

### 3c. Do we need go.work in the repo? Does it hurt?

**Decision taken in PR #115:** `go.work` is gitignored and generated in CI via `task init/go.work`.

**Rationale:**

- A committed `go.work` references all 35 modules; sparse-checkout jobs only have a subset, so Go tooling
  would fail on missing paths without a post-checkout override.
- The previous workaround was `rm go.work && task init/go.work` — the `rm` exists only because
  `task init/go.work` is a no-op when go.work already exists.
- Gitignoring eliminates the `rm` step and makes the CI job order simple: Install Task → Setup Go
  (via `.go-version`) → `task init/go.work` → build/test.

**Does it hurt locally?** Developers must run `task init/go.work` once after cloning. After that, any
`go build`/`go test` in a binding works via workspace resolution. IDEs that respect `go.work` (VS Code,
GoLand) work correctly. The file can be safely added to editor gitignore templates.

---

## 5. Critical Challenges — Findings

Adversarial review of the analysis above. Each item is a confirmed problem, not a hypothetical.

### 5a. replace directives recommendation is wrong (§1 retracted)

The analysis claimed `pinDeps` "already handles stripping" replace directives. This is factually wrong.
`go mod edit -require` updates the `require` stanza but leaves any `replace` directive untouched. A local-path
`replace => ../sibling` in a published `go.mod` causes the Go module proxy to reject the module with
`invalid module: go.mod has replace directive`. The recommendation would break publishing entirely.

Additionally, `go.work` already provides workspace-scoped module replacement — `replace` directives add nothing
for local development and introduce the proxy-rejection risk at publish time. **Recommendation dropped.**

### 5b. `go mod edit -fmt` is redundant (§1 retracted)

`go mod tidy` canonicalizes `go.mod` formatting and runs immediately after each `go mod edit -require` call in
`pinDeps`. Adding `-fmt` is dead code. **Recommendation dropped.**

### 5c. Known risks in `release-bindings.js` (§2 context)

The following risks exist in our custom solution and were not acknowledged in the original analysis:

**Silent wrong-bump via Conventional Commits discipline gap.**
`detectBump` greps commit subjects/bodies for `feat!:` or `BREAKING CHANGE`. A breaking API change committed as
`fix: remove deprecated method` produces a patch bump with no warning. The human gate cannot detect this —
it shows `v0.0.47 → v0.0.48 (patch)` with a plausible changelog. Operator-decides (multimod) has the opposite
failure mode (requires active negligence to misclassify) and is arguably safer for teams without strict commit
convention enforcement. **Mitigation:** document explicitly that the gate reviewer must verify bump kind against
semantic content, not just commit subjects.

**`GOPROXY=direct go get @commit` race window.**
For untagged modules, `pinDeps` runs `GOPROXY=direct go get <module>@<headCommit>`. This requires the pushed
commit to be immediately visible via VCS. GitHub push propagation is not instantaneous. If the release workflow
starts within seconds of the triggering push, `go get` may fail with "not found." `go mod tidy` then runs with
the system proxy, which may not have the pseudo-version cached yet. **Mitigation:** add a retry loop (3×, 10s
backoff) around the `go get @commit` calls.

**Pseudo-versions are unretractable once published.**
Once `pinDeps` commits `require foo v0.0.0-20260623-abc123def456` into a dependent's `go.mod` and that
dependent is tagged and pushed, the pseudo-version is a permanent fact in the public module graph. Go's
`retract` directive works only for semver releases. Recovery requires a full new release of the dependent.
This is an inherent limitation of the "untagged module pinned by commit" design, not a fixable bug.
**Mitigation:** document the constraint; treat it as motivation to bootstrap new modules with a `v0.0.1` tag
before the first bulk release (see §3a gap).

**`pinDeps` is not idempotent.**
The analysis criticized multimod's `tag` non-idempotency but `pinDeps` has the same class of problem.
If `pinDeps` processes 10 of 15 modules then fails, re-running starts from scratch. The already-modified
`go.mod` files may be in a partially committed state. Re-pinning the first 10 is harmless, but if `go mod tidy`
after re-pin produces a different result from the partially-committed state, the re-run is not equivalent to a
clean run. **Mitigation:** add a pre-check in `pinDeps` that detects already-pinned modules and skips them.

**Human gate does not catch version classification errors.**
The gate is effective for catching obvious operator errors (wrong module, wrong direction). It cannot detect a
misclassified bump because the reviewer sees only commit subjects, not diffs. The gate is a process control,
not a semantic correctness check. The analysis overstated its value. **Mitigation:** document scope explicitly
in the workflow comment.

### 5d. Known risks in the go.work gitignore decision (§3c)

**Stale go.work on self-hosted runners.**
`task init/go.work` uses `status: find go.work` and is a no-op if the file exists. On a reused self-hosted
runner, a stale `go.work` from a previous checkout (different branch, different module set) causes Go tooling
to silently resolve against the wrong module graph. **Fix:** prefix all CI steps that call `task init/go.work`
with `rm -f go.work go.work.sum`, or add `preconditions: - sh: "! find go.work"` instead of `status`.

**Lost workspace-membership observability.**
With a committed `go.work`, a PR that accidentally adds a non-binding module to the workspace shows a diff
and triggers review. Gitignored, there is no such signal — only `discover_modules.js`'s filtering logic.
A bug in that script silently misbehaves. **Mitigation:** add a CI step that asserts the discovered module
list matches expected patterns (all entries start with `bindings/go/`).

**Developer onboarding gap.**
`task init/go.work` is documented in `bindings/go/CONTRIBUTING.md` but not linked from the root `README.md`.
A new contributor opening the repo in VS Code gets a broken gopls workspace (no cross-module type resolution)
before they find the doc. **Fix:** add a one-liner to the root `README.md` Getting Started section.

### 5e. Known risks in the always-test-all strategy (§ADR decision)

**"Fast unit tests" assumption is partly unverified.**
`bindings/go/helm` and `bindings/go/examples` contain tests that make live HTTP calls to Helm repositories
and OCI registries. These land in the *unit* bucket because they coexist with unit tests, not because of test
speed. No timing baseline exists in the repo. **Mitigation:** audit and move network-dependent tests to the
`/integration` sub-module, or add `t.Skip` guards behind an env flag for the unit matrix.

**Flaky integration test blocks unrelated PRs.**
`check-completion` blocks merge on any failure in `run_tests`. A flaky testcontainer timeout in
`bindings/go/sigstore/integration` blocks a PR that only touched `bindings/go/runtime`. There is no per-module
retry or per-module `continue-on-error`. **Mitigation:** consider per-job `timeout-minutes` and
`continue-on-error: true` on integration jobs with a separate non-blocking status check for integration results.

**No path filtering for docs-only PRs.**
`ci.yml` triggers on `pull_request` with no `paths-ignore` filter. A PR touching only `docs/*.md` or
`website/` runs the full 31-module lint and test matrix (~26 minutes wall-clock at 5 parallel jobs).
**Mitigation:** add `paths-ignore: ['docs/**', '*.md', 'website/**']` to the `pull_request` trigger, with the
`check-completion` required-status check bypassed for excluded paths via a GitHub branch protection allowance.

---

## 4. Summary Table

| Question / Approach                        | Verdict                                                                           | Action                                                                       |
|--------------------------------------------|-----------------------------------------------------------------------------------|------------------------------------------------------------------------------|
| Kubernetes publishing-bot / separate repos | Not applicable                                                                    | None                                                                         |
| K8s `replace` directives                   | ~~Worth adopting~~ **Retracted** — breaks proxy publish; redundant with `go.work` | None                                                                         |
| K8s `go mod edit -fmt`                     | ~~Worth adopting~~ **Retracted** — redundant; `go mod tidy` already canonicalizes | None                                                                         |
| OpenTelemetry `multimod`                   | Do not adopt — regex go.mod rewriting, non-idempotent tag, manual versions.yaml   | `crosslink` confirms our topo-sort approach; no adoption needed              |
| Add new binding in same PR                 | Works with caveats                                                                | Bootstrap gap: document manual first-tag; warn on missing Taskfile test task |
| Rename binding                             | Hard external break                                                               | Document in CONTRIBUTING; no tooling change                                  |
| Commit go.work                             | Decided: gitignore it                                                             | Done in PR #115; add `rm -f go.work` prefix for self-hosted runner safety    |
| `detectBump` wrong-bump risk               | Known limitation                                                                  | Document gate reviewer responsibility; consider version bump override input  |
| `GOPROXY=direct` race window               | Known bug                                                                         | Add retry loop around `go get @commit` calls in `pinDeps`                    |
| `pinDeps` not idempotent                   | Known gap                                                                         | Add pre-check to skip already-pinned modules on re-run                       |
| Flaky integration test blast radius        | Known risk                                                                        | Per-job `timeout-minutes`; consider non-blocking integration status          |
| Docs-only PR runs full matrix              | Known waste                                                                       | Add `paths-ignore` on `pull_request` trigger in `ci.yml`                     |
| Stale go.work on self-hosted runners       | Latent correctness bug                                                            | `rm -f go.work go.work.sum` before `task init/go.work` in all CI jobs        |
| Developer onboarding (no root README link) | Friction trap                                                                     | Add `task init/go.work` mention to root `README.md`                          |
