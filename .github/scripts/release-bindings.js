// @ts-check
// Binding release orchestration — one file, named exports per workflow step.
//
// Workflow calls each export in sequence:
//   buildGraph   → discover modules, topo-sort, output ordered_json
//   planRelease  → compute next semver tag per module, output tags_json
//   pinDeps      → go mod edit -require + go mod tidy for changed modules
//   publish      → signed git tags + push + step summary

import {execFileSync} from 'child_process';
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
 * Build a Go pseudo-version string for modules that have never been tagged.
 * Format: v0.0.0-YYYYMMDDHHMMSS-{12-char commit hash}  (Go module proxy compatible)
 *
 * @param {(args: string[]) => string} execGit
 * @returns {string}
 */
export function pseudoVersion(execGit = git) {
    const hash = execGit(['rev-parse', 'HEAD']).slice(0, 12);
    const tsRaw = execGit(['log', '-1', '--format=%ct', 'HEAD']);
    const date = new Date(Number(tsRaw) * 1000)
        .toISOString()
        .replace(/[^0-9]/g, '')
        .slice(0, 14);
    return `v0.0.0-${date}-${hash}`;
}

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
 * Compute the next tag for a module.
 * - Already tagged: bumps the latest semver tag by bumpKind.
 * - Never tagged: uses a pseudo-version anchored to the current HEAD commit,
 *   consistent with the v0.0.0-{ts}-{hash} format already in use across go.mod files.
 *
 * @param {string} modPath
 * @param {'patch'|'minor'|'major'} bumpKind
 * @param {(args: string[]) => string} [execGit]
 * @returns {string}
 */
export function computeNextTag(modPath, bumpKind, execGit = git) {
    const prefix = tagPrefix(modPath);
    const latest = latestTag(modPath, execGit);
    if (!latest) return `${modPath}/${pseudoVersion(execGit)}`;
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

    core.info(`Discovered ${ordered.length} binding modules in release order:`);
    for (const m of ordered) {
        const deps = depsMap.get(m) ?? [];
        core.info(`  ${m}${deps.length ? ` ← ${deps.join(', ')}` : ''}`);
    }

    core.setOutput('ordered_json', JSON.stringify(ordered));
}

/**
 * Step 2 — Compute the next semver tag for every module, output tags_json.
 * Reads ORDERED_JSON and BUMP from the environment.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function planRelease({core}) {
    const ordered = /** @type {string[]} */ (JSON.parse(process.env.ORDERED_JSON ?? '[]'));
    const bumpFloor = /** @type {'patch'|'minor'|'major'} */ (process.env.BUMP ?? 'patch');

    /** @type {Record<string, string>} */
    const tags = {};
    const skipped = [];

    core.info('Planned tags:');
    for (const mod of ordered) {
        const currentTag = latestTag(mod);

        // Skip modules with no changes since their last release.
        if (!hasChanges(mod, currentTag)) {
            core.info(`  ${mod} → (unchanged since ${currentTag ?? 'untagged'}, skipping)`);
            skipped.push(mod);
            continue;
        }

        // When BUMP=patch (default), auto-detect per module from git log.
        // When BUMP=minor or major, that explicit choice is always honoured.
        let bump = bumpFloor;
        if (bumpFloor === 'patch' && detectBump(mod, currentTag) === 'minor') bump = 'minor';

        tags[mod] = computeNextTag(mod, bump);
        core.info(`  ${mod} → ${tags[mod]}${bump === 'minor' ? '  ⚠ breaking change' : ''}`);
    }

    if (skipped.length) core.info(`\nSkipped ${skipped.length} unchanged module(s)`);
    core.setOutput('tags_json', JSON.stringify(tags));
}

/**
 * Step 3 — Pin newly-released internal deps in each module's go.mod, then tidy.
 * Reads ORDERED_JSON and TAGS_JSON from the environment.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function pinDeps({core}) {
    const repoRoot = process.env.GITHUB_WORKSPACE ?? process.cwd();
    const ordered = /** @type {string[]} */ (JSON.parse(process.env.ORDERED_JSON ?? '[]'));
    const consumers = /** @type {string[]} */ (JSON.parse(process.env.CONSUMERS_JSON ?? '[]'));
    const tags = /** @type {Record<string, string>} */ (JSON.parse(process.env.TAGS_JSON ?? '{}'));
    const dryRun = process.env.DRY_RUN === 'true';

    // Process bindings first (in topo order), then consumers (cli, controller).
    // pinsFor filters by dep in tags, so consumers only get pinned for
    // bindings that were actually released in this run.
    const getDeps = (/** @type {string} */ mod) => internalDepsOf(repoRoot, mod);
    const pinMap = pinsFor([...ordered, ...consumers], tags, getDeps);

    for (const [mod, pins] of pinMap) {
        core.info(`\nPinning deps in ${mod}:`);
        const modDir = join(repoRoot, mod);
        for (const {name, version} of pins) {
            core.info(`  ${dryRun ? '[dry-run] would pin ' : ''}${name}@${version}`);
            if (!dryRun) go_(['mod', 'edit', `-require=${name}@${version}`], {cwd: modDir});
        }
        if (!dryRun) go_(['mod', 'tidy'], {cwd: modDir});
        else core.info('  [dry-run] would run go mod tidy');
    }

    core.info(`\n${dryRun ? '[dry-run] would pin' : 'Pinned'} deps in ${pinMap.size} module(s)`);
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

    core.info('Creating tags:');
    for (const [, tag] of entries) {
        if (dryRun) {
            core.info(`  [dry-run] ${tag}`);
        } else {
            git(['tag', '-s', '-m', `Release ${tag}`, tag]);
            core.info(`  ${tag}`);
        }
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
