import { test, expect } from '@playwright/test';
import type { TeiaEvidence } from '../src/teia/teia-ui';

// API integration with the running lab. This deliberately uses no browser/page
// fixture: rendering and accessibility still require their own interactive audit.
test('Grafana serves the installed Teia plugin and its bundle', async ({ request }) => {
  const health = await request.get('/api/health');
  expect(health.ok()).toBe(true);
  expect((await health.json()).database).toBe('ok');
  const plugin = await request.get('/api/plugins/nilum-teia-panel/settings');
  expect(plugin.ok()).toBe(true);
  expect((await plugin.json()).id).toBe('nilum-teia-panel');
  const bundle = await request.get('/public/plugins/nilum-teia-panel/module.js');
  expect(bundle.ok()).toBe(true);
  expect(bundle.headers()['content-type']).toMatch(/javascript/);
});

for (const from of ['now-15m', 'now-30d']) {
  test(`saved dashboard preserves current-state evidence for ${from}`, async ({ request }) => {
    const dashboard = await request.get('/api/dashboards/uid/teia-overview');
    expect(dashboard.ok()).toBe(true);
    const { dashboard: saved } = await dashboard.json();
    const panel = saved.panels.find((p: { type: string }) => p.type === 'nilum-teia-panel');
    expect(panel).toBeDefined();
    // Grafana's frontend fills dashboard variables in before a query leaves it;
    // posting saved queries directly must do the same, with the saved values.
    const variables = Object.fromEntries((saved.templating?.list ?? []).map((v: { name: string; current?: { value?: string } }) => [v.name, String(v.current?.value ?? '')]));
    const interpolate = (url: string) => url.replace(/\$\{(\w+)(?::(\w+))?\}/g, (_: string, name: string, format?: string) =>
      format === 'percentencode' ? encodeURIComponent(variables[name] ?? '') : variables[name] ?? '');
    const queries = panel.targets.filter((q: { refId: string }) => ['nodes', 'edges'].includes(q.refId))
      .map((q: { url?: string }) => ({ ...q, url: q.url && interpolate(q.url) }));
    expect(queries.every((q: { url?: string }) => q.url?.endsWith('?slice='))).toBe(true);
    const response = await request.post('/api/ds/query', { data: { queries, from, to: 'now' } });
    expect(response.ok()).toBe(true);
    const { results } = await response.json();
    for (const role of ['nodes', 'edges']) {
      expect(results[role].status).toBe(200);
      const frame = results[role].frames[0];
      const names: string[] = frame.schema.fields.map((f: { name: string }) => f.name);
      const values = (name: string) => frame.data.values[names.indexOf(name)];
      expect(values('timeMode').length).toBeGreaterThan(0);
      expect(values('timeMode').every((mode: string) => mode === 'current')).toBe(true);
      expect(values('snapshotAt').every((at: string) => Number.isFinite(Date.parse(at)))).toBe(true);
      expect(names).toContain('evidence');
      expect(values('evidence').some((raw: string) => JSON.parse(raw).length > 0)).toBe(true);
      const evidence: TeiaEvidence[] = values('evidence').flatMap((raw: string) => JSON.parse(raw) as TeiaEvidence[]);
      const metrics = evidence.filter(e => e.source === 'prometheus');
      expect(metrics.length).toBeGreaterThan(0);
      for (const e of metrics) {
        expect(Date.parse(e.evaluatedAt ?? '')).toBeLessThanOrEqual(Date.parse(e.collectedAt ?? ''));
      }
      if (role === 'nodes') {
        const gauges = metrics.filter(e => e.origin?.startsWith('redis ') && e.metrics?.includes('saturation: memory'));
        expect(gauges.length).toBeGreaterThan(0);
        for (const e of gauges) {
          expect(Date.parse(e.oldestSampleAt ?? '')).toBeLessThanOrEqual(Date.parse(e.evaluatedAt ?? ''));
          expect(e.observedAt).toBeUndefined();
        }
      }
      if (role === 'edges') {
        const samples = evidence.filter(e => e.source === 'tempo' && (e.sampleCount ?? 0) > 0);
        expect(samples.length).toBeGreaterThan(0);
        for (const e of samples) {
          expect(Number.isSafeInteger(e.sampleCount)).toBe(true);
          expect(Date.parse(e.observedFrom ?? '')).toBeLessThanOrEqual(Date.parse(e.observedAt ?? ''));
          expect(Date.parse(e.observedAt ?? '')).toBeLessThan(Date.parse(e.collectedAt ?? ''));
          expect(e.windowSeconds).toBe(600);
        }
      }
    }
  });
}
