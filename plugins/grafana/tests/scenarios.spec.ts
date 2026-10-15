import { expect, test, type Page } from '@playwright/test';
import { mountFixture } from './teia.fixture';
import type { TeiaEvidence, TeiaSnapshot } from '../src/teia/teia-ui';

// Deterministic browser contracts, not recordings of live incidents or a human study.
const at = '2026-10-15T12:00:00Z';
const evidence: TeiaEvidence = { source: 'fixture-metrics', signal: 'metrics', collectedAt: at,
  evaluatedAt: at, windowSeconds: 300, metrics: ['traffic', 'errors', 'latency'] };
const names = ['errors', 'latency', 'queue backlog', 'source outage', 'shared service', 'ghost flow', 'unknown'] as const;
type Scenario = typeof names[number];

function scenarioSnapshot(scenario: Scenario): TeiaSnapshot {
  const snap: TeiaSnapshot = { round: 1, generatedAt: at, timeMode: 'current', drift: true,
    nodes: ['web', 'orders', 'store'].map(id => ({ id, kind: id === 'store' ? 'database' : 'service',
      health: 'healthy', visibility: 'full', sources: ['fixture-metrics'], requestRate: 10, errorRate: 0,
      labels: { journey: 'checkout', 'k8s.namespace': 'synthetic' }, evidence: [evidence] })),
    edges: [{ from: 'web', to: 'orders', state: 'ok', health: 'healthy', declared: true, observed: true,
      requestRate: 10, errorRate: 0, signals: ['metrics'], sources: ['fixture-metrics'], evidence: [evidence] },
    { from: 'orders', to: 'store', state: 'ok', health: 'healthy', declared: true, observed: true,
      requestRate: 10, errorRate: 0, signals: ['metrics'], sources: ['fixture-metrics'], evidence: [evidence] }],
    sources: [{ name: 'fixture-metrics', type: 'prometheus', state: 'ok', nodes: 3, edges: 2, lastSuccessAt: at }],
    findings: [], coverage: { nodes: 3, edges: 2, visibility: 1, declared: 1, observed: 1 } };
  const edge = snap.edges[0];
  if (scenario === 'errors') {
    edge.health = 'failing'; edge.errorRate = 0.2;
    edge.reasons = [{ cause: 'errors', health: 'failing', value: 0.2, limit: 0.05, unit: 'share',
      rule: 'global', summary: 'Measured errors crossed the failure limit', evidence: [evidence] }];
  } else if (scenario === 'latency') {
    edge.health = 'degraded'; edge.latencyP95Ms = 800;
    edge.reasons = [{ cause: 'latency', health: 'degraded', value: 800, limit: 500, unit: 'milliseconds',
      rule: 'checkout-latency', summary: 'Measured latency crossed the warning limit', evidence: [evidence] }];
  } else if (scenario === 'queue backlog') {
    const queue = snap.nodes[2]; queue.kind = 'queue'; queue.tech = 'rabbitmq'; queue.health = 'failing';
    queue.measures = [{ cause: 'backlog', what: 'ready messages', value: 1200 }];
    queue.reasons = [{ cause: 'backlog', health: 'failing', what: 'ready messages', value: 1200,
      limit: 1000, unit: 'count', rule: 'orders-queue', summary: 'Ready messages crossed the backlog limit', evidence: [evidence] }];
  } else if (scenario === 'source outage') {
    snap.sources[0] = { ...snap.sources[0], state: 'error', message: 'Synthetic collector timeout', retained: true };
    for (const item of [...snap.nodes, ...snap.edges]) {
      item.health = 'unknown'; delete item.requestRate; delete item.errorRate; item.errorsUnknown = true;
      item.evidence = [{ ...evidence, retained: true, metrics: [] }];
    }
  } else if (scenario === 'shared service') {
    snap.nodes[0].labels!.journey = 'checkout,browse';
    snap.nodes.push({ id: 'catalog', kind: 'service', health: 'healthy', visibility: 'full', sources: ['fixture-metrics'],
      labels: { journey: 'browse' }, requestRate: 50, errorRate: 0, evidence: [evidence] });
    snap.edges.push({ ...edge, from: 'web', to: 'catalog', requestRate: 50 });
    snap.coverage.nodes = 4; snap.coverage.edges = 3;
  } else if (scenario === 'ghost flow') {
    edge.state = 'ghost'; edge.declared = false;
  } else if (scenario === 'unknown') {
    delete edge.errorRate; edge.errorsUnknown = true; edge.health = 'unknown';
    edge.evidence = [{ ...evidence, metrics: ['traffic'] }];
  }
  return snap;
}

function frames(snap: TeiaSnapshot) {
  const rows: Record<string, Array<Record<string, unknown>>> = {
    nodes: snap.nodes.map(n => ({ ...n, generatedAt: snap.generatedAt, timeMode: snap.timeMode })),
    edges: snap.edges.map(e => ({ ...e, source: e.from, target: e.to })),
    sources: snap.sources.map(s => ({ ...s })), coverage: [{ ...snap.coverage }], findings: [], changes: [],
  };
  const encode = (key: string, value: unknown) => {
    if (['sources', 'signals', 'addresses'].includes(key)) { return (value as string[] || []).join(','); }
    return value && typeof value === 'object' ? JSON.stringify(value) : value ?? null;
  };
  return Object.fromEntries(Object.entries(rows).map(([refId, values]) => {
    const keys = [...new Set(values.flatMap(row => Object.keys(row)))];
    const fields = keys.map(name => {
      const sample = values.find(row => row[name] != null)?.[name];
      const type = typeof sample === 'number' ? 'number' : typeof sample === 'boolean' ? 'boolean' : 'string';
      return { name, type, typeInfo: { frame: type === 'number' ? 'float64' : type === 'boolean' ? 'bool' : 'string', nullable: true } };
    });
    return [refId, { status: 200, frames: keys.length ? [{ schema: { refId, fields },
      data: { values: keys.map(key => values.map(row => encode(key, row[key]))) } }] : [] }];
  }));
}

async function mountGrafana(page: Page, snap: TeiaSnapshot) {
  const results = frames(snap);
  await page.route('**/api/ds/query*', async route => {
    const request = route.request().postDataJSON() as { queries: Array<{ refId: string }> };
    const original = await route.fetch();
    const body = await original.json();
    for (const query of request.queries) {
      const replacement = results[query.refId] || { status: 200, frames: [] };
      const meta = body.results[query.refId]?.frames?.[0]?.schema?.meta;
      for (const frame of replacement.frames) {
        Object.assign(frame.schema, { name: query.refId, meta });
      }
      body.results[query.refId] = replacement;
    }
    await route.fulfill({ response: original, json: body });
  });
  await page.goto('/d-solo/teia-overview/teia?panelId=1&theme=light&refresh=');
  return page.getByTestId('teia-panel');
}

for (const surface of ['standalone', 'grafana'] as const) for (const scenario of names) {
  test(`${surface}: synthetic ${scenario}`, async ({ page }, info) => {
    const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
    await page.setViewportSize({ width: 1024, height: 768 });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    const snap = scenarioSnapshot(scenario);
    const root = surface === 'standalone' ? await mountFixture(page, snap, { theme: 'light' }) : await mountGrafana(page, snap);
    await expect(root.locator('.node')).toHaveCount(snap.nodes.length, { timeout: 30_000 });
    if (scenario === 'shared service') {
      await root.getByTitle('Show only the journey checkout', { exact: true }).click();
      await expect(root.locator('.node[data-id="web"]')).toHaveCount(1);
      await expect(root.locator('.node[data-id="catalog"]')).toHaveCount(0);
      await expect(root.locator('.port').first()).toBeVisible();
      await root.locator('.node[data-id="web"]').focus(); await page.keyboard.press('Enter');
      await expect(root.locator('#pane-details')).toContainText('Part of journey checkout');
    } else {
      const target = scenario === 'queue backlog' ? root.locator('.node[data-id="store"]') : root.getByRole('button', { name: /^web to orders,/ });
      await target.focus(); await page.keyboard.press('Enter');
      const details = root.locator('#pane-details');
      if (scenario === 'errors') {
        await expect(details).toContainText('20%'); await expect(details).toContainText('Measured errors crossed the failure limit');
        await expect(details).toContainText('5.0%');
      } else if (scenario === 'latency') {
        await expect(details).toContainText('800 ms'); await expect(details).toContainText('500 ms');
        await expect(details).toContainText('checkout-latency');
      } else if (scenario === 'queue backlog') {
        await expect(details).toContainText('Ready messages crossed the backlog limit');
        await expect(details).toContainText('orders-queue'); await expect(details).toContainText('ready messages');
      } else if (scenario === 'source outage') {
        await expect(details).toContainText('Retained topology; current measurements unavailable');
        await expect(details.locator('.hero')).toContainText('unknown');
        await root.locator('#tab-sources').click();
        await expect(root.locator('#pane-sources')).toContainText('Synthetic collector timeout');
        await expect(root.locator('#pane-sources')).toContainText('retained topology');
      } else if (scenario === 'ghost flow') {
        await expect(details).toContainText('Observed in traffic but no flows file declares it');
        await expect(details.locator('.hero')).toContainText('healthy');
      } else {
        await expect(details).toContainText('failures not measured');
        await expect(details.locator('.hero')).toContainText('failures not measured');
        await expect(details.locator('.hero')).not.toContainText('healthy');
      }
      if (scenario !== 'source outage') {
        await expect(details).toContainText('fixture-metrics');
        await expect(details).toContainText('Original sample age unknown');
      }
    }
    await info.attach('synthetic-input', { body: JSON.stringify(snap, null, 2), contentType: 'application/json' });
    await page.screenshot({ path: info.outputPath('scenario.png'), animations: 'disabled' });
    expect(errors).toEqual([]);
  });
}
