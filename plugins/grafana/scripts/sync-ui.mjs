// The map component lives once, in internal/api/ui/teia-ui.js (served by
// teia itself). The panel bundles a copy taken right before every build.
import { copyFileSync, mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const from = resolve(here, '../../../internal/api/ui/teia-ui.js');
const to = resolve(here, '../src/teia/teia-ui.js');
mkdirSync(dirname(to), { recursive: true });
copyFileSync(from, to);
console.log(`synced ${from} -> ${to}`);
