import assert from 'assert';
import {
  discoverModules, internalDepsOf, topoSort, groupByLevel,
  bumpVersion, tagToVersion, latestVersionTag, computeNextTag,
  hasChanges, detectBump, pinsFor, resolvePins,
} from './release-bindings.js';

/** @param {string} output  returned for all git calls (tag list, log, etc.) */
const mockGit = (output) => (/** @type {string[]} */ _args) => output;

// ----------------------------------------------------------
// discoverModules
// ----------------------------------------------------------
console.log('Testing discoverModules...');

{
  const repoRoot = new URL('../..', import.meta.url).pathname.replace(/\/$/, '');
  const mods = discoverModules(repoRoot);

  assert.ok(mods.length > 0, 'finds modules');
  assert.ok(mods.every(m => m.startsWith('bindings/go/')), 'all paths are binding paths');
  assert.ok(!mods.some(m => m.endsWith('/integration')), 'no integration test modules');
  assert.ok(!mods.includes('cli'), 'cli excluded');
  assert.ok(!mods.includes('kubernetes/controller'), 'controller excluded');
  assert.ok(mods.includes('bindings/go/oci'), 'spot-check: oci present');
  assert.ok(mods.includes('bindings/go/descriptor/v2'), 'spot-check: nested path present');
}

// ----------------------------------------------------------
// internalDepsOf
// ----------------------------------------------------------
console.log('Testing internalDepsOf...');

{
  const repoRoot = new URL('../..', import.meta.url).pathname.replace(/\/$/, '');

  // oci has several known binding deps
  const ociDeps = internalDepsOf(repoRoot, 'bindings/go/oci');
  assert.ok(ociDeps.includes('bindings/go/repository'), 'oci depends on repository');
  assert.ok(ociDeps.includes('bindings/go/runtime'),    'oci depends on runtime');
  assert.ok(!ociDeps.some(d => d.startsWith('github.com')), 'no external deps returned');

  // runtime is a leaf — it has no binding deps
  const runtimeDeps = internalDepsOf(repoRoot, 'bindings/go/runtime');
  assert.deepStrictEqual(runtimeDeps, [], 'runtime has no internal binding deps');
}

// ----------------------------------------------------------
// topoSort
// ----------------------------------------------------------
console.log('Testing topoSort...');

{
  const s = topoSort(['A', 'B', 'C'], new Map([['A', []], ['B', ['A']], ['C', ['B']]]));
  assert.ok(s.indexOf('A') < s.indexOf('B') && s.indexOf('B') < s.indexOf('C'));
}
{
  // Diamond
  const s = topoSort(['A', 'B', 'C', 'D'], new Map([['A', []], ['B', ['A']], ['C', ['A']], ['D', ['B', 'C']]]));
  assert.ok(s.indexOf('A') < s.indexOf('D') && s.indexOf('B') < s.indexOf('D') && s.indexOf('C') < s.indexOf('D'));
}
assert.throws(() => topoSort(['A', 'B'], new Map([['A', ['B']], ['B', ['A']]])), /Cycle/);
assert.deepStrictEqual(topoSort(['A'], new Map([['A', []]])), ['A']);

// ----------------------------------------------------------
// groupByLevel
// ----------------------------------------------------------
console.log('Testing groupByLevel...');

{
  // runtime ← blob, configuration
  // blob, configuration ← ctf
  const depsMap = new Map([
    ['bindings/go/runtime',       []],
    ['bindings/go/blob',          ['bindings/go/runtime']],
    ['bindings/go/configuration', ['bindings/go/runtime']],
    ['bindings/go/ctf',           ['bindings/go/blob', 'bindings/go/configuration']],
  ]);
  const ordered = topoSort([...depsMap.keys()], depsMap);
  const groups  = groupByLevel(ordered, depsMap);

  // level 0: runtime (no deps)
  assert.deepStrictEqual(groups[0], ['bindings/go/runtime'], 'level 0: root only');

  // level 1: blob and configuration (both only depend on runtime)
  assert.deepStrictEqual(groups[1].sort(), ['bindings/go/blob', 'bindings/go/configuration'].sort(),
    'level 1: direct deps of root');

  // level 2: ctf (depends on level-1 modules)
  assert.deepStrictEqual(groups[2], ['bindings/go/ctf'], 'level 2: downstream');

  // No module appears in more than one group
  const flat = groups.flat();
  assert.strictEqual(flat.length, new Set(flat).size, 'no duplicates across groups');
  assert.strictEqual(flat.length, depsMap.size, 'all modules present');
}

{
  // Independent modules with no deps → all in level 0
  const depsMap = new Map([['A', []], ['B', []], ['C', []]]);
  const ordered = topoSort([...depsMap.keys()], depsMap);
  const groups  = groupByLevel(ordered, depsMap);
  assert.strictEqual(groups.length, 1, 'all independent → single group');
  assert.deepStrictEqual(groups[0].sort(), ['A', 'B', 'C'].sort(), 'all in level 0');
}

{
  // Linear chain A ← B ← C ← D → each at its own level
  const depsMap = new Map([['A', []], ['B', ['A']], ['C', ['B']], ['D', ['C']]]);
  const ordered = topoSort([...depsMap.keys()], depsMap);
  const groups  = groupByLevel(ordered, depsMap);
  assert.strictEqual(groups.length, 4, 'linear chain: one module per level');
  assert.deepStrictEqual(groups.map(g => g[0]), ['A', 'B', 'C', 'D'], 'correct linear order');
}

{
  // Diamond: A ← B, A ← C, B+C ← D
  // D depends on B (level 1) and C (level 1), so D is level 2
  const depsMap = new Map([['A', []], ['B', ['A']], ['C', ['A']], ['D', ['B', 'C']]]);
  const ordered = topoSort([...depsMap.keys()], depsMap);
  const groups  = groupByLevel(ordered, depsMap);
  assert.deepStrictEqual(groups[0], ['A'],       'diamond: root at level 0');
  assert.deepStrictEqual(groups[1].sort(), ['B', 'C'].sort(), 'diamond: middle at level 1');
  assert.deepStrictEqual(groups[2], ['D'],       'diamond: tip at level 2');
}

// ----------------------------------------------------------
// bumpVersion
// ----------------------------------------------------------
console.log('Testing bumpVersion...');

assert.strictEqual(bumpVersion('0.0.9',       'patch'), '0.0.10');
assert.strictEqual(bumpVersion('0.4.1',       'minor'), '0.5.0');
assert.strictEqual(bumpVersion('0.0.46',      'major'), '1.0.0');
assert.strictEqual(bumpVersion('v0.0.9',      'patch'), '0.0.10', 'strips v');
assert.strictEqual(bumpVersion('0.0.9-alpha', 'patch'), '0.0.10', 'strips pre-release');
assert.strictEqual(bumpVersion('0.9.9',       'minor'), '0.10.0', 'carries');

// ----------------------------------------------------------
// tagToVersion
// ----------------------------------------------------------
console.log('Testing tagToVersion...');

assert.strictEqual(tagToVersion('bindings/go/oci/v0.0.47'),          'v0.0.47');
assert.strictEqual(tagToVersion('bindings/go/descriptor/v2/v2.0.3'), 'v2.0.3');

// ----------------------------------------------------------
// latestVersionTag / computeNextTag
// ----------------------------------------------------------
console.log('Testing latestVersionTag / computeNextTag...');

assert.strictEqual(latestVersionTag('m', mockGit('m/v0.0.46')),           'm/v0.0.46');
assert.strictEqual(latestVersionTag('m', mockGit('')),                     null);
assert.strictEqual(latestVersionTag('m', () => { throw new Error(); }),    null, 'git error → null');

// No existing tag → null (pinned via pseudo-version instead)
assert.strictEqual(computeNextTag('bindings/go/cel',  'patch', mockGit('')), null);
assert.strictEqual(computeNextTag('bindings/go/cel',  'minor', mockGit('')), null);

// Already tagged → bump normally
assert.strictEqual(computeNextTag('bindings/go/oci', 'patch', mockGit('bindings/go/oci/v0.0.46')), 'bindings/go/oci/v0.0.47');
assert.strictEqual(computeNextTag('bindings/go/oci', 'minor', mockGit('bindings/go/oci/v0.0.46')), 'bindings/go/oci/v0.1.0');
// mock returns tags in ascending order (as git --sort=version:refname does)
assert.strictEqual(computeNextTag('bindings/go/ctf', 'patch', mockGit('bindings/go/ctf/v0.4.0\nbindings/go/ctf/v0.4.1')), 'bindings/go/ctf/v0.4.2');
// Previously pseudo-versioned → graduated to proper semver on next release
assert.strictEqual(computeNextTag('bindings/go/cel', 'patch', mockGit('bindings/go/cel/v0.0.0-20260101000000-abc123def456')), 'bindings/go/cel/v0.0.1');

// ----------------------------------------------------------
// hasChanges
// ----------------------------------------------------------
console.log('Testing hasChanges...');

// No last tag → always true (first release)
assert.strictEqual(hasChanges('bindings/go/cel', null, () => ''), true);

// Empty git log → no changes
assert.strictEqual(hasChanges('bindings/go/oci', 'bindings/go/oci/v0.0.46',
  args => args.includes('--oneline') ? '' : ''), false);

// Non-empty git log → has changes
assert.strictEqual(hasChanges('bindings/go/oci', 'bindings/go/oci/v0.0.46',
  args => args.includes('--oneline') ? 'abc1234 fix: something' : ''), true);

// Whitespace-only output → treated as no changes
assert.strictEqual(hasChanges('bindings/go/oci', 'bindings/go/oci/v0.0.46',
  args => args.includes('--oneline') ? '   \n  ' : ''), false);

// git error → safe default: treat as changed (don't skip on failure)
assert.strictEqual(hasChanges('bindings/go/oci', 'bindings/go/oci/v0.0.46',
  () => { throw new Error('git failed'); }), true);

// ----------------------------------------------------------
// detectBump
// ----------------------------------------------------------
console.log('Testing detectBump...');

// No last tag → always patch (module uses pseudo-version, bump kind irrelevant)
assert.strictEqual(detectBump('bindings/go/cel', null, () => ''), 'patch');

// Normal commits → patch
assert.strictEqual(detectBump('bindings/go/oci', 'bindings/go/oci/v0.0.46', mockGit('fix: correct nil pointer\nchore: tidy deps')), 'patch');

// Conventional Commit type!: subject → minor
assert.strictEqual(detectBump('bindings/go/oci', 'bindings/go/oci/v0.0.46', mockGit('feat!: remove deprecated API')), 'minor');
assert.strictEqual(detectBump('bindings/go/oci', 'bindings/go/oci/v0.0.46', mockGit('fix!: change error type')),   'minor');
assert.strictEqual(detectBump('bindings/go/oci', 'bindings/go/oci/v0.0.46', mockGit('chore!: drop Go 1.21')),     'minor');

// BREAKING CHANGE footer → minor
assert.strictEqual(detectBump('bindings/go/oci', 'bindings/go/oci/v0.0.46', mockGit('feat: new thing\n\nBREAKING CHANGE: old API removed')), 'minor');
assert.strictEqual(detectBump('bindings/go/oci', 'bindings/go/oci/v0.0.46', mockGit('BREAKING-CHANGE: behaviour changed')), 'minor');

// Case-insensitive
assert.strictEqual(detectBump('bindings/go/oci', 'bindings/go/oci/v0.0.46', mockGit('breaking change: something')), 'minor');

// git error → safe fallback to patch
assert.strictEqual(detectBump('bindings/go/oci', 'bindings/go/oci/v0.0.46', () => { throw new Error(); }), 'patch');

// ----------------------------------------------------------
// pinsFor
// ----------------------------------------------------------
console.log('Testing pinsFor...');

{
  const ordered = ['A', 'B', 'C'];
  const tags    = { A: 'bindings/go/a/v0.1.0', B: 'bindings/go/b/v0.2.0', C: 'bindings/go/c/v0.3.0' };
  const getDeps = (/** @type {string} */ m) => ({ A: [], B: ['A'], C: ['A', 'B'] }[m] ?? []);
  const pins    = pinsFor(ordered, tags, getDeps);

  assert.ok(!pins.has('A'), 'leaf has no pins');
  assert.strictEqual(pins.get('B')?.length, 1);
  assert.strictEqual(pins.get('B')?.[0].name,    'ocm.software/open-component-model/A');
  assert.strictEqual(pins.get('B')?.[0].version, 'v0.1.0');
  assert.strictEqual(pins.get('C')?.length, 2);
}
{
  // External dep not in tags → not pinned
  const pins = pinsFor(['A', 'B'], { A: 'bindings/go/a/v0.1.0' }, () => ['A', 'external']);
  assert.strictEqual(pins.get('B')?.length, 1, 'only released dep is pinned');
}

// ----------------------------------------------------------
// resolvePins — all paths
// ----------------------------------------------------------
console.log('Testing resolvePins...');

{
  const ordered   = ['bindings/go/runtime', 'bindings/go/oci', 'bindings/go/transfer'];
  const tags      = { 'bindings/go/runtime': 'bindings/go/runtime/v0.0.9' };
  const consumers = ['cli'];

  // dep released this run → pinned to new tag
  {
    const pins = resolvePins(ordered, tags, [], mod => mod === 'bindings/go/oci' ? ['bindings/go/runtime'] : [], () => null);
    assert.deepStrictEqual(pins.get('bindings/go/oci'), [
      { name: 'ocm.software/open-component-model/bindings/go/runtime', version: 'v0.0.9' },
    ], 'released dep pinned to new tag');
  }

  // dep skipped but has existing tag → pinned to latest tag
  {
    const latestTags = { 'bindings/go/oci': 'bindings/go/oci/v0.0.47' };
    const pins = resolvePins(ordered, {}, [], mod => mod === 'bindings/go/transfer' ? ['bindings/go/oci'] : [], mod => latestTags[mod] ?? null);
    assert.deepStrictEqual(pins.get('bindings/go/transfer'), [
      { name: 'ocm.software/open-component-model/bindings/go/oci', version: 'v0.0.47' },
    ], 'skipped dep pinned to latest existing tag');
  }

  // new module with no tag at all → skipped entirely
  {
    const pins = resolvePins(ordered, {}, [], mod => mod === 'bindings/go/transfer' ? ['bindings/go/oci'] : [], () => null);
    assert.ok(!pins.has('bindings/go/transfer'), 'new untagged dep produces no pin');
  }

  // external dep (not in ordered) → ignored
  {
    const pins = resolvePins(ordered, tags, [], () => ['github.com/external/lib'], () => null);
    assert.strictEqual(pins.size, 0, 'external dep not pinned');
  }

  // module with no binding deps → absent from result
  {
    const pins = resolvePins(ordered, tags, [], () => [], () => null);
    assert.strictEqual(pins.size, 0, 'module with no deps absent from result');
  }

  // consumer gets both released and skipped deps pinned
  {
    const latestTags = { 'bindings/go/oci': 'bindings/go/oci/v0.0.47' };
    const getDeps = mod => mod === 'cli' ? ['bindings/go/runtime', 'bindings/go/oci'] : [];
    const pins = resolvePins(ordered, tags, consumers, getDeps, mod => latestTags[mod] ?? null);
    const cliPins = pins.get('cli') ?? [];
    assert.strictEqual(cliPins.length, 2, 'consumer gets all binding deps pinned');
    assert.ok(cliPins.some(p => p.name.endsWith('runtime') && p.version === 'v0.0.9'), 'released dep');
    assert.ok(cliPins.some(p => p.name.endsWith('oci')     && p.version === 'v0.0.47'), 'skipped dep');
  }

  // released dep takes precedence over any existing latestTag
  {
    const latestTags = { 'bindings/go/runtime': 'bindings/go/runtime/v0.0.8' };
    const pins = resolvePins(ordered, tags, [], mod => mod === 'bindings/go/oci' ? ['bindings/go/runtime'] : [], mod => latestTags[mod] ?? null);
    assert.deepStrictEqual(pins.get('bindings/go/oci')?.[0].version, 'v0.0.9', 'new tag beats latestTag');
  }
}

// ----------------------------------------------------------
// Integration: real repo go.work + go.mod files
// ----------------------------------------------------------
console.log('Testing integration with real repo...');

{
  const repoRoot  = new URL('../..', import.meta.url).pathname.replace(/\/$/, '');
  const modules   = discoverModules(repoRoot);
  const moduleSet = new Set(modules);
  const depsMap   = new Map(modules.map(m => [m, internalDepsOf(repoRoot, m).filter(d => moduleSet.has(d))]));
  const ordered   = topoSort(modules, depsMap);

  assert.strictEqual(ordered.length, modules.length, 'all modules present');

  for (const mod of ordered) {
    for (const dep of (depsMap.get(mod) ?? [])) {
      assert.ok(ordered.indexOf(dep) < ordered.indexOf(mod), `${dep} before ${mod}`);
    }
  }

  const idx = (/** @type {string} */ m) => ordered.indexOf(m);
  assert.ok(idx('bindings/go/runtime')    < idx('bindings/go/oci'));
  assert.ok(idx('bindings/go/oci')         < idx('bindings/go/transfer'));
  assert.ok(idx('bindings/go/constructor') < idx('bindings/go/helm'));
}

console.log('✅ All release-bindings tests passed.');
