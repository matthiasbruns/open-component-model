// @ts-check
// Orchestrates the release of all Go binding modules in topological dependency
// order. For each module the script:
//   1. Pre-computes the next semver tag
//   2. Pins newly-released internal deps via `go mod edit -require`
//   3. Runs `go mod tidy` (workspace-aware, so no proxy needed for local modules)
//   4. Commits all go.mod / go.sum / go.work.sum changes in one release-prep commit
//   5. Creates a signed tag for every module — all pointing at the same commit
//   6. Pushes the commit + all tags

import { execFileSync } from 'child_process';
import { readFileSync } from 'fs';
import { join } from 'path';

const OCM_PREFIX = 'ocm.software/open-component-model/';

// --------------------------
// Git / Go helpers
// --------------------------

/** @param {string[]} args @param {import('child_process').ExecFileSyncOptions} [opts] */
function git(args, opts = {}) {
  const token = process.env.GITHUB_TOKEN;
  const authArgs = token
    ? ['-c', `http.extraHeader=Authorization: basic ${Buffer.from(`x-access-token:${token}`).toString('base64')}`]
    : [];
  return /** @type {string} */ (execFileSync('git', [...authArgs, ...args], { encoding: 'utf-8', stdio: 'pipe', ...opts })).trim();
}

/** @param {string[]} args @param {import('child_process').ExecFileSyncOptions} [opts] */
function go_(args, opts = {}) {
  return /** @type {string} */ (execFileSync('go', args, { encoding: 'utf-8', stdio: ['pipe', 'pipe', 'inherit'], ...opts })).trim();
}

// --------------------------
// Module discovery
// --------------------------

/**
 * Parse go.work and return repo-relative paths of binding modules to release.
 * Excludes integration test modules (path ends with /integration).
 *
 * @param {string} repoRoot
 * @returns {string[]}
 */
function discoverModules(repoRoot) {
  const src = readFileSync(join(repoRoot, 'go.work'), 'utf-8');
  const block = src.match(/^use\s*\(([\s\S]*?)\)/m)?.[1] ?? '';
  return block
    .split('\n')
    .map(l => l.trim().replace(/^\.\//, ''))
    .filter(p => p.startsWith('bindings/go/') && !p.endsWith('/integration'));
}

/**
 * Return the internal binding deps of a module as repo-relative paths.
 * Only returns paths that are within the bindings/go/ tree.
 *
 * @param {string} repoRoot
 * @param {string} modPath  e.g. "bindings/go/oci"
 * @returns {string[]}
 */
function internalDepsOf(repoRoot, modPath) {
  const src = readFileSync(join(repoRoot, modPath, 'go.mod'), 'utf-8');
  const deps = new Set();
  for (const [, name] of src.matchAll(new RegExp(`(${OCM_PREFIX}bindings/go/[^\\s]+)\\s+v`, 'g'))) {
    deps.add(name.replace(OCM_PREFIX, ''));
  }
  return [...deps];
}

// --------------------------
// Topological sort (Kahn's algorithm)
// --------------------------

/**
 * Sort modules so that a dependency always appears before its dependent.
 *
 * @param {string[]} modules
 * @param {Map<string, string[]>} depsMap  module → [dep, ...]
 * @returns {string[]}
 */
function topoSort(modules, depsMap) {
  /** @type {Map<string, number>} */
  const inDeg = new Map(modules.map(m => [m, 0]));
  /** @type {Map<string, string[]>} */
  const adj   = new Map(modules.map(m => [m, []]));

  for (const [mod, deps] of depsMap) {
    for (const d of deps) {
      if (!inDeg.has(d)) continue;
      inDeg.set(mod, (inDeg.get(mod) ?? 0) + 1);
      adj.get(d)?.push(mod);
    }
  }

  const queue  = modules.filter(m => inDeg.get(m) === 0);
  /** @type {string[]} */
  const sorted = [];
  while (queue.length) {
    const n = queue.shift() ?? '';
    sorted.push(n);
    for (const dependent of (adj.get(n) ?? [])) {
      const deg = (inDeg.get(dependent) ?? 0) - 1;
      inDeg.set(dependent, deg);
      if (deg === 0) queue.push(dependent);
    }
  }

  if (sorted.length !== modules.length) {
    const stuck = modules.filter(m => !sorted.includes(m));
    throw new Error(`Cycle detected in binding dependencies: ${stuck.join(', ')}`);
  }
  return sorted;
}

// --------------------------
// Version computation
// --------------------------

/**
 * Get the latest semver tag for a module path, or null if none exists.
 *
 * @param {string} modPath
 * @returns {string|null}
 */
function latestVersionTag(modPath) {
  const tags = git(['tag', '--list', `${modPath}/v*`, '--sort=-version:refname'])
    .split('\n')
    .filter(Boolean);
  return tags[0] ?? null;
}

/**
 * Bump a semver string (no leading "v") by the given kind.
 *
 * @param {string} version  e.g. "0.4.1"
 * @param {'patch'|'minor'|'major'} kind
 * @returns {string}
 */
function bumpSemver(version, kind) {
  const [maj, min, pat] = version.replace(/^v/, '').split('-')[0].split('.').map(Number);
  if (kind === 'major') return `${maj + 1}.0.0`;
  if (kind === 'minor') return `${maj}.${min + 1}.0`;
  return `${maj}.${min}.${pat + 1}`;
}

/**
 * Compute the next tag string for a module.
 *
 * @param {string} modPath
 * @param {'patch'|'minor'|'major'} bumpKind
 * @returns {string}  e.g. "bindings/go/oci/v0.0.47"
 */
function computeNextTag(modPath, bumpKind) {
  const prefix = `${modPath}/v`;
  const latest = latestVersionTag(modPath);
  if (!latest) return `${prefix}0.0.1`;
  return `${prefix}${bumpSemver(latest.replace(prefix, ''), bumpKind)}`;
}

/**
 * Extract a bare semver version string from a full tag.
 * "bindings/go/oci/v0.0.47" → "v0.0.47"
 *
 * @param {string} tag
 * @returns {string}
 */
function tagToVersion(tag) {
  return `v${tag.split('/v').at(-1)}`;
}

// --------------------------
// Main entrypoint
// --------------------------

/** @param {import('@actions/github-script').AsyncFunctionArguments} args */
export default async function releaseBindings({ core }) {
  const repoRoot = process.env.GITHUB_WORKSPACE ?? process.cwd();
  const bumpKind = /** @type {'patch'|'minor'|'major'} */ (process.env.BUMP ?? 'patch');
  const dryRun   = process.env.DRY_RUN === 'true';

  // 1. Discover modules and build internal dependency graph
  const modules = discoverModules(repoRoot);
  const depsMap = new Map(
    modules.map(m => [m, internalDepsOf(repoRoot, m).filter(d => modules.includes(d))])
  );
  const sorted = topoSort(modules, depsMap);

  core.info(`Releasing ${sorted.length} binding modules in dependency order:`);
  for (const m of sorted) {
    const deps = depsMap.get(m) ?? [];
    core.info(`  ${m}${deps.length ? ` (needs: ${deps.join(', ')})` : ''}`);
  }

  // 2. Pre-compute all new tags so downstream modules can reference them
  const newTags = new Map(sorted.map(m => [m, computeNextTag(m, bumpKind)]));

  // 3. Pin newly-released internal deps in each module's go.mod, then tidy
  const modifiedMods = new Set();
  for (const mod of sorted) {
    const toPin = (depsMap.get(mod) ?? []).filter(d => newTags.has(d));
    if (!toPin.length) continue;

    const modDir = join(repoRoot, mod);
    core.info(`\nPinning deps in ${mod}:`);
    for (const dep of toPin) {
      const version = tagToVersion(newTags.get(dep) ?? '');
      const name    = `${OCM_PREFIX}${dep}`;
      core.info(`  ${name}@${version}`);
      go_(['mod', 'edit', `-require=${name}@${version}`], { cwd: modDir });
    }
    go_(['mod', 'tidy'], { cwd: modDir });
    modifiedMods.add(mod);
  }

  // 4. Commit go.mod / go.sum / go.work.sum changes in a single release-prep commit
  const dirty = git(['status', '--porcelain']);
  if (dirty) {
    const toStage = ['go.work.sum'];
    for (const mod of modifiedMods) {
      toStage.push(`${mod}/go.mod`, `${mod}/go.sum`);
    }

    if (dryRun) {
      core.info(`\n[dry-run] would stage and commit:\n  ${toStage.join('\n  ')}`);
    } else {
      git(['add', '--', ...toStage]);
      git(['commit', '-S', '-s', '-m', 'chore(release): pin binding go.mod deps for release']);
      core.info('\nCommitted go.mod dependency pins');
    }
  }

  // 5. Create a signed tag for every module (all point at the same post-commit HEAD)
  core.info('\nCreating tags:');
  for (const [, tag] of newTags) {
    if (dryRun) {
      core.info(`  [dry-run] ${tag}`);
    } else {
      git(['tag', '-s', '-m', `Release ${tag}`, tag]);
      core.info(`  ${tag}`);
    }
  }

  // 6. Push the release-prep commit and all tags in one operation
  if (!dryRun) {
    git(['push', 'origin', 'HEAD']);
    git(['push', 'origin', ...[...newTags.values()].map(t => `refs/tags/${t}`)]);
    core.info(`\nPushed ${newTags.size} tags`);
  }

  // Summary
  const table = [
    [{ data: 'Module', header: true }, { data: 'New Tag', header: true }, { data: 'Deps Updated', header: true }],
    ...sorted.map(m => [
      m,
      newTags.get(m) ?? '',
      (depsMap.get(m) ?? []).filter(d => newTags.has(d)).map(d => tagToVersion(newTags.get(d) ?? '')).join(', ') || '—',
    ]),
  ];
  await core.summary
    .addHeading(dryRun ? '🔍 Dry-run: Binding Release Plan' : '✅ Binding Release Complete')
    .addTable(table)
    .write();

  core.setOutput('tags_json', JSON.stringify(Object.fromEntries(newTags)));
}
