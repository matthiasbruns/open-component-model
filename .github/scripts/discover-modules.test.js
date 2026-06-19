import assert from 'assert';
import { generateFilters, filterModules, splitByTestability } from './discover-modules.js';

// ---------------------------------------------------------------------------
// generateFilters
// ---------------------------------------------------------------------------
console.log('Testing generateFilters...');

{
  const result = generateFilters(['bindings/go/oci', 'cli']);
  assert.ok(result.includes('bindings/go/oci:'), 'should include module key');
  assert.ok(result.includes('"bindings/go/oci/**"'), 'should include glob pattern');
  assert.ok(result.includes('cli:'), 'should include cli key');
  assert.ok(result.includes('"cli/**"'), 'should include cli glob');
  assert.ok(!result.includes('/integration'), 'should not add parent for non-integration modules');
}

{
  const result = generateFilters(['bindings/go/oci/integration']);
  assert.ok(result.includes('"bindings/go/oci/integration/**"'), 'should include integration glob');
  assert.ok(result.includes('"bindings/go/oci/**"'), 'should also watch parent module');
}

{
  assert.strictEqual(generateFilters([]), '', 'empty input produces empty output');
}

console.log('  ✅ generateFilters');

// ---------------------------------------------------------------------------
// filterModules
// ---------------------------------------------------------------------------
console.log('Testing filterModules...');

const ALL = ['bindings/go/oci', 'bindings/go/helm', 'cli'];

{
  // ciChanged overrides everything → all modules in both lists
  const { modules, lintModules } = filterModules(ALL, ['bindings/go/oci'], {
    checkOnlyChanged: true, ciChanged: true, envChanged: false,
  });
  assert.deepStrictEqual(modules, ALL, 'ciChanged: modules = all');
  assert.deepStrictEqual(lintModules, ALL, 'ciChanged: lintModules = all');
}

{
  // PR mode, only one module changed, .env unchanged → test + lint that module only
  const { modules, lintModules } = filterModules(ALL, ['bindings/go/oci'], {
    checkOnlyChanged: true, ciChanged: false, envChanged: false,
  });
  assert.deepStrictEqual(modules, ['bindings/go/oci']);
  assert.deepStrictEqual(lintModules, ['bindings/go/oci']);
}

{
  // PR mode, .env changed → test changed only, lint everything
  const { modules, lintModules } = filterModules(ALL, ['bindings/go/oci'], {
    checkOnlyChanged: true, ciChanged: false, envChanged: true,
  });
  assert.deepStrictEqual(modules, ['bindings/go/oci'], '.env change: test = changed');
  assert.deepStrictEqual(lintModules, ALL, '.env change: lint = all');
}

{
  // PR mode, nothing changed → empty
  const { modules, lintModules } = filterModules(ALL, [], {
    checkOnlyChanged: true, ciChanged: false, envChanged: false,
  });
  assert.deepStrictEqual(modules, []);
  assert.deepStrictEqual(lintModules, []);
}

{
  // Push to main (checkOnlyChanged=false) → all modules regardless of changedPaths
  const { modules, lintModules } = filterModules(ALL, [], {
    checkOnlyChanged: false, ciChanged: false, envChanged: false,
  });
  assert.deepStrictEqual(modules, ALL, 'push: modules = all');
  assert.deepStrictEqual(lintModules, ALL, 'push: lintModules = all');
}

console.log('  ✅ filterModules');

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

console.log('\n✅ All discover-modules tests passed.');
