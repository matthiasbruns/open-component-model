// @ts-check
// Binding release orchestration — one file, named exports per workflow step.
//
// Workflow calls each export in sequence:
//   buildGraph    → discover modules, topo-sort by level, output ordered_json + groups_json
//   planRelease   → compute next semver tag per module, output tags_json
//   releaseGroups → for each dependency level: pin go.mod, go mod tidy, commit,
//                   tag + push, wait for proxy, then repeat for the next level.
//                   Consumers (cli, controller) are pinned + tidied in a final commit.

import {execFileSync} from 'child_process';
import {existsSync, writeFileSync} from 'fs';
import {join} from 'path';
import {tagPrefix, latestTag, bumpVersion} from './submodule-version.js'; // eslint-disable-line -- tagPrefix used in computeNextTag below

const OCM_PREFIX = 'ocm.software/open-component-model/';

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/** @param {string[]} args */
function git(args) {
    const token = process.env.GITHUB_TOKEN;
    const auth = token
        ? ['-c', `http.extraHeader=Authorization: basic ${Buffer.from(`x-access-token:${token}`).toString('base64')}`]
        : [];
    return /** @type {string} */ (execFileSync('git', [...auth, ...args], {encoding: 'utf-8', stdio: 'pipe'})).trim();
}

/**
 * Runs commands with the go toolchain.
 * This function should be used instead if regex to handle go mod dependencies.
 *
 * @param {string[]} args
 * @param {import('child_process').ExecFileSyncOptions} [opts]
 */
function go_(args, opts = {}) {
    execFileSync('go', args, {stdio: 'inherit', ...opts});
}

// ---------------------------------------------------------------------------
// Pure / injectable functions (exported for testing)
// ---------------------------------------------------------------------------

/**
 * Return repo-relative binding module paths from go.work.
 * Uses `go work edit -json` for authoritative parsing — no regex.
 * Excludes /integration test modules.
 *
 * @param {string} repoRoot
 * @returns {string[]}
 */
export function discoverModules(repoRoot) {
    const raw = /** @type {string} */ (execFileSync(
        'go', ['work', 'edit', '-json'],
        {encoding: 'utf-8', stdio: 'pipe', cwd: repoRoot},
    ));
    const {Use = []} = /** @type {{Use?: {DiskPath: string}[]}} */ (JSON.parse(raw));
    return Use
        .map(u => u.DiskPath.replace(/^\.\//, ''))
        .filter(p => p.startsWith('bindings/go/') && !p.endsWith('/integration'));
}

/**
 * Return the internal binding deps of a module as repo-relative paths.
 * Uses `go mod edit -json` so all go.mod syntax (replace directives,
 * indirect markers, multi-line blocks) is handled correctly by the Go
 * toolchain itself rather than a regex.
 *
 * OpenTelemetry's crosslink solves the same problem (go.mod-derived topo-sort)
 * but via regex parsing, which silently mishandles edge cases. Our approach was
 * validated against crosslink in ADR-0025 (§ External Alternatives Evaluated):
 * https://github.com/open-telemetry/opentelemetry-go-build-tools/tree/main/crosslink
 *
 * @param {string} repoRoot
 * @param {string} modPath
 * @returns {string[]}
 */
export function internalDepsOf(repoRoot, modPath) {
    const raw = /** @type {string} */ (execFileSync(
        'go', ['mod', 'edit', '-json'],
        {encoding: 'utf-8', stdio: 'pipe', cwd: join(repoRoot, modPath)},
    ));
    const {Require = []} = /** @type {{Require?: {Path: string}[]}} */ (JSON.parse(raw));
    return Require
        .map(r => r.Path)
        .filter(p => p.startsWith(`${OCM_PREFIX}bindings/go/`))
        .map(p => p.replace(OCM_PREFIX, ''));
}

/**
 * Topological sort (Kahn's algorithm). Deps precede their dependents.
 * Throws if a cycle is detected.
 *
 * @param {string[]} modules
 * @param {Map<string, string[]>} depsMap  module → its direct internal deps
 * @returns {string[]}
 */
export function topoSort(modules, depsMap) {
    // How many unprocessed deps each module is still waiting on.
    /** @type {Map<string, number>} */
    const pendingDepsCount = new Map(modules.map(module => [module, 0]));

    // For each module: which other modules depend on it (need it released first).
    /** @type {Map<string, string[]>} */
    const dependents = new Map(modules.map(module => [module, []]));

    for (const [module, deps] of depsMap) {
        for (const dep of deps) {
            if (!pendingDepsCount.has(dep)) continue; // dep not in our release set
            pendingDepsCount.set(module, (pendingDepsCount.get(module) ?? 0) + 1);
            dependents.get(dep)?.push(module);
        }
    }

    // Start with modules that have no deps to wait on.
    const modulesWithoutDeps = modules.filter(module => pendingDepsCount.get(module) === 0);
    /** @type {string[]} */
    const sorted = [];

    while (modulesWithoutDeps.length) {
        const released = modulesWithoutDeps.shift() ?? '';
        sorted.push(released);

        // Each module that was waiting on this one has one fewer pending dep.
        for (const dependent of (dependents.get(released) ?? [])) {
            const remaining = (pendingDepsCount.get(dependent) ?? 0) - 1;
            pendingDepsCount.set(dependent, remaining);
            if (remaining === 0) modulesWithoutDeps.push(dependent);
        }
    }

    if (sorted.length !== modules.length) {
        const stuck = modules.filter(module => !sorted.includes(module));
        throw new Error(`Cycle detected: ${stuck.join(', ')}`);
    }
    return sorted;
}

/**
 * Group modules into dependency levels for batched sequential release.
 *
 * Modules in the same level share no intra-level dependencies, so they can
 * be pinned, tidied, committed, and tagged together. All dependencies of
 * level N are in levels 0..N-1 and will already have been pushed to the
 * proxy before level N begins.
 *
 * @param {string[]} ordered  modules in topological order (deps before dependents)
 * @param {Map<string, string[]>} depsMap  module → its direct internal deps
 * @returns {string[][]}  array of groups; group[0] has no internal deps
 */
export function groupByLevel(ordered, depsMap) {
    /** @type {Map<string, number>} */
    const levels = new Map();

    for (const mod of ordered) {
        const deps = (depsMap.get(mod) ?? []).filter(d => levels.has(d));
        const level = deps.length === 0 ? 0 : Math.max(...deps.map(d => levels.get(d) ?? 0)) + 1;
        levels.set(mod, level);
    }

    /** @type {string[][]} */
    const groups = [];
    for (const mod of ordered) {
        const level = levels.get(mod) ?? 0;
        while (groups.length <= level) groups.push([]);
        groups[level].push(mod);
    }
    return groups;
}

// bumpVersion and latestTag imported from submodule-version.js (shared with
// release-go-submodule.yaml). Re-export bumpVersion for test coverage.
export {bumpVersion} from './submodule-version.js';

/**
 * Extract a bare semver version from a path-scoped module tag.
 * "bindings/go/oci/v0.0.47" → "v0.0.47"
 *
 * @param {string} tag
 * @returns {string}
 */
export function tagToVersion(tag) {
    return `v${tag.split('/v').at(-1)}`;
}

// Alias so existing call-sites in this file and tests still compile.
export {latestTag as latestVersionTag} from './submodule-version.js';

/**
 * Return true if a module has any commits touching its path since its last tag.
 * Always returns true for modules with no previous tag (first release).
 *
 * @param {string} modPath
 * @param {string|null} lastTag
 * @param {(args: string[]) => string} [execGit]
 * @returns {boolean}
 */
export function hasChanges(modPath, lastTag, execGit = git) {
    if (!lastTag) return true;
    try {
        return execGit(['log', `${lastTag}..HEAD`, '--oneline', '--', modPath]).trim() !== '';
    } catch {
        return true; // safe default
    }
}

/**
 * Scan git log for a module since its last tag for breaking change markers
 * (Conventional Commits: `type!:` subject or `BREAKING CHANGE:` footer).
 * Returns 'minor' if any are found, 'patch' otherwise.
 * Returns 'patch' immediately when lastTag is null — untagged modules use a
 * pseudo-version regardless of bump kind.
 *
 * @param {string} modPath
 * @param {string|null} lastTag
 * @param {(args: string[]) => string} [execGit]
 * @returns {'minor'|'patch'}
 */
export function detectBump(modPath, lastTag, execGit = git) {
    if (!lastTag) return 'patch';
    try {
        const log = execGit(['log', `${lastTag}..HEAD`, '--', modPath, '--format=%s%n%b']);
        return /(!:|BREAKING[- ]CHANGE)/i.test(log) ? 'minor' : 'patch';
    } catch {
        return 'patch';
    }
}

/**
 * Compute the next semver tag for an already-tagged module.
 * Returns null for modules with no existing tag — they must be released separately first.
 *
 * @param {string} modPath
 * @param {'patch'|'minor'} bumpKind
 * @param {(args: string[]) => string} [execGit]
 * @returns {string|null}
 */
export function computeNextTag(modPath, bumpKind, execGit = git) {
    const prefix = tagPrefix(modPath);
    const latest = latestTag(modPath, execGit);
    if (!latest) return null;
    return `${prefix}${bumpVersion(latest.replace(prefix, ''), bumpKind)}`;
}

/**
 * Compute which internal deps need pinning for each module being released.
 *
 * @param {string[]} orderedModules
 * @param {Record<string, string>} tags  module path → new tag
 * @param {(mod: string) => string[]} getDeps
 * @returns {Map<string, Array<{name: string, version: string}>>}
 */
export function pinsFor(orderedModules, tags, getDeps) {
    /** @type {Map<string, Array<{name: string, version: string}>>} */
    const result = new Map();
    for (const mod of orderedModules) {
        const pins = getDeps(mod)
            .filter(dep => dep in tags)
            .map(dep => ({name: `${OCM_PREFIX}${dep}`, version: tagToVersion(tags[dep])}));
        if (pins.length) result.set(mod, pins);
    }
    return result;
}

// ---------------------------------------------------------------------------
// Workflow step entrypoints
// ---------------------------------------------------------------------------

/**
 * Step 1 — Discover modules, topo-sort, output ordered_json.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function buildGraph({core}) {
    const repoRoot = process.env.GITHUB_WORKSPACE ?? process.cwd();
    const modules = discoverModules(repoRoot);
    const moduleSet = new Set(modules);
    const depsMap = new Map(modules.map(m => [m, internalDepsOf(repoRoot, m).filter(d => moduleSet.has(d))]));
    const ordered = topoSort(modules, depsMap);
    const groups  = groupByLevel(ordered, depsMap);

    core.info(`Discovered ${ordered.length} binding modules in ${groups.length} dependency level(s):`);
    groups.forEach((group, i) => {
        core.info(`  Level ${i}:`);
        for (const m of group) {
            const deps = depsMap.get(m) ?? [];
            core.info(`    ${m}${deps.length ? ` ← ${deps.join(', ')}` : ''}`);
        }
    });

    core.setOutput('ordered_json', JSON.stringify(ordered));
    core.setOutput('groups_json',  JSON.stringify(groups));
}

/**
 * Step 2 — Compute the next semver tag for every module, output tags_json.
 * Reads ORDERED_JSON and BUMP from the environment.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function planRelease({core}) {
    const repoRoot = process.env.GITHUB_WORKSPACE ?? process.cwd();
    const ordered  = /** @type {string[]} */ (JSON.parse(process.env.ORDERED_JSON ?? '[]'));

    /** @type {Record<string, string>} */
    const tags = {};
    /** @type {Record<string, string>} module → previous tag (for from→to display in gate) */
    const fromTags = {};
    /** @type {Record<string, string>} module → detected bump kind (for gate summary) */
    const bumpKinds = {};
    /** @type {string[]} modules that changed and need testing + pinning (tagged or untagged) */
    const changed = [];
    /** @type {string[]} modules skipped because nothing changed since last release */
    const skipped = [];

    core.info('Planned tags:');
    for (const mod of ordered) {
        const currentTag = latestTag(mod);

        if (!hasChanges(mod, currentTag)) {
            core.info(`  ${mod} → (unchanged since ${currentTag ?? 'untagged'}, skipping)`);
            skipped.push(mod);
            continue;
        }

        changed.push(mod);

        const bump = detectBump(mod, currentTag);
        const nextTag = computeNextTag(mod, bump);

        if (!nextTag) {
            core.info(`  ${mod} → (no prior tag; skipped — module must be released separately first)`);
            continue;
        }

        tags[mod]      = nextTag;
        fromTags[mod]  = currentTag ?? '(none)';
        bumpKinds[mod] = bump;

        core.info(`  ${mod} → ${nextTag}${bump === 'minor' ? '  ⚠ breaking change' : ''}`);
    }

    if (skipped.length) core.info(`\nSkipped ${skipped.length} unchanged module(s)`);

    // Changelogs for tagged modules only — same format as release-go-submodule.yaml:
    // git log --pretty=format:"- %s (%h)" <fromTag>..HEAD -- <modPath>
    /** @type {Record<string, string>} */
    const changelogs = {};
    for (const mod of Object.keys(tags)) {
        const from = fromTags[mod];
        try {
            const log = git(['log', `${from}..HEAD`, '--', mod, '--pretty=format:- %s (%h)']);
            changelogs[mod] = log || 'No changes';
        } catch {
            changelogs[mod] = 'Unable to generate changelog';
        }
    }

    // For each changed module that has a sibling integration/ sub-module, include it
    // in the integration test matrix — mirrors how ci.yml discovers integration modules.
    const integrationModules = changed
        .filter(mod => existsSync(join(repoRoot, mod, 'integration', 'Taskfile.yml')))
        .map(mod => `${mod}/integration`);

    // Compute the Go pseudo-version for untagged modules so the gate can display it.
    const headCommit = git(['rev-parse', 'HEAD']);
    const tsRaw      = git(['log', '-1', '--format=%ct', 'HEAD']);
    const date       = new Date(Number(tsRaw) * 1000).toISOString().replace(/[^0-9]/g, '').slice(0, 14);
    const pseudoVer  = `v0.0.0-${date}-${headCommit.slice(0, 12)}`;

    // Write changelogs to a file for artifact upload — too large to pass as a job output.
    const artifactDir = process.env.RUNNER_TEMP ?? '/tmp';
    writeFileSync(join(artifactDir, 'changelogs.json'), JSON.stringify(changelogs, null, 2));
    core.info(`Changelogs written to ${artifactDir}/changelogs.json`);

    core.setOutput('tags_json',                JSON.stringify(tags));
    core.setOutput('from_tags_json',           JSON.stringify(fromTags));
    core.setOutput('bump_kinds_json',          JSON.stringify(bumpKinds));
    core.setOutput('changed_modules_json',     JSON.stringify(changed));
    core.setOutput('skipped_modules_json',     JSON.stringify(skipped));
    core.setOutput('integration_modules_json', JSON.stringify(integrationModules));
    core.setOutput('head_commit',              headCommit);
    core.setOutput('pseudo_version',           pseudoVer);
    core.setOutput('artifact_dir',             artifactDir);
}

/**
 * Compute the version pins for all modules (bindings + consumers).
 *
 * For each module's internal binding deps:
 *   - released this run (in tags)         → pin to the new tag
 *   - skipped/unchanged (latestTag ≠ null) → pin to the current latest tag
 *   - new module with no tag yet           → skip (no pin possible)
 *   - external dep (not a known binding)   → ignored
 *
 * @param {string[]} ordered  binding module paths in topological order (deps before dependents)
 * @param {Record<string, string>} tags  module → new tag being released
 * @param {string[]} consumers
 * @param {(mod: string) => string[]} getDeps
 * @param {(mod: string) => string|null} getLatestTag
 * @returns {Map<string, Array<{name: string, version: string}>>}
 */
export function resolvePins(ordered, tags, consumers, getDeps, getLatestTag) {
    const taggedSet  = new Set(Object.keys(tags));
    const orderedSet = new Set(ordered);
    /** @type {Map<string, Array<{name: string, version: string}>>} */
    const result = new Map();

    for (const mod of [...ordered, ...consumers]) {
        const pins = [];
        for (const dep of getDeps(mod)) {
            if (!orderedSet.has(dep)) continue; // external dep — ignore
            const tag = taggedSet.has(dep) ? tags[dep] : getLatestTag(dep);
            if (!tag) continue; // new untagged module — skip
            pins.push({name: `${OCM_PREFIX}${dep}`, version: tagToVersion(tag)});
        }
        if (pins.length) result.set(mod, pins);
    }
    return result;
}

/**
 * Step 3 — Pin newly-released internal deps in each module's go.mod, then tidy.
 * Reads ORDERED_JSON and TAGS_JSON from the environment.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function pinDeps({core}) {
    const repoRoot  = process.env.GITHUB_WORKSPACE ?? process.cwd();
    const ordered   = /** @type {string[]} */ (JSON.parse(process.env.ORDERED_JSON ?? '[]'));
    const consumers = /** @type {string[]} */ (JSON.parse(process.env.CONSUMERS_JSON ?? '[]'));
    const tags      = /** @type {Record<string, string>} */ (JSON.parse(process.env.TAGS_JSON ?? '{}'));
    const dryRun    = process.env.DRY_RUN === 'true';

    const pins = resolvePins(
        ordered, tags, consumers,
        mod => internalDepsOf(repoRoot, mod),
        latestTag,
    );

    for (const [mod, modPins] of pins) {
        core.info(`\nPinning deps in ${mod}:`);
        const modDir = join(repoRoot, mod);
        for (const {name, version} of modPins) {
            core.info(`  ${dryRun ? '[dry-run] ' : ''}${name}@${version}`);
            if (!dryRun) go_(['mod', 'edit', `-require=${name}@${version}`], {cwd: modDir});
        }
    }
}

/**
 * Step 4 — Create signed git tags and push everything.
 * Reads TAGS_JSON and DRY_RUN from the environment.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function publish({core}) {
    const tags = /** @type {Record<string, string>} */ (JSON.parse(process.env.TAGS_JSON ?? '{}'));
    const dryRun = process.env.DRY_RUN === 'true';
    const entries = Object.entries(tags);

    const head = git(['rev-parse', 'HEAD']);

    core.info('Creating tags:');
    for (const [, tag] of entries) {
        if (dryRun) {
            core.info(`  [dry-run] ${tag}`);
            continue;
        }

        // Idempotent: if the tag already exists at HEAD, skip silently.
        // If it exists at a different commit, fail loudly — that's a real conflict.
        try {
            const existing = git(['rev-parse', `refs/tags/${tag}^{commit}`]);
            if (existing === head) {
                core.info(`  ${tag} (already exists at HEAD, skipping)`);
                continue;
            }
            core.setFailed(`Tag ${tag} already exists but points to ${existing.slice(0, 7)}, not HEAD ${head.slice(0, 7)}`);
            return;
        } catch {
            // tag does not exist yet — create it
        }

        git(['tag', '-a', '-m', `Release ${tag}`, tag]);
        core.info(`  ${tag}`);
    }

    if (!dryRun) {
        git(['push', 'origin', 'HEAD']);
        git(['push', 'origin', ...entries.map(([, t]) => `refs/tags/${t}`)]);
        core.info(`\nPushed ${entries.length} tags`);
    }

    const heading = dryRun ? '🔍 Binding Release Plan (dry-run)' : '✅ Binding Release Complete';
    const rows = entries.map(([mod, tag]) => `| \`${mod}\` | \`${tag}\` |`).join('\n');
    await core.summary
        .addHeading(heading)
        .addRaw(`| Module | Tag |\n| :--- | :--- |\n${rows}\n`)
        .write();

    core.setOutput('tags_json', JSON.stringify(tags));
}

// ---------------------------------------------------------------------------
// Internal: proxy readiness probe
// ---------------------------------------------------------------------------

/**
 * Poll `go list -m module@version` until the proxy serves the version.
 * Needed between level pushes so the next level's `go mod tidy` can fetch
 * the checksums for the just-published tags.
 *
 * @param {string} modPath  repo-relative path, e.g. "bindings/go/runtime"
 * @param {string} version  semver, e.g. "v0.0.9"
 * @param {import('@actions/core')} core
 * @param {number} [retries]
 * @param {number} [delayMs]
 */
async function waitForProxy(modPath, version, core, retries = 12, delayMs = 5000) {
    const name = `${OCM_PREFIX}${modPath}`;
    for (let i = 0; i < retries; i++) {
        try {
            go_(['list', '-m', `${name}@${version}`]);
            core.info(`  proxy ready: ${name}@${version}`);
            return;
        } catch {
            core.info(`  waiting for proxy (${i + 1}/${retries}): ${name}@${version}`);
            await new Promise(r => setTimeout(r, delayMs));
        }
    }
    throw new Error(`Proxy did not index ${name}@${version} after ${retries} attempts`);
}

// ---------------------------------------------------------------------------
// Internal: check for staged changes
// ---------------------------------------------------------------------------

function hasStagedChanges() {
    try { git(['diff', '--staged', '--quiet']); return false; } catch { return true; }
}

// ---------------------------------------------------------------------------
// Step 3+4 — Sequential group-based release
// ---------------------------------------------------------------------------

/**
 * @typedef {{
 *   pin:   (name: string, version: string, modDir: string) => void,
 *   tidy:  (modDir: string) => void,
 *   add:   (files: string[]) => void,
 *   commit:(msg: string) => void,
 *   tag:   (tag: string) => void,
 *   push:  (refs: string[]) => void,
 *   wait:  (mod: string, version: string) => Promise<void>,
 * }} ReleaseOps
 */

/**
 * Core release loop — separated from env/IO so it can be unit tested.
 *
 * For each dependency level with changed modules:
 *   pin → tidy → commit → tag → push → wait for proxy
 * Then consumers: pin → tidy → commit → push
 *
 * @param {string[][]} groups     modules grouped by dependency level
 * @param {Record<string, string>} tags    module → new tag
 * @param {string[]} consumers    consumer module paths (cli, controller)
 * @param {Map<string, Array<{name: string, version: string}>>} allPins  pre-resolved pins
 * @param {string} repoRoot
 * @param {ReleaseOps} ops        injectable side-effects for testing
 * @returns {Promise<void>}
 */
export async function executeRelease(groups, tags, consumers, allPins, repoRoot, ops) {
    const taggedSet = new Set(Object.keys(tags));

    // ── Binding levels ───────────────────────────────────────────────────────
    for (const [level, group] of groups.entries()) {
        const changed = group.filter(mod => taggedSet.has(mod));
        if (!changed.length) continue;

        // 1. Pin go.mod
        for (const mod of changed) {
            const modDir = join(repoRoot, mod);
            for (const {name, version} of (allPins.get(mod) ?? [])) {
                ops.pin(name, version, modDir);
            }
        }

        // 2. go mod tidy — proxy has tags from all previous levels
        for (const mod of changed) {
            ops.tidy(join(repoRoot, mod));
        }

        // 3. Commit go.mod + go.sum for this level
        ops.add(changed.flatMap(m => [`${m}/go.mod`, `${m}/go.sum`]));
        const names = changed.map(m => tags[m]).join(', ');
        ops.commit(`release: level ${level} — ${names}`);

        // 4. Create and push all tags for this level at once
        const levelTags = changed.map(m => tags[m]);
        for (const tag of levelTags) ops.tag(tag);
        ops.push(['HEAD', ...levelTags.map(t => `refs/tags/${t}`)]);

        // 5. Wait for proxy to index before the next level runs tidy
        for (const mod of changed) {
            await ops.wait(mod, tagToVersion(tags[mod]));
        }
    }

    // ── Consumers ────────────────────────────────────────────────────────────
    const consumerPinned = consumers.filter(c => (allPins.get(c) ?? []).length > 0);
    if (consumerPinned.length) {
        for (const consumer of consumerPinned) {
            const consumerDir = join(repoRoot, consumer);
            for (const {name, version} of (allPins.get(consumer) ?? [])) {
                ops.pin(name, version, consumerDir);
            }
            ops.tidy(consumerDir);
        }
        ops.add(consumerPinned.flatMap(c => [`${c}/go.mod`, `${c}/go.sum`]));
        ops.commit('release: pin and tidy consumers');
        ops.push(['HEAD']);
    }
}

/**
 * Reads: GROUPS_JSON, CONSUMERS_JSON, TAGS_JSON, DRY_RUN
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function releaseGroups({core}) {
    const repoRoot  = process.env.GITHUB_WORKSPACE ?? process.cwd();
    const groups    = /** @type {string[][]} */ (JSON.parse(process.env.GROUPS_JSON  ?? '[]'));
    const consumers = /** @type {string[]}   */ (JSON.parse(process.env.CONSUMERS_JSON ?? '[]'));
    const tags      = /** @type {Record<string, string>} */ (JSON.parse(process.env.TAGS_JSON ?? '{}'));
    const dryRun    = process.env.DRY_RUN === 'true';

    const allModules = groups.flat();
    const allPins = resolvePins(
        allModules, tags, consumers,
        mod => internalDepsOf(repoRoot, mod),
        latestTag,
    );

    /** @type {ReleaseOps} */
    const ops = {
        pin:    (name, version, cwd) => { core.info(`  pin ${name}@${version}`); if (!dryRun) go_(['mod', 'edit', `-require=${name}@${version}`], {cwd}); },
        tidy:   (cwd) => { core.info(`  tidy ${cwd}`); if (!dryRun) go_(['mod', 'tidy'], {cwd}); },
        add:    (files) => { if (!dryRun) git(['add', ...files]); },
        commit: (msg) => { if (!dryRun && hasStagedChanges()) git(['commit', '-s', '-m', msg]); },
        tag:    (tag) => { core.info(`  ${dryRun ? '[dry-run] ' : ''}tag ${tag}`); if (!dryRun) git(['tag', '-a', '-m', `Release ${tag}`, tag]); },
        push:   (refs) => { if (!dryRun) git(['push', 'origin', ...refs]); },
        wait:   async (mod, version) => { if (!dryRun) await waitForProxy(mod, version, core); },
    };

    await executeRelease(groups, tags, consumers, allPins, repoRoot, ops);

    // ── Summary ──────────────────────────────────────────────────────────────
    const entries = Object.entries(tags);
    const heading = dryRun ? '🔍 Binding Release Plan (dry-run)' : '✅ Binding Release Complete';
    const rows = entries.map(([mod, tag]) => `| \`${mod}\` | \`${tag}\` |`).join('\n');
    await core.summary
        .addHeading(heading)
        .addRaw(`| Module | Tag |\n| :--- | :--- |\n${rows}\n`)
        .write();

    core.setOutput('tags_json', JSON.stringify(tags));
}
