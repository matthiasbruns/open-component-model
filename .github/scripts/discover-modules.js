// @ts-check
import { execSync as _execSync } from 'child_process';

/**
 * Generate dorny/paths-filter YAML config from a list of module paths.
 * Integration sub-modules (paths containing '/integration') also watch their
 * parent module so that a change to the parent triggers the integration test.
 *
 * @param {string[]} modules
 * @returns {string} YAML config for dorny/paths-filter
 */
export function generateFilters(modules) {
  return modules.map(module => {
    const lines = [`${module}:`, ` - "${module}/**"`];
    if (module.includes('/integration')) {
      const parent = module.split('/integration')[0];
      lines.push(` - "${parent}/**"`);
    }
    return lines.join('\n');
  }).join('\n');
}

/**
 * Decide which modules to build/test/lint given change signals.
 *
 * Priority order:
 *   1. ciChanged  → everything (a CI config change is a global signal)
 *   2. checkOnlyChanged (PR mode) → changed modules only; lint all when envChanged
 *   3. otherwise (push to main)  → everything
 *
 * @param {string[]} allModules
 * @param {string[]} changedPaths  - module keys that dorny/paths-filter reported changed
 * @param {{ checkOnlyChanged: boolean, ciChanged: boolean, envChanged: boolean }} opts
 * @returns {{ modules: string[], lintModules: string[] }}
 */
export function filterModules(allModules, changedPaths, { checkOnlyChanged, ciChanged, envChanged }) {
  if (ciChanged) {
    return { modules: allModules, lintModules: allModules };
  }
  if (checkOnlyChanged) {
    const filtered = allModules.filter(m => changedPaths.some(c => c.includes(m)));
    return { modules: filtered, lintModules: envChanged ? allModules : filtered };
  }
  return { modules: allModules, lintModules: allModules };
}

/**
 * Split a module list into unit-testable and integration-testable subsets by
 * probing each module's Taskfile for the presence of 'test' / 'test/integration'
 * targets. Modules without a Taskfile (or that error) are silently skipped.
 *
 * @param {string[]} modules
 * @param {function} [execSyncFn]  injectable for testing; defaults to child_process.execSync
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
 * Step: Discover Go Modules
 * Outputs: modules_json, filters
 */
export async function discoverModules({ core, execSyncFn = _execSync }) {
  const raw = execSyncFn('task go_modules --output interleaved', { encoding: 'utf-8' });
  const modules = raw.split('\n').filter(Boolean);
  core.setOutput('modules_json', JSON.stringify(modules));
  core.setOutput('filters', generateFilters(modules));
  console.log('📦 Detected modules:', modules);
}

/**
 * Step: Filter JSONs Based on Changes
 * Reads env: MODULES_JSON, CHANGE_JSON, CI_CHANGED, ENV_CHANGED, check_only_changed
 * Outputs: modules_json, lint_modules_json
 */
export async function filterChanged({ core }) {
  const allModules = JSON.parse(process.env.MODULES_JSON || '[]');
  const changedPaths = JSON.parse(process.env.CHANGE_JSON || '[]');
  const { modules, lintModules } = filterModules(allModules, changedPaths, {
    checkOnlyChanged: process.env.check_only_changed === 'true',
    ciChanged: process.env.CI_CHANGED === 'true',
    envChanged: process.env.ENV_CHANGED === 'true',
  });
  core.setOutput('modules_json', JSON.stringify(modules));
  core.setOutput('lint_modules_json', JSON.stringify(lintModules));
  console.log('🎯 Filtered modules:', modules);
}

/**
 * Step: Filter based on Testability
 * Reads env: MODULES_JSON
 * Outputs: unit_test_modules_json, integration_test_modules_json
 *
 * Only binding modules are tested here — cli, kubernetes/controller, and other
 * top-level modules have dedicated CI workflows and must not appear in this matrix.
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
