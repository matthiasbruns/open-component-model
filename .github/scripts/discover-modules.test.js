import assert from 'assert';
import { splitByTestability } from './discover-modules.js';

// ---------------------------------------------------------------------------
// splitByTestability
// ---------------------------------------------------------------------------
console.log('Testing splitByTestability...');

{
  const mockExec = (cmd) => {
    if (cmd.includes('bindings/go/oci'))
      return JSON.stringify({ tasks: [{ name: 'test' }, { name: 'test/integration' }, { name: 'build' }] });
    if (cmd.includes('cli'))
      return JSON.stringify({ tasks: [{ name: 'test' }, { name: 'build' }] });
    if (cmd.includes('bindings/go/helm'))
      return JSON.stringify({ tasks: [{ name: 'build' }] });
    throw new Error('no taskfile');
  };

  const { unitTestModules, integrationTestModules } = splitByTestability(
    ['bindings/go/oci', 'cli', 'bindings/go/helm', 'unknown-module'],
    mockExec,
  );
  assert.deepStrictEqual(unitTestModules, ['bindings/go/oci', 'cli'],
    'unit: modules with test target');
  assert.deepStrictEqual(integrationTestModules, ['bindings/go/oci'],
    'integration: modules with test/integration target');
}

{
  // all errors → both lists empty
  const { unitTestModules, integrationTestModules } = splitByTestability(
    ['a', 'b'],
    () => { throw new Error('exec failed'); },
  );
  assert.deepStrictEqual(unitTestModules, []);
  assert.deepStrictEqual(integrationTestModules, []);
}

{
  // empty input
  const { unitTestModules, integrationTestModules } = splitByTestability([], () => '{}');
  assert.deepStrictEqual(unitTestModules, []);
  assert.deepStrictEqual(integrationTestModules, []);
}

console.log('  ✅ splitByTestability');

// ---------------------------------------------------------------------------
// splitTestModules binding-only filter (simulated)
// ---------------------------------------------------------------------------
console.log('Testing splitTestModules binding-only filter...');

{
  const nonBindingModules = ['cli', 'kubernetes/controller', 'conformance/scenarios/sovereign/components/notes'];
  const calledWith = [];
  const mockExec = (cmd) => {
    calledWith.push(cmd);
    if (cmd.includes('bindings/go/cel'))
      return JSON.stringify({ tasks: [{ name: 'test' }] });
    throw new Error('unexpected module');
  };

  const allModules = ['cli', 'kubernetes/controller', 'bindings/go/cel', 'conformance/scenarios/sovereign/components/notes'];
  const bindingModules = allModules.filter(m => m.startsWith('bindings/'));
  const { unitTestModules, integrationTestModules } = splitByTestability(bindingModules, mockExec);

  assert.deepStrictEqual(unitTestModules, ['bindings/go/cel'], 'only bindings in unit list');
  assert.deepStrictEqual(integrationTestModules, [], 'no integration modules');
  for (const nm of nonBindingModules) {
    assert.ok(!calledWith.some(c => c.includes(nm)), `${nm} should not be probed`);
  }
}

console.log('  ✅ splitTestModules binding-only filter');

console.log('\n✅ All discover-modules tests passed.');
