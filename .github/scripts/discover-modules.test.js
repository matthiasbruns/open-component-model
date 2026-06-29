import assert from 'assert';
import {
  computeAffectedSet,
  changedModulesFromFiles,
  buildDependentsMap,
  isConsumerAffected,
  splitByTestability,
} from './discover-modules.js';

// ---------------------------------------------------------------------------
// changedModulesFromFiles
// ---------------------------------------------------------------------------
console.log('Testing changedModulesFromFiles...');

const ALL = ['bindings/go/runtime', 'bindings/go/blob', 'bindings/go/oci', 'bindings/go/transfer'];

assert.deepStrictEqual(
  changedModulesFromFiles(ALL, ['bindings/go/blob/blob.go', 'bindings/go/blob/doc.go']),
  ['bindings/go/blob'],
  'single module detected from multiple files in same module',
);

assert.deepStrictEqual(
  changedModulesFromFiles(ALL, ['bindings/go/runtime/runtime.go', 'bindings/go/oci/oci.go']),
  ['bindings/go/runtime', 'bindings/go/oci'],
  'multiple modules detected',
);

assert.deepStrictEqual(
  changedModulesFromFiles(ALL, ['cli/cmd/main.go', 'docs/adr/0001.md']),
  [],
  'changes outside bindings produce empty set',
);

assert.deepStrictEqual(
  changedModulesFromFiles(ALL, []),
  [],
  'no changed files → empty set',
);

// Prefix match must be exact — 'bindings/go/blob' must not match 'bindings/go/blob-extra/foo.go'
assert.deepStrictEqual(
  changedModulesFromFiles(ALL, ['bindings/go/blob-extra/foo.go']),
  [],
  'prefix match does not bleed into similarly-named modules',
);

// ---------------------------------------------------------------------------
// computeAffectedSet
// ---------------------------------------------------------------------------
console.log('Testing computeAffectedSet...');

{
  // runtime ← blob ← transfer
  //         ← oci  ←
  const dependentsOf = new Map([
    ['bindings/go/runtime',  ['bindings/go/blob', 'bindings/go/oci']],
    ['bindings/go/blob',     ['bindings/go/transfer']],
    ['bindings/go/oci',      ['bindings/go/transfer']],
    ['bindings/go/transfer', []],
  ]);

  // Leaf change: only itself
  assert.deepStrictEqual(
    computeAffectedSet(['bindings/go/transfer'], ALL, dependentsOf),
    ['bindings/go/transfer'],
    'leaf module: only itself affected',
  );

  // Change runtime → all modules affected
  assert.deepStrictEqual(
    computeAffectedSet(['bindings/go/runtime'], ALL, dependentsOf),
    ALL,
    'root dep change: all transitively affected',
  );

  // Change blob → blob + transfer (not runtime, not oci)
  assert.deepStrictEqual(
    computeAffectedSet(['bindings/go/blob'], ALL, dependentsOf),
    ['bindings/go/blob', 'bindings/go/transfer'],
    'mid-graph change: only downstream affected',
  );

  // Change oci → oci + transfer
  assert.deepStrictEqual(
    computeAffectedSet(['bindings/go/oci'], ALL, dependentsOf),
    ['bindings/go/oci', 'bindings/go/transfer'],
    'sibling branch: only its own downstream',
  );

  // Change multiple: blob + oci → both + transfer, no duplicates
  assert.deepStrictEqual(
    computeAffectedSet(['bindings/go/blob', 'bindings/go/oci'], ALL, dependentsOf),
    ['bindings/go/blob', 'bindings/go/oci', 'bindings/go/transfer'],
    'multiple changed modules: union without duplicates',
  );

  // Module not in allModules (e.g. a consumer) → ignored
  assert.deepStrictEqual(
    computeAffectedSet(['cli'], ALL, dependentsOf),
    [],
    'module not in allModules is ignored',
  );

  // Output preserves the order of allModules
  const reversed = [...ALL].reverse();
  const result = computeAffectedSet(['bindings/go/runtime'], reversed, dependentsOf);
  assert.deepStrictEqual(result, reversed, 'output order matches allModules order');
}

// ---------------------------------------------------------------------------
// buildDependentsMap
// ---------------------------------------------------------------------------
console.log('Testing buildDependentsMap...');

{
  const modules = ['bindings/go/runtime', 'bindings/go/blob', 'bindings/go/oci'];

  // blob and oci both depend on runtime
  const mockExec = (_cmd, opts) => {
    const mod = opts.cwd.replace('/root/', '');
    const deps = {
      'bindings/go/runtime': [],
      'bindings/go/blob':    ['bindings/go/runtime'],
      'bindings/go/oci':     ['bindings/go/runtime'],
    }[mod] ?? [];
    return JSON.stringify({ Require: deps.map(d => ({ Path: `ocm.software/open-component-model/${d}` })) });
  };

  const map = buildDependentsMap(modules, '/root', mockExec);

  assert.deepStrictEqual(
    map.get('bindings/go/runtime').sort(),
    ['bindings/go/blob', 'bindings/go/oci'].sort(),
    'runtime has blob and oci as dependents',
  );
  assert.deepStrictEqual(map.get('bindings/go/blob'),    [], 'blob has no dependents');
  assert.deepStrictEqual(map.get('bindings/go/oci'),     [], 'oci has no dependents');

  // External deps must be ignored
  const withExternal = (_cmd, opts) => {
    const mod = opts.cwd.replace('/root/', '');
    if (mod !== 'bindings/go/blob') return JSON.stringify({ Require: [] });
    return JSON.stringify({ Require: [
      { Path: 'ocm.software/open-component-model/bindings/go/runtime' },
      { Path: 'github.com/external/lib' },
    ]});
  };
  const map2 = buildDependentsMap(modules, '/root', withExternal);
  assert.deepStrictEqual(
    map2.get('bindings/go/runtime'),
    ['bindings/go/blob'],
    'external deps do not appear in dependents map',
  );

  // go mod edit failure → module treated as having no deps
  const failingExec = () => { throw new Error('go mod edit failed'); };
  const map3 = buildDependentsMap(modules, '/root', failingExec);
  for (const mod of modules) {
    assert.deepStrictEqual(map3.get(mod), [], `failure: ${mod} has empty dependent list`);
  }
}

// ---------------------------------------------------------------------------
// isConsumerAffected
// ---------------------------------------------------------------------------
console.log('Testing isConsumerAffected...');

{
  const affected = new Set(['bindings/go/oci']);
  const noImports = () => JSON.stringify({ Require: [] });
  const importsOCI = () => JSON.stringify({
    Require: [{ Path: 'ocm.software/open-component-model/bindings/go/oci' }],
  });
  const importsRuntime = () => JSON.stringify({
    Require: [{ Path: 'ocm.software/open-component-model/bindings/go/runtime' }],
  });

  assert.strictEqual(
    isConsumerAffected('cli', affected, ['cli/cmd/main.go'], '/root', noImports),
    true,
    'consumer own code changed → affected',
  );

  assert.strictEqual(
    isConsumerAffected('cli', affected, [], '/root', importsOCI),
    true,
    'consumer imports affected binding → affected',
  );

  assert.strictEqual(
    isConsumerAffected('cli', affected, [], '/root', importsRuntime),
    false,
    'consumer only imports unaffected bindings → not affected',
  );

  assert.strictEqual(
    isConsumerAffected('cli', affected, [], '/root', noImports),
    false,
    'no imports, no file changes → not affected',
  );

  assert.strictEqual(
    isConsumerAffected('cli', affected, [], '/root', () => { throw new Error(); }),
    false,
    'go mod edit failure → not affected (safe fallback)',
  );

  // File change in a different consumer must not trigger this consumer
  assert.strictEqual(
    isConsumerAffected('cli', affected, ['kubernetes/controller/main.go'], '/root', noImports),
    false,
    'file change in different consumer does not affect cli',
  );
}

// ---------------------------------------------------------------------------
// splitByTestability
// ---------------------------------------------------------------------------
console.log('Testing splitByTestability...');

{
  const mockTask = (cmd) => {
    if (cmd.includes('bindings/go/blob'))
      return JSON.stringify({ tasks: [{ name: 'test' }] });
    if (cmd.includes('bindings/go/transfer'))
      return JSON.stringify({ tasks: [{ name: 'test' }, { name: 'test/integration' }] });
    throw new Error('no Taskfile');
  };

  const { unitTestModules, integrationTestModules } = splitByTestability(
    ['bindings/go/blob', 'bindings/go/transfer', 'bindings/go/no-taskfile'],
    mockTask,
  );

  assert.deepStrictEqual(
    unitTestModules, ['bindings/go/blob', 'bindings/go/transfer'],
    'modules with test target in unit list',
  );
  assert.deepStrictEqual(
    integrationTestModules, ['bindings/go/transfer'],
    'only modules with test/integration in integration list',
  );
}

console.log('✅ All discover-modules tests passed.');
