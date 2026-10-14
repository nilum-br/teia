// Regenerates the README screenshots from a running lab.
//
//   docker compose -f lab/compose.yaml up -d --build      # wait a minute for a few rounds
//   node scripts/screenshots.mjs                          # Node 22+, Chrome, Chromium or Edge
//
// Pages are captured over the DevTools protocol rather than with
// --screenshot: that flag either waits forever on the live stream or cancels
// requests still in flight. Motion is reduced so cards are not caught mid-fade.
// Set BROWSER to the browser's path if it is not found.
import { spawn } from 'node:child_process';
import { existsSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const teia = process.env.TEIA_URL || 'http://localhost:8480';
const grafana = process.env.GRAFANA_URL || 'http://localhost:3480';

const SHOTS = [
  { file: 'docs/assets/screenshots/teia-map.png', url: `${teia}/?theme=dark&flow=1`, width: 1600, height: 960 },
  { file: 'docs/assets/screenshots/teia-details.png', url: `${teia}/?theme=dark&flow=1&select=recommendations`, width: 1600, height: 960 },
  { file: 'docs/assets/screenshots/teia-groups-light.png', url: `${teia}/?theme=light&flow=1&group=team`, width: 1600, height: 960 },
  { file: 'docs/assets/screenshots/teia-queue.png', url: `${teia}/?theme=dark&flow=1&select=${encodeURIComponent('email-worker->order-events')}`, width: 1600, height: 960 },
  { file: 'docs/assets/screenshots/teia-journey.png', url: `${teia}/?theme=dark&flow=1&journey=checkout`, width: 1600, height: 960 },
  { file: 'docs/assets/screenshots/teia-cache-light.png', url: `${teia}/?theme=light&flow=1&select=catalog-cache`, width: 1600, height: 960 },
  { file: 'docs/assets/screenshots/grafana-panel.png', url: `${grafana}/d/teia-overview?orgId=1&kiosk&theme=dark&viewPanel=1`, width: 1600, height: 900, wait: 15000 },
];

const CANDIDATES = [
  process.env.BROWSER,
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
  '/usr/bin/microsoft-edge',
].filter(Boolean);
const browser = CANDIDATES.find(p => existsSync(p));
if (!browser) {
  console.error('No Chrome, Chromium or Edge found; set BROWSER to its path.');
  process.exit(1);
}

const sleep = ms => new Promise(r => setTimeout(r, ms));

async function capture({ file, url, width, height, wait = 9000 }) {
  const port = 9300 + Math.floor(Math.random() * 600);
  const profile = mkdtempSync(join(tmpdir(), 'teia-shot-'));
  const proc = spawn(browser, ['--headless=new', '--disable-gpu', '--hide-scrollbars', `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, `--window-size=${width},${height}`, 'about:blank'], { stdio: 'ignore' });
  try {
    let targets;
    for (let i = 0; i < 50 && !targets; i++) {
      try { targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json(); } catch { await sleep(200); }
    }
    const page = targets?.find(t => t.type === 'page');
    if (!page) throw new Error('the browser did not open a page');
    const ws = new WebSocket(page.webSocketDebuggerUrl);
    await new Promise((ok, fail) => { ws.addEventListener('open', ok); ws.addEventListener('error', fail); });
    let id = 0;
    const pending = new Map();
    ws.addEventListener('message', ev => {
      const m = JSON.parse(ev.data);
      if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); }
    });
    const send = (method, params = {}) => new Promise(r => { const i = ++id; pending.set(i, r); ws.send(JSON.stringify({ id: i, method, params })); });
    await send('Page.enable');
    await send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: false });
    await send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-reduced-motion', value: 'reduce' }] });
    await send('Page.navigate', { url });
    await sleep(wait);
    const shot = await send('Page.captureScreenshot', { format: 'png' });
    if (!shot.result?.data) throw new Error(JSON.stringify(shot.error || shot));
    writeFileSync(join(root, file), Buffer.from(shot.result.data, 'base64'));
    ws.close();
    console.log(`${file}  <-  ${url}`);
  } finally {
    proc.kill();
    await sleep(300);
    rmSync(profile, { recursive: true, force: true, maxRetries: 3 });
  }
}

// A headless browser now and then fails to start or to answer; one more try
// is usually enough.
for (const shot of SHOTS) {
  try {
    await capture(shot);
  } catch (err) {
    console.warn(`retrying ${shot.file}: ${err.message}`);
    await capture(shot);
  }
}
