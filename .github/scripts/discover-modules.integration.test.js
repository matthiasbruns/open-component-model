// Integration test — runs against the real local checkout.
// Must be run from the repository root:
//   node .github/scripts/discover-modules.integration.test.js

import assert from 'assert';
import { execSync } from 'child_process';
import { generateFilters, splitByTestability } from './discover-modules.js';

// ---------------------------------------------------------------------------
// Discover real modules via task
// ---------------------------------------------------------------------------
console.log('Discovering modules from local checkout...');
const raw = execSync('task go_modules --output interleaved', { encoding: 'utf-8' });
const modules = raw.split('\n').filter(Boolean);

console.log(`  Found ${modules.length} modules`);
assert.ok(modules.length > 0, 'should discover at least one module');
assert.ok(modules.includes('cli'), 'cli module should be present');
assert.ok(modules.some(m => m.startsWith('bindings/')), 'should include binding modules');
assert.ok(modules.some(m => m.includes('/integration')), 'should include integration sub-modules');
console.log('  ✅ module discovery');

// ---------------------------------------------------------------------------
// generateFilters on real module list
// ---------------------------------------------------------------------------
console.log('Testing generateFilters with real modules...');
const filters = generateFilters(modules);

assert.ok(filters.includes('cli:'), 'filters should contain cli key');
assert.ok(filters.includes('"cli/**"'), 'filters should contain cli glob');

// every integration sub-module must also watch its parent
const integrationModules = modules.filter(m => m.includes('/integration'));
for (const m of integrationModules) {
  const parent = m.split('/integration')[0];
  assert.ok(filters.includes(`"${parent}/**"`),
    `integration module ${m} should watch parent ${parent}`);
}
console.log('  ✅ generateFilters');

// ---------------------------------------------------------------------------
// splitByTestability on real modules
// ---------------------------------------------------------------------------
console.log('Testing splitByTestability with real Taskfiles (may take a moment)...');
const { unitTestModules, integrationTestModules } = splitByTestability(modules);

assert.ok(unitTestModules.length > 0, 'should find at least one unit-testable module');
assert.ok(unitTestModules.includes('cli'), 'cli should be unit-testable');
assert.ok(unitTestModules.every(m => modules.includes(m)),
  'all unit test modules should be in the discovered set');

assert.ok(Array.isArray(integrationTestModules), 'integration list should be an array');
assert.ok(integrationTestModules.every(m => modules.includes(m)),
  'all integration test modules should be in the discovered set');

// no module should appear in both lists (unit and integration are mutually exclusive)
// actually they CAN overlap (a module can have both test and test/integration)
// so just check there are no duplicates within each list
const uniqueUnit = new Set(unitTestModules);
assert.strictEqual(uniqueUnit.size, unitTestModules.length, 'no duplicate unit test modules');
const uniqueIntegration = new Set(integrationTestModules);
assert.strictEqual(uniqueIntegration.size, integrationTestModules.length, 'no duplicate integration test modules');

console.log(`  Unit-testable:        ${unitTestModules.length} modules`);
console.log(`  Integration-testable: ${integrationTestModules.length} modules`);
console.log('  ✅ splitByTestability');

console.log('\n✅ All integration tests passed.');
