// @ts-check
// Binding release orchestration — one file, named exports per workflow step.
//
// Workflow calls each export in sequence:
//   buildGraph   → discover modules, topo-sort, output ordered_json
//   planRelease  → compute next semver tag per module, output tags_json
//   pinDeps      → go mod edit -require + go mod tidy for changed modules
//   publish      → signed git tags + push + step summary

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
 * Returns null for modules with no existing tag — those are pinned
 * via `go get @commit` instead (no tag created).
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
            core.info(`  ${mod} → (untagged, will pin via commit hash)`);
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
 * Step 3 — Pin newly-released internal deps in each module's go.mod, then tidy.
 * Reads ORDERED_JSON and TAGS_JSON from the environment.
 *
 * @param {import('@actions/github-script').AsyncFunctionArguments} args
 */
export async function pinDeps({core}) {
    const repoRoot   = process.env.GITHUB_WORKSPACE ?? process.cwd();
    const ordered    = /** @type {string[]} */ (JSON.parse(process.env.ORDERED_JSON ?? '[]'));
    const consumers  = /** @type {string[]} */ (JSON.parse(process.env.CONSUMERS_JSON ?? '[]'));
    const tags       = /** @type {Record<string, string>} */ (JSON.parse(process.env.TAGS_JSON ?? '{}'));
    const headCommit = process.env.HEAD_COMMIT ?? git(['rev-parse', 'HEAD']);
    const dryRun     = process.env.DRY_RUN === 'true';

    const taggedSet  = new Set(Object.keys(tags));
    const getDeps    = (/** @type {string} */ mod) => internalDepsOf(repoRoot, mod);

    // For each module (bindings in topo order, then consumers), pin the versions
    // of any internal deps that changed in this release run:
    //   - semver-tagged deps: go mod edit -require (tag may not exist yet, no fetch)
    //   - untagged deps:      GOPROXY=direct go get @commit (Go derives the pseudo-version)
    for (const mod of [...ordered, ...consumers]) {
        const deps = getDeps(mod);
        const semverPins = deps.filter(dep => taggedSet.has(dep));
        const commitPins = deps.filter(dep => !taggedSet.has(dep) && ordered.includes(dep)
                                              && !latestTag(dep));

        if (!semverPins.length && !commitPins.length) continue;

        core.info(`\nPinning deps in ${mod}:`);
        const modDir = join(repoRoot, mod);

        for (const dep of semverPins) {
            const version = tagToVersion(tags[dep]);
            const name    = `${OCM_PREFIX}${dep}`;
            core.info(`  ${dryRun ? '[dry-run] ' : ''}${name}@${version}`);
            if (!dryRun) go_(['mod', 'edit', `-require=${name}@${version}`], {cwd: modDir});
        }

        for (const dep of commitPins) {
            const name = `${OCM_PREFIX}${dep}`;
            core.info(`  ${dryRun ? '[dry-run] ' : ''}${name}@${headCommit.slice(0, 12)} (commit)`);
            if (!dryRun) go_(['get', `${name}@${headCommit}`], {
                cwd: modDir,
                env: {...process.env, GOPROXY: 'direct', GONOSUMDB: 'ocm.software/open-component-model/*'},
            });
        }

        if (!dryRun) go_(['mod', 'tidy'], {cwd: modDir});
        else core.info('  [dry-run] would run go mod tidy');
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

        git(['tag', '-s', '-m', `Release ${tag}`, tag]);
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
