// @ts-check
// Shared version computation for Go submodule releases.
// Used by release-go-submodule.yaml (manual per-module releases) and
// release-bindings.js (automated bulk releases).

import {execFileSync} from 'child_process';

/** @param {string[]} args */
function defaultGit(args) {
    return /** @type {string} */ (execFileSync('git', args, {encoding: 'utf-8', stdio: 'pipe'})).trim();
}

/**
 * Resolve the tag path prefix for a module.
 * Major-versioned modules (e.g. bindings/go/descriptor/v2) use the parent
 * path as their tag prefix (bindings/go/descriptor/v), matching the
 * convention established by release-go-submodule.yaml.
 *
 * @param {string} modPath  e.g. "bindings/go/descriptor/v2"
 * @returns {string}        e.g. "bindings/go/descriptor/v"
 */
export function tagPrefix(modPath) {
    return `${modPath.replace(/\/v\d+$/, '')}/v`;
}

/**
 * Find the latest semver tag for a module, or null if none exist.
 *
 * @param {string} modPath
 * @param {(args: string[]) => string} [execGit]
 * @returns {string|null}
 */
export function latestTag(modPath, execGit = defaultGit) {
    const prefix = tagPrefix(modPath);
    try {
        const tags = execGit(['tag', '--list', `${prefix}[0-9]*`, '--sort=version:refname'])
            .split('\n').filter(Boolean);
        return tags.at(-1) ?? null;
    } catch {
        return null;
    }
}

/**
 * Bump a semver version string by the given kind.
 * Pre-release suffixes are stripped before bumping ("2.0.3-alpha3" → "2.0.4").
 * 'none' returns the clean version unchanged.
 *
 * @param {string} version  e.g. "0.4.1" or "2.0.3-alpha3"
 * @param {'patch'|'minor'|'major'|'none'} bump
 * @returns {string}
 */
export function bumpVersion(version, bump) {
    const clean = version.replace(/^v/, '').split('-')[0];
    const [maj, min, pat] = clean.split('.').map(n => parseInt(n, 10) || 0);
    if (bump === 'major') return `${maj + 1}.0.0`;
    if (bump === 'minor') return `${maj}.${min + 1}.0`;
    if (bump === 'patch') return `${maj}.${min}.${pat + 1}`;
    return clean; // 'none'
}

/**
 * Compute the next tag for a module.
 *
 * For first releases, the starting version is inferred from the module path:
 * a path ending in /vN starts at N.0.0 (so a patch bump yields N.0.1).
 *
 * @param {string} modPath
 * @param {'patch'|'minor'|'major'|'none'} bump
 * @param {{suffix?: string, execGit?: (args: string[]) => string}} [opts]
 * @returns {string}
 */
export function computeNextTag(modPath, bump, {suffix = '', execGit = defaultGit} = {}) {
    const prefix = tagPrefix(modPath);
    const latest = latestTag(modPath, execGit);

    let version;
    if (!latest) {
        const majorVersion = modPath.match(/\/v(\d+)$/)?.[1];
        const base = majorVersion ? `${majorVersion}.0.0` : '0.0.0';
        version = bumpVersion(base, bump === 'none' ? 'patch' : bump);
    } else {
        version = bumpVersion(latest.replace(prefix, ''), bump);
    }

    return `${prefix}${version}${suffix ? `-${suffix}` : ''}`;
}
