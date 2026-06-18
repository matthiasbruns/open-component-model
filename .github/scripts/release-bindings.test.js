import assert from 'assert';
import {
  discoverModules, internalDepsOf, topoSort,
  bumpSemver, tagToVersion, latestVersionTag, computeNextTag, pseudoVersion,
  detectBump, pinsFor,
} from './release-bindings.js';

const mockReadFile = (/** @type {Record<string, string>} */ files) =>
  (/** @type {string} */ p) => {
    if (p in files) return files[p];
    throw new Error(`Unexpected path: ${p}`);
  };

/**
 * @param {string} tagOutput   returned for `git tag --list`
 * @param {string} [headHash]  returned for `git rev-parse HEAD`
 * @param {string} [headTs]    returned for `git log -1 --format=%ct` (unix seconds)
 */
const mockGit = (tagOutput, headHash = 'aabbccddeeff', headTs = '1750000000') =>
  (/** @type {string[]} */ args) => {
    if (args.includes('rev-parse')) return headHash;
    if (args.includes('%ct'))       return headTs;   // timestamp-only log format
    return tagOutput;                                // tag list + commit-message log
  };

// ----------------------------------------------------------
// discoverModules
// ----------------------------------------------------------
console.log('Testing discoverModules...');

{
  const goWork = 'go 1.26\n\nuse (\n\t./bindings/go/blob\n\t./bindings/go/oci/integration\n\t./bindings/go/oci\n\t./cli\n)\n';
  assert.deepStrictEqual(
    discoverModules('/r', mockReadFile({ '/r/go.work': goWork })),
    ['bindings/go/blob', 'bindings/go/oci'],
    'excludes integration and non-bindings',
  );
}
{
  const goWork = 'go 1.26\n\nuse (\n)\n';
  assert.deepStrictEqual(discoverModules('/r', mockReadFile({ '/r/go.work': goWork })), []);
}

// ----------------------------------------------------------
// internalDepsOf
// ----------------------------------------------------------
console.log('Testing internalDepsOf...');

{
  const goMod = 'module ocm.software/open-component-model/bindings/go/oci\n\nrequire (\n\tocm.software/open-component-model/bindings/go/repository v0.0.9\n\tocm.software/open-component-model/bindings/go/runtime v0.0.8\n\tgithub.com/external v1.0.0\n)\n';
  const deps  = internalDepsOf('/r', 'bindings/go/oci', mockReadFile({ '/r/bindings/go/oci/go.mod': goMod }));
  assert.ok(deps.includes('bindings/go/repository'));
  assert.ok(deps.includes('bindings/go/runtime'));
  assert.ok(!deps.some(d => d.startsWith('github.com')), 'no external deps');
}
{
  // Deduplication
  const goMod = 'module m\n\nrequire (\n\tocm.software/open-component-model/bindings/go/runtime v0.0.8\n\tocm.software/open-component-model/bindings/go/runtime v0.0.8 // indirect\n)\n';
  const deps  = internalDepsOf('/r', 'x', mockReadFile({ '/r/x/go.mod': goMod }));
  assert.strictEqual(deps.filter(d => d === 'bindings/go/runtime').length, 1);
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
// bumpSemver
// ----------------------------------------------------------
console.log('Testing bumpSemver...');

assert.strictEqual(bumpSemver('0.0.9',       'patch'), '0.0.10');
assert.strictEqual(bumpSemver('0.4.1',       'minor'), '0.5.0');
assert.strictEqual(bumpSemver('0.0.46',      'major'), '1.0.0');
assert.strictEqual(bumpSemver('v0.0.9',      'patch'), '0.0.10', 'strips v');
assert.strictEqual(bumpSemver('0.0.9-alpha', 'patch'), '0.0.10', 'strips pre-release');
assert.strictEqual(bumpSemver('0.9.9',       'minor'), '0.10.0', 'carries');

// ----------------------------------------------------------
// tagToVersion
// ----------------------------------------------------------
console.log('Testing tagToVersion...');

assert.strictEqual(tagToVersion('bindings/go/oci/v0.0.47'),          'v0.0.47');
assert.strictEqual(tagToVersion('bindings/go/descriptor/v2/v2.0.3'), 'v2.0.3');

// ----------------------------------------------------------
// pseudoVersion
// ----------------------------------------------------------
console.log('Testing pseudoVersion...');

{
  const pv = pseudoVersion(mockGit('', 'abc123def456', '1750000000'));
  // 1750000000 Unix → 2025-06-16T…; check structural format
  assert.match(pv, /^v0\.0\.0-\d{14}-[0-9a-f]{12}$/, 'pseudo-version format');
  assert.ok(pv.endsWith('-abc123def456'), 'embeds commit hash');
}
{
  // Hash is truncated to 12 chars
  const pv = pseudoVersion(mockGit('', 'aabbccddeeff0011', '1750000000'));
  assert.ok(pv.endsWith('-aabbccddeeff'), 'truncates hash to 12 chars');
}

// ----------------------------------------------------------
// latestVersionTag / computeNextTag
// ----------------------------------------------------------
console.log('Testing latestVersionTag / computeNextTag...');

assert.strictEqual(latestVersionTag('m', mockGit('m/v0.0.46')),           'm/v0.0.46');
assert.strictEqual(latestVersionTag('m', mockGit('')),                     null);
assert.strictEqual(latestVersionTag('m', () => { throw new Error(); }),    null, 'git error → null');

// No existing tag → pseudo-version (not v0.0.1)
{
  const git = mockGit('', 'abc123def456', '1750000000');
  const tag = computeNextTag('bindings/go/cel', 'patch', git);
  assert.ok(tag.startsWith('bindings/go/cel/v0.0.0-'), 'untagged module gets pseudo-version');
  assert.ok(tag.endsWith('-abc123def456'), 'pseudo-version embeds commit hash');
  assert.ok(!tag.includes('v0.0.1'), 'does not fall back to v0.0.1');
}
// Already tagged → bump normally
assert.strictEqual(computeNextTag('bindings/go/oci', 'patch', mockGit('bindings/go/oci/v0.0.46')), 'bindings/go/oci/v0.0.47');
assert.strictEqual(computeNextTag('bindings/go/oci', 'minor', mockGit('bindings/go/oci/v0.0.46')), 'bindings/go/oci/v0.1.0');
assert.strictEqual(computeNextTag('bindings/go/oci', 'major', mockGit('bindings/go/oci/v0.0.46')), 'bindings/go/oci/v1.0.0');
// Uses highest tag when multiple returned
assert.strictEqual(computeNextTag('bindings/go/ctf', 'patch', mockGit('bindings/go/ctf/v0.4.1\nbindings/go/ctf/v0.4.0')), 'bindings/go/ctf/v0.4.2');
// Previously pseudo-versioned → next release bumps to v0.0.1
assert.strictEqual(computeNextTag('bindings/go/cel', 'patch', mockGit('bindings/go/cel/v0.0.0-20260101000000-abc123def456')), 'bindings/go/cel/v0.0.1');

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
