import { test, expect } from '@playwright/test';

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
    const queries = panel.targets.filter((q: { refId: string }) => ['nodes', 'edges'].includes(q.refId));
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
    }
  });
}
