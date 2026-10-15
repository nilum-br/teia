import type { Page } from '@playwright/test';
import { resolve } from 'node:path';
import type { TeiaController, TeiaMountOptions, TeiaSnapshot } from '../src/teia/teia-ui';

export type FixtureWindow = Window & {
  TeiaUI: { mount(host: HTMLElement, options: TeiaMountOptions): TeiaController };
  fixture: TeiaController;
  fixtureSnapshot: TeiaSnapshot;
};

export function graphFixture(count = 14): TeiaSnapshot {
  const at = '2026-10-15T12:00:00Z';
  const nodes: TeiaSnapshot['nodes'] = Array.from({ length: count }, (_, i) => ({
    id: `service-${String(i).padStart(4, '0')}`, kind: 'service', health: i % 7 === 0 ? 'failing' : 'healthy', visibility: 'full',
    labels: { cluster: 'cluster-' + i % 2, 'k8s.namespace': 'namespace-' + i % 5, 'service.name': 'service-' + i % 20, journey: i < count / 2 ? 'checkout' : 'browse' },
    requestRate: 10, errorRate: i % 7 === 0 ? 0.1 : 0, sources: ['metrics'],
    reasons: i % 7 === 0 ? [{ cause: 'saturation', health: 'failing', what: 'CPU used', value: 0.95, limit: 0.9, unit: 'share', summary: 'CPU saturation above 90%' }] : [],
    measures: [{ cause: 'saturation', what: 'CPU used', value: i % 7 === 0 ? 0.95 : 0.3, max: 1 }],
    evidence: [{ source: 'metrics', signal: 'metrics', collectedAt: at, evaluatedAt: at, oldestSampleAt: '2026-10-15T11:59:50Z', windowSeconds: 300, metrics: ['errors', 'latency'] }],
  }));
  // Ten layers, two callers per node after the first layer; 180/900/1800 edges.
  const edges: TeiaSnapshot['edges'] = [];
  const width = Math.ceil(count / 10);
  for (let i = width; i < count; i++) for (const delta of [0, 1]) {
    const from = (Math.floor(i / width) - 1) * width + (i + delta) % width;
    edges.push({ from: nodes[from].id, to: nodes[i].id, state: 'ok', health: 'healthy', declared: true, observed: true,
      requestRate: 10, errorRate: 0, signals: ['metrics'], sources: ['metrics'], latencyP95Ms: 30 });
  }
  return { round: 1, generatedAt: at, timeMode: 'current', nodes, edges, findings: [], sources: [],
    coverage: { nodes: count, edges: edges.length, visibility: 1, declared: 1, observed: 1 } };
}

export async function mountFixture(page: Page, snapshot: TeiaSnapshot, options: TeiaMountOptions = {}) {
  page.on('pageerror', error => { console.error(error.message); });
  page.on('console', msg => { if (msg.type() === 'error') console.error(msg.text()); });
  const base = process.env.TEIA_URL || 'http://localhost:8480';
  await page.route('**/teia-ui.js', route => route.fulfill({ path: resolve('../../internal/api/ui/teia-ui.js'), contentType: 'text/javascript; charset=utf-8' }));
  await page.route('**/__ui-fixture*', route => route.fulfill({ contentType: 'text/html', body:
    '<html><meta charset="utf-8"><style>html,body{margin:0;height:100%}#fixture{height:100%}</style><div id="fixture"></div><script src="/teia-ui.js"></script></html>' }));
  await page.goto(base + '/__ui-fixture');
  await page.evaluate(({ snapshot, options }) => {
    const w = window as FixtureWindow;
    w.fixtureSnapshot = snapshot;
    w.fixture = w.TeiaUI.mount(document.getElementById('fixture')!, { mode: 'page', flow: false, ...options });
    w.fixture.update(snapshot);
  }, { snapshot, options });
  return page.locator('#fixture');
}
