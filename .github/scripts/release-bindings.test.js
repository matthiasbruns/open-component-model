import assert from 'assert';
import {
  bumpSemver,
  tagToVersion,
  topoSort,
  discoverModules,
  internalDepsOf,
  latestVersionTag,
  computeNextTag,
} from './release-bindings.js';

// ----------------------------------------------------------
// Helpers
// ----------------------------------------------------------

/** @param {Record<string, string>} files */
function mockReadFile(files) {
  return (/** @type {string} */ path, /** @type {string} */ _enc) => {
    if (path in files) return files[path];
    throw new Error(`Unexpected readFile path: ${path}`);
  };
}

/** @param {string} output */
function mockGit(output) {
  return (/** @type {string[]} */ _args) => output;
}

// ----------------------------------------------------------
// bumpSemver
// ----------------------------------------------------------
console.log('Testing bumpSemver...');

assert.strictEqual(bumpSemver('0.0.9',     'patch'), '0.0.10', 'patch increments patch');
assert.strictEqual(bumpSemver('0.4.1',     'minor'), '0.5.0',  'minor resets patch');
assert.strictEqual(bumpSemver('0.0.46',    'major'), '1.0.0',  'major resets minor and patch');
assert.strictEqual(bumpSemver('v0.0.9',    'patch'), '0.0.10', 'strips leading v');
assert.strictEqual(bumpSemver('0.0.9-alpha','patch'), '0.0.10', 'strips pre-release before bumping');
assert.strictEqual(bumpSemver('0.9.9',     'minor'), '0.10.0', 'minor carries at 9');
assert.strictEqual(bumpSemver('9.9.9',     'major'), '10.0.0', 'major carries at 9');
assert.strictEqual(bumpSemver('0.0.0',     'patch'), '0.0.1',  'first patch from zero');

// ----------------------------------------------------------
// tagToVersion
// ----------------------------------------------------------
console.log('Testing tagToVersion...');

assert.strictEqual(tagToVersion('bindings/go/oci/v0.0.47'),         'v0.0.47');
assert.strictEqual(tagToVersion('bindings/go/descriptor/v2/v2.0.3'),'v2.0.3');
assert.strictEqual(tagToVersion('bindings/go/runtime/v0.0.8'),       'v0.0.8');
assert.strictEqual(tagToVersion('bindings/go/ctf/v0.4.1'),           'v0.4.1');
assert.strictEqual(tagToVersion('bindings/go/cel/v0.0.1'),           'v0.0.1');

// ----------------------------------------------------------
// topoSort
// ----------------------------------------------------------
console.log('Testing topoSort...');

// Linear chain A ← B ← C (A has no deps, B depends on A, C depends on B)
{
  const modules = ['A', 'B', 'C'];
  const deps    = new Map([['A', []], ['B', ['A']], ['C', ['B']]]);
  const sorted  = topoSort(modules, deps);
  assert.ok(sorted.indexOf('A') < sorted.indexOf('B'), 'A before B');
  assert.ok(sorted.indexOf('B') < sorted.indexOf('C'), 'B before C');
  assert.strictEqual(sorted.length, 3, 'all modules present');
}

// Diamond: B and C both depend on A; D depends on B and C
{
  const modules = ['A', 'B', 'C', 'D'];
  const deps    = new Map([['A', []], ['B', ['A']], ['C', ['A']], ['D', ['B', 'C']]]);
  const sorted  = topoSort(modules, deps);
  assert.ok(sorted.indexOf('A') < sorted.indexOf('B'));
  assert.ok(sorted.indexOf('A') < sorted.indexOf('C'));
  assert.ok(sorted.indexOf('B') < sorted.indexOf('D'));
  assert.ok(sorted.indexOf('C') < sorted.indexOf('D'));
}

// No deps at all — all modules present in any order
{
  const modules = ['A', 'B', 'C'];
  const deps    = new Map([['A', []], ['B', []], ['C', []]]);
  assert.strictEqual(topoSort(modules, deps).length, 3);
}

// Single module
{
  const sorted = topoSort(['A'], new Map([['A', []]]));
  assert.deepStrictEqual(sorted, ['A']);
}

// External dep (not in modules) is silently ignored
{
  const modules = ['A', 'B'];
  const deps    = new Map([['A', ['external-not-in-list']], ['B', ['A']]]);
  const sorted  = topoSort(modules, deps);
  assert.ok(sorted.indexOf('A') < sorted.indexOf('B'));
}

// Cycle → throws with helpful message
{
  const modules = ['A', 'B'];
  const deps    = new Map([['A', ['B']], ['B', ['A']]]);
  assert.throws(() => topoSort(modules, deps), /Cycle detected/);
}

// Three-node cycle
{
  const modules = ['A', 'B', 'C'];
  const deps    = new Map([['A', ['C']], ['B', ['A']], ['C', ['B']]]);
  assert.throws(() => topoSort(modules, deps), /Cycle detected/);
}

// ----------------------------------------------------------
// discoverModules
// ----------------------------------------------------------
console.log('Testing discoverModules...');

// Standard case: includes binding modules, excludes integration and non-binding
{
  const goWork = [
    'go 1.26.4',
    '',
    'use (',
    '\t./bindings/go/blob',
    '\t./bindings/go/http/integration',
    '\t./bindings/go/oci',
    '\t./cli',
    '\t./kubernetes/controller',
    ')',
  ].join('\n');
  const modules = discoverModules('/repo', mockReadFile({ '/repo/go.work': goWork }));
  assert.deepStrictEqual(modules, ['bindings/go/blob', 'bindings/go/oci']);
}

// Nested path (e.g. descriptor/v2) is included
{
  const goWork = 'go 1.26\n\nuse (\n\t./bindings/go/descriptor/v2\n\t./bindings/go/oci/integration\n)\n';
  const modules = discoverModules('/repo', mockReadFile({ '/repo/go.work': goWork }));
  assert.deepStrictEqual(modules, ['bindings/go/descriptor/v2']);
}

// Empty use block
{
  const goWork = 'go 1.26\n\nuse (\n)\n';
  const modules = discoverModules('/repo', mockReadFile({ '/repo/go.work': goWork }));
  assert.deepStrictEqual(modules, []);
}

// ----------------------------------------------------------
// internalDepsOf
// ----------------------------------------------------------
console.log('Testing internalDepsOf...');

// Finds internal deps, ignores external ones
{
  const goMod = [
    'module ocm.software/open-component-model/bindings/go/oci',
    '',
    'go 1.26',
    '',
    'require (',
    '\tocm.software/open-component-model/bindings/go/repository v0.0.9',
    '\tocm.software/open-component-model/bindings/go/runtime v0.0.8',
    '\tgithub.com/external/dep v1.0.0',
    ')',
  ].join('\n');
  const deps = internalDepsOf('/repo', 'bindings/go/oci', mockReadFile({ '/repo/bindings/go/oci/go.mod': goMod }));
  assert.ok(deps.includes('bindings/go/repository'), 'finds repository');
  assert.ok(deps.includes('bindings/go/runtime'),    'finds runtime');
  assert.ok(!deps.some(d => d.startsWith('github.com')), 'excludes external deps');
}

// Module with no internal deps
{
  const goMod = 'module ocm.software/open-component-model/bindings/go/runtime\n\ngo 1.26\n\nrequire github.com/foo/bar v1.0.0\n';
  const deps = internalDepsOf('/repo', 'bindings/go/runtime', mockReadFile({ '/repo/bindings/go/runtime/go.mod': goMod }));
  assert.deepStrictEqual(deps, []);
}

// Deduplicated (same dep appearing as direct + indirect)
{
  const goMod = [
    'module ocm.software/open-component-model/bindings/go/oci',
    '',
    'require (',
    '\tocm.software/open-component-model/bindings/go/runtime v0.0.8',
    '\tocm.software/open-component-model/bindings/go/runtime v0.0.8 // indirect',
    ')',
  ].join('\n');
  const deps = internalDepsOf('/repo', 'bindings/go/oci', mockReadFile({ '/repo/bindings/go/oci/go.mod': goMod }));
  assert.strictEqual(deps.filter(d => d === 'bindings/go/runtime').length, 1, 'deduplicates same dep');
}

// ----------------------------------------------------------
// latestVersionTag
// ----------------------------------------------------------
console.log('Testing latestVersionTag...');

// Returns the first line (highest tag after sort -version:refname)
{
  const git = mockGit('bindings/go/oci/v0.0.46\nbindings/go/oci/v0.0.45\nbindings/go/oci/v0.0.1');
  assert.strictEqual(latestVersionTag('bindings/go/oci', git), 'bindings/go/oci/v0.0.46');
}

// No tags → null
{
  assert.strictEqual(latestVersionTag('bindings/go/oci', mockGit('')), null);
}

// Git throws (no tags, error from git) → null
{
  assert.strictEqual(latestVersionTag('bindings/go/oci', () => { throw new Error('no tags'); }), null);
}

// ----------------------------------------------------------
// computeNextTag
// ----------------------------------------------------------
console.log('Testing computeNextTag...');

// No existing tag → first release
{
  assert.strictEqual(computeNextTag('bindings/go/oci',  'patch', mockGit('')), 'bindings/go/oci/v0.0.1');
  assert.strictEqual(computeNextTag('bindings/go/blob', 'minor', mockGit('')), 'bindings/go/blob/v0.0.1');
}

// Git throws → treated as no tags
{
  const git = () => { throw new Error('no tags'); };
  assert.strictEqual(computeNextTag('bindings/go/oci', 'patch', git), 'bindings/go/oci/v0.0.1');
}

// Existing tag — patch
{
  assert.strictEqual(
    computeNextTag('bindings/go/oci', 'patch', mockGit('bindings/go/oci/v0.0.46')),
    'bindings/go/oci/v0.0.47',
  );
}

// Existing tag — minor
{
  assert.strictEqual(
    computeNextTag('bindings/go/repository', 'minor', mockGit('bindings/go/repository/v0.0.9')),
    'bindings/go/repository/v0.1.0',
  );
}

// Existing tag — major
{
  assert.strictEqual(
    computeNextTag('bindings/go/ctf', 'major', mockGit('bindings/go/ctf/v0.4.1')),
    'bindings/go/ctf/v1.0.0',
  );
}

// Uses only the first (highest) tag when multiple are returned
{
  const git = mockGit('bindings/go/ctf/v0.4.1\nbindings/go/ctf/v0.4.0\nbindings/go/ctf/v0.3.9');
  assert.strictEqual(computeNextTag('bindings/go/ctf', 'patch', git), 'bindings/go/ctf/v0.4.2');
}

// ----------------------------------------------------------
// Integration: real repo go.work → discoverModules + topoSort
// ----------------------------------------------------------
console.log('Testing integration with real repo...');

{
  // Run against the actual workspace to verify the topo-sort is cycle-free
  // and produces a deterministic ordering of all 27 binding modules.
  // This test reads real go.work and go.mod files from the repo.
  const repoRoot = new URL('../..', import.meta.url).pathname.replace(/\/$/, '');

  const { discoverModules: dm, internalDepsOf: deps, topoSort: ts } = await import('./release-bindings.js');

  const modules   = dm(repoRoot);
  const moduleSet = new Set(modules);
  const depsMap   = new Map(modules.map(m => [m, deps(repoRoot, m).filter(d => moduleSet.has(d))]));
  const sorted    = ts(modules, depsMap);

  assert.strictEqual(sorted.length, modules.length, 'all modules in sorted output');

  // Verify ordering invariant: for every module, all its deps appear earlier
  for (const mod of sorted) {
    const modDeps = depsMap.get(mod) ?? [];
    for (const dep of modDeps) {
      assert.ok(
        sorted.indexOf(dep) < sorted.indexOf(mod),
        `${dep} must appear before ${mod}`,
      );
    }
  }

  // Spot-check known ordering constraints from the actual dep graph
  assert.ok(sorted.indexOf('bindings/go/runtime')    < sorted.indexOf('bindings/go/oci'));
  assert.ok(sorted.indexOf('bindings/go/oci')         < sorted.indexOf('bindings/go/transfer'));
  assert.ok(sorted.indexOf('bindings/go/constructor') < sorted.indexOf('bindings/go/helm'));
  assert.ok(sorted.indexOf('bindings/go/helm')        < sorted.indexOf('bindings/go/transfer'));
}

console.log('✅ All release-bindings tests passed.');
