// Compare the locked direct dependencies with official stable npm releases.
// Aliases (including TypeScript's compiler-API bridge) are checked by real name.
import { readFile } from 'node:fs/promises';
import semver from 'semver';

const root = new URL('../', import.meta.url);
const pkg = JSON.parse(await readFile(new URL('package.json', root), 'utf8'));
const lock = JSON.parse(await readFile(new URL('package-lock.json', root), 'utf8'));
const inputs = Object.entries({ ...pkg.dependencies, ...pkg.devDependencies });
const rows = [];

async function metadata(name, suffix = '/latest') {
  const response = await fetch(`https://registry.npmjs.org/${encodeURIComponent(name)}${suffix}`, { signal: AbortSignal.timeout(30_000) });
  if (!response.ok) { throw new Error(`${name}: registry returned ${response.status}`); }
  return response.json();
}

async function worker() {
  while (inputs.length) {
    const [name, declared] = inputs.shift();
    const actual = declared.match(/^npm:(.+)@[^@]+$/)?.[1] || name;
    const current = lock.packages[`node_modules/${name}`]?.version;
    if (!current) { throw new Error(`${name}: missing from package-lock.json`); }
    const info = await metadata(actual);
    let latest = info.version;
    let note = actual === name ? '' : `alias → ${actual}`;
    if (semver.prerelease(latest)) {
      const all = await metadata(actual, '');
      latest = Object.keys(all.versions).filter(v => semver.valid(v) && !semver.prerelease(v)).sort(semver.rcompare)[0];
      note = `${note} latest tag is prerelease; comparing stable releases`.trim();
    }
    rows.push({ package: name, locked: current, stable: latest, status: current === latest ? 'current' : 'UPDATE', note });
  }
}

await Promise.all(Array.from({ length: 6 }, worker));
rows.sort((a, b) => a.package.localeCompare(b.package));
console.log(rows.map(row => `${row.package}: ${row.locked} / stable ${row.stable} [${row.status}] ${row.note}`).join('\n'));
const updates = rows.filter(row => row.status === 'UPDATE');
console.log(`${new Date().toISOString()}: ${rows.length} direct dependencies checked; ${updates.length} updates. Transitive security: npm audit.`);
process.exitCode = updates.length ? 1 : 0;
