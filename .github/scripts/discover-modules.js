// @ts-check
import { execSync as _execSync } from 'child_process';

const OCM_PREFIX = 'ocm.software/open-component-model/';

// ---------------------------------------------------------------------------
// Graph helpers
// ---------------------------------------------------------------------------

/**
 * Read the internal binding deps of a module from its go.mod.
 * Returns repo-relative paths like 'bindings/go/runtime'.
 *
 * @param {string} repoRoot
 * @param {string} modPath
 * @param {function} execSyncFn
 * @returns {string[]}
 */
function internalDepsOf(repoRoot, modPath, execSyncFn) {
  try {
    const raw = execSyncFn('go mod edit -json', {
      encoding: 'utf-8',
      cwd: `${repoRoot}/${modPath}`,
      stdio: 'pipe',
    });
    const { Require = [] } = JSON.parse(raw);
    return Require
      .map(r => r.Path)
      .filter(p => p.startsWith(`${OCM_PREFIX}bindings/go/`))
      .map(p => p.replace(OCM_PREFIX, ''));
  } catch {
    return [];
  }
}

/**
 * Build a forward dep map (module → modules that directly depend on it)
 * so we can walk from a changed module to all its dependents.
 *
 * @param {string[]} modules
 * @param {string} repoRoot
 * @param {function} execSyncFn
 * @returns {Map<string, string[]>}
 */
export function buildDependentsMap(modules, repoRoot, execSyncFn) {
  const dependentsOf = new Map(modules.map(m => [m, []]));
  for (const mod of modules) {
    for (const dep of internalDepsOf(repoRoot, mod, execSyncFn)) {
      if (dependentsOf.has(dep)) {
        dependentsOf.get(dep).push(mod);
      }
    }
  }
  return dependentsOf;
}

/**
 * BFS forward from changedModules through the dependents graph.
 * Returns all affected modules in the same order as allModules.
 *
 * @param {string[]} changedModules
 * @param {string[]} allModules
 * @param {Map<string, string[]>} dependentsOf
 * @returns {string[]}
 */
export function computeAffectedSet(changedModules, allModules, dependentsOf) {
  const affected = new Set(changedModules.filter(m => allModules.includes(m)));
  const queue = [...affected];
  while (queue.length > 0) {
    const mod = queue.shift();
    for (const dependent of (dependentsOf.get(mod) ?? [])) {
      if (!affected.has(dependent)) {
        affected.add(dependent);
        queue.push(dependent);
      }
    }
  }
  return allModules.filter(m => affected.has(m));
}

/**
 * Map a list of changed file paths to the binding modules that contain them.
 *
 * @param {string[]} allModules
 * @param {string[]} changedFiles
 * @returns {string[]}
 */
export function changedModulesFromFiles(allModules, changedFiles) {
  return allModules.filter(mod => changedFiles.some(f => f.startsWith(`${mod}/`)));
}

/**
 * Get changed files in this PR or push via git diff.
 * Falls back to all files (safe) if the diff cannot be computed.
 *
 * @param {string} baseBranch  e.g. "main" (GITHUB_BASE_REF), or empty string for push
 * @param {function} execSyncFn
 * @returns {string[]}
 */
function getChangedFiles(baseBranch, execSyncFn) {
  try {
    const cmd = baseBranch
      ? `git diff --name-only origin/${baseBranch}...HEAD`
      : `git diff --name-only HEAD~1 HEAD`;
    return execSyncFn(cmd, { encoding: 'utf-8' }).split('\n').filter(Boolean);
  } catch {
    return []; // empty → caller should fall back to all modules
  }
}

// ---------------------------------------------------------------------------
// Testability helpers
// ---------------------------------------------------------------------------

/**
 * Split a module list into unit-testable and integration-testable subsets by
 * probing each module's Taskfile for the presence of 'test' / 'test/integration'
 * targets. Modules without a Taskfile (or that error) are silently skipped.
 *
 * @param {string[]} modules
 * @param {function} [execSyncFn]
 * @returns {{ unitTestModules: string[], integrationTestModules: string[] }}
 */
export function splitByTestability(modules, execSyncFn = _execSync) {
  const unitTestModules = [];
  const integrationTestModules = [];
  for (const module of modules) {
    try {
      const raw = execSyncFn(`task -d ${module} -aj`, { encoding: 'utf-8' });
      const taskNames = JSON.parse(raw).tasks.map(t => t.name);
      if (taskNames.includes('test')) unitTestModules.push(module);
      if (taskNames.includes('test/integration')) integrationTestModules.push(module);
    } catch {
      // no Taskfile or task invocation error — skip
    }
  }
  return { unitTestModules, integrationTestModules };
}

// ---------------------------------------------------------------------------
// Entry points called from actions/github-script
// ---------------------------------------------------------------------------

/**
 * Step: Discover Go Modules and compute the affected set for this PR/push.
 *
 * The dependency graph is built for ALL Go modules (bindings, cli, controller,
 * etc.) so consumers appear naturally in the affected set when their binding
 * deps change — no hardcoded module names anywhere.
 *
 * Outputs:
 *   modules_json              — all Go modules discovered in the repo
 *   affected_modules_json     — all modules directly or transitively affected
 *   affected_unit_json        — subset with a 'test' Taskfile target
 *   affected_integration_json — subset with a 'test/integration' Taskfile target
 *   affected_lint_json        — same as affected_modules_json (all can be linted)
 */
export async function discoverModules({ core, execSyncFn = _execSync }) {
  const repoRoot = process.env.GITHUB_WORKSPACE ?? process.cwd();
  const baseBranch = process.env.GITHUB_BASE_REF ?? ''; // set on PRs, empty on push

  // 1. All Go modules from go.work
  const raw = execSyncFn('task go_modules --output interleaved', { encoding: 'utf-8' });
  const allModules = raw.split('\n').filter(Boolean);

  // 2. Full dependency graph across all modules + changed files
  const dependentsOf = buildDependentsMap(allModules, repoRoot, execSyncFn);
  const changedFiles = getChangedFiles(baseBranch, execSyncFn);

  // 3. Affected set — fall back to all modules when diff is unavailable (push to main)
  const changedModules = changedModulesFromFiles(allModules, changedFiles);
  const affectedModules = changedFiles.length > 0
    ? computeAffectedSet(changedModules, allModules, dependentsOf)
    : allModules;

  // 4. Testability subsets — bindings only.
  // cli and kubernetes/controller have dedicated workflows (pipeline.yml)
  // and must not be added to the binding test matrix.
  const affectedBindings = affectedModules.filter(m => m.startsWith('bindings/'));
  const { unitTestModules, integrationTestModules } = splitByTestability(affectedBindings, execSyncFn);

  core.setOutput('modules_json',              JSON.stringify(allModules));
  core.setOutput('affected_modules_json',     JSON.stringify(affectedModules));
  core.setOutput('affected_unit_json',        JSON.stringify(unitTestModules));
  core.setOutput('affected_integration_json', JSON.stringify(integrationTestModules));
  core.setOutput('affected_lint_json',        JSON.stringify(affectedModules));

  console.log('📦 All modules:', allModules.length);
  console.log('🎯 Affected:', affectedModules);
  console.log('🧪 Unit:', unitTestModules);
  console.log('🧬 Integration:', integrationTestModules);
}

/**
 * Step: Split binding modules by testability (kept for release-bindings.yaml compatibility).
 * Reads env: MODULES_JSON
 * Outputs: unit_test_modules_json, integration_test_modules_json
 */
export async function splitTestModules({ core, execSyncFn = _execSync }) {
  const allModules = JSON.parse(process.env.MODULES_JSON || '[]');
  const modules = allModules.filter(m => m.startsWith('bindings/'));
  const { unitTestModules, integrationTestModules } = splitByTestability(modules, execSyncFn);
  core.setOutput('unit_test_modules_json', JSON.stringify(unitTestModules));
  core.setOutput('integration_test_modules_json', JSON.stringify(integrationTestModules));
  console.log('🧪 Unit test modules:', unitTestModules);
  console.log('🧬 Integration test modules:', integrationTestModules);
}
