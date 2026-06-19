// mock core intentionally omits unused @actions/core members
// Local dry-run runner for the binding release workflow.
// Calls all four step functions with DRY_RUN=true — no git tags, no pushes,
// no go.mod modifications. Safe to run at any time.
//
// Usage:
//   node .github/scripts/release-bindings-dryrun.js

import { buildGraph, planRelease, pinDeps, publish } from './release-bindings.js';

process.env.GITHUB_WORKSPACE = new URL('../..', import.meta.url).pathname.replace(/\/$/, '');
process.env.DRY_RUN = 'true';

// Minimal mock of @actions/core that prints to stdout and captures outputs.
function makeCore(/** @type {string} */ label) {
  /** @type {Record<string, string>} */
  const outputs = {};
  const prefix  = `\x1b[2m[${label}]\x1b[0m `;
  return {
    info:      (/** @type {string} */ msg) => console.log(prefix + msg),
    warning:   (/** @type {string} */ msg) => console.warn(`\x1b[33m${prefix}${msg}\x1b[0m`),
    setFailed: (/** @type {string} */ msg) => { console.error(`\x1b[31m${prefix}FAILED: ${msg}\x1b[0m`); process.exit(1); },
    setOutput: (/** @type {string} */ key, /** @type {string} */ val) => { outputs[key] = val; },
    summary:   { addHeading: (/** @type {string} */ _h) => ({ addRaw: (/** @type {string} */ _r) => ({ write: async () => {} }) }) },
    _outputs:  outputs,
  };
}

const separator = () => console.log('\n' + '─'.repeat(60));

// ── Step 1: Build dependency graph ──────────────────────────────
separator();
console.log('\x1b[1mStep 1 — Build dependency graph\x1b[0m');
const core1 = makeCore('graph');
await buildGraph({ core: /** @type {any} */ (core1) });
process.env.ORDERED_JSON = core1._outputs.ordered_json;

// ── Step 2: Plan release ─────────────────────────────────────────
separator();
console.log('\x1b[1mStep 2 — Plan release\x1b[0m');
const core2 = makeCore('plan');
await planRelease({ core: /** @type {any} */ (core2) });
process.env.TAGS_JSON = core2._outputs.tags_json;

// ── Step 3: Pin go.mod dependencies (dry-run: no file changes) ───
separator();
console.log('\x1b[1mStep 3 — Pin go.mod dependencies\x1b[0m');
await pinDeps({ core: /** @type {any} */ (makeCore('pin')) });

// ── Step 4: Publish (dry-run: no tags, no push) ──────────────────
separator();
console.log('\x1b[1mStep 4 — Publish\x1b[0m');
await publish({ core: /** @type {any} */ (makeCore('publish')) });

separator();
console.log('\n\x1b[32m✅ Dry-run complete — no files modified, no tags created.\x1b[0m\n');
