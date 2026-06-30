// Integration test — runs against the real local checkout.
// Must be run from the repository root:
//   node .github/scripts/discover-modules.integration.test.js

import assert from 'assert';
import { execSync } from 'child_process';
import { splitByTestability } from './discover-modules.js';

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
// splitByTestability on real binding modules only
// ---------------------------------------------------------------------------
console.log('Testing splitByTestability with real Taskfiles (may take a moment)...');
const bindingModules = modules.filter(m => m.startsWith('bindings/'));
const { unitTestModules, integrationTestModules } = splitByTestability(bindingModules);

assert.ok(unitTestModules.length > 0, 'should find at least one unit-testable binding');
assert.ok(unitTestModules.every(m => m.startsWith('bindings/')), 'all unit modules must be bindings');
assert.ok(unitTestModules.every(m => modules.includes(m)),
  'all unit test modules should be in the discovered set');

assert.ok(Array.isArray(integrationTestModules), 'integration list should be an array');
assert.ok(integrationTestModules.every(m => m.startsWith('bindings/')), 'all integration modules must be bindings');
assert.ok(integrationTestModules.every(m => modules.includes(m)),
  'all integration test modules should be in the discovered set');

const uniqueUnit = new Set(unitTestModules);
assert.strictEqual(uniqueUnit.size, unitTestModules.length, 'no duplicate unit test modules');
const uniqueIntegration = new Set(integrationTestModules);
assert.strictEqual(uniqueIntegration.size, integrationTestModules.length, 'no duplicate integration test modules');

// non-binding modules must not appear
assert.ok(!unitTestModules.includes('cli'), 'cli must not appear in binding unit tests');
assert.ok(!unitTestModules.includes('kubernetes/controller'), 'controller must not appear in binding unit tests');

console.log(`  Unit-testable:        ${unitTestModules.length} modules`);
console.log(`  Integration-testable: ${integrationTestModules.length} modules`);
console.log('  ✅ splitByTestability');

console.log('\n✅ All integration tests passed.');
