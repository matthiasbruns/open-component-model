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

/**
 * Check whether a consumer module (cli, kubernetes/controller) is affected.
 * True if its own code changed, or any binding it imports is in the affected set.
 *
 * @param {string} consumerPath   e.g. "cli"
 * @param {Set<string>} affectedBindings
 * @param {string[]} changedFiles
 * @param {string} repoRoot
 * @param {function} execSyncFn
 * @returns {boolean}
 */
export function isConsumerAffected(consumerPath, affectedBindings, changedFiles, repoRoot, execSyncFn) {
  if (changedFiles.some(f => f.startsWith(`${consumerPath}/`))) return true;
  return internalDepsOf(repoRoot, consumerPath, execSyncFn)
    .some(dep => affectedBindings.has(dep));
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
 * Outputs:
 *   modules_json              — all binding modules (for lint)
 *   affected_bindings_json    — bindings directly or transitively affected
 *   affected_integration_json — subset of above that have integration tests
 *   test_cli                  — "true" if cli should be tested
 *   test_controller           — "true" if kubernetes/controller should be tested
 */
export async function discoverModules({ core, execSyncFn = _execSync }) {
  const repoRoot = process.env.GITHUB_WORKSPACE ?? process.cwd();
  const baseBranch = process.env.GITHUB_BASE_REF ?? ''; // set on PRs, empty on push

  // 1. All binding modules from go.work
  const raw = execSyncFn('task go_modules --output interleaved', { encoding: 'utf-8' });
  const allModules = raw.split('\n').filter(Boolean);
  const bindingModules = allModules.filter(m => m.startsWith('bindings/'));

  // 2. Dependency graph and changed files
  const dependentsOf = buildDependentsMap(bindingModules, repoRoot, execSyncFn);
  const changedFiles = getChangedFiles(baseBranch, execSyncFn);

  // 3. Affected set — fall back to all bindings when diff is unavailable (push to main)
  const changedBindings = changedModulesFromFiles(bindingModules, changedFiles);
  const affectedBindings = changedFiles.length > 0
    ? computeAffectedSet(changedBindings, bindingModules, dependentsOf)
    : bindingModules;

  // 4. Unit and integration subsets of the affected set
  const { unitTestModules, integrationTestModules } = splitByTestability(affectedBindings, execSyncFn);

  // 5. Consumer impact
  const affectedSet = new Set(affectedBindings);
  const testCLI = isConsumerAffected('cli', affectedSet, changedFiles, repoRoot, execSyncFn);
  const testController = isConsumerAffected('kubernetes/controller', affectedSet, changedFiles, repoRoot, execSyncFn);

  // 6. Combined lint set — affected bindings + affected consumers
  const affectedLint = [
    ...affectedBindings,
    ...(testCLI ? ['cli'] : []),
    ...(testController ? ['kubernetes/controller'] : []),
  ];

  core.setOutput('modules_json', JSON.stringify(allModules));
  core.setOutput('affected_bindings_json', JSON.stringify(affectedBindings));
  core.setOutput('affected_unit_json', JSON.stringify(unitTestModules));
  core.setOutput('affected_integration_json', JSON.stringify(integrationTestModules));
  core.setOutput('affected_lint_json', JSON.stringify(affectedLint));
  core.setOutput('test_cli', String(testCLI));
  core.setOutput('test_controller', String(testController));

  console.log('📦 All modules:', allModules.length);
  console.log('🎯 Affected bindings:', affectedBindings);
  console.log('🧪 Affected unit:', unitTestModules);
  console.log('🧬 Affected integration:', integrationTestModules);
  console.log('🔍 Affected lint:', affectedLint);
  console.log('🔧 Test CLI:', testCLI, '| Test Controller:', testController);
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
