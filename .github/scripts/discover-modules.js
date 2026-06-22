// @ts-check
import { execSync as _execSync } from 'child_process';

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
 * Outputs: modules_json
 */
export async function discoverModules({ core, execSyncFn = _execSync }) {
  const raw = execSyncFn('task go_modules --output interleaved', { encoding: 'utf-8' });
  const modules = raw.split('\n').filter(Boolean);
  core.setOutput('modules_json', JSON.stringify(modules));
  console.log('📦 Detected modules:', modules);
}

/**
 * Step: Split binding modules by testability
 * Reads env: MODULES_JSON (all modules)
 * Outputs: unit_test_modules_json, integration_test_modules_json
 *
 * Only binding modules are tested here — cli, kubernetes/controller, and other
 * top-level modules have dedicated CI workflows.
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
