import { expect, test, type BrowserContext } from '@playwright/test';
import type { TeiaEdge, TeiaSnapshot } from '../src/teia/teia-ui';

const base = process.env.TEIA_URL || 'http://localhost:8480';

// Evidence links carry the Grafana URL a viewer's browser uses (the Teia
// server's GRAFANA_PUBLIC_URL). Inside Docker, forward that same URL to the
// reachable service without changing its query or UI.
async function reachPublicGrafana(context: BrowserContext) {
  const publicURL = process.env.GRAFANA_PUBLIC_URL || 'http://localhost:3480';
  const grafana = process.env.GRAFANA_URL || publicURL;
  if (grafana.startsWith(publicURL)) {
    return;
  }
  await context.route(publicURL + '/**', async route => {
    await route.fulfill({ response: await route.fetch({ url: route.request().url().replace(publicURL, grafana) }) });
  });
}

test('stored history replays and a clean shared view restores the same snapshot', async ({ page, browser, request }) => {
  const response = await request.get(base + '/api/v1/history');
  expect(response.ok()).toBe(true);
  const listing = await response.json();
  expect(listing.enabled).toBe(true);
  expect(listing.snapshots.length).toBeGreaterThan(0);
  // The picker lists the newest 200 captures; a long-running lab has older ones too.
  const original = [...listing.snapshots].sort((a: { generatedAt: string }, b: { generatedAt: string }) => b.generatedAt.localeCompare(a.generatedAt))[0];
  await page.goto(base + '/?flow=0');
  const root = page.locator('#teia');
  await expect(root.locator('.node').first()).toBeVisible();
  await root.locator('#historyBtn').click();
  await root.locator(`[data-history-id="${original.id}"]`).click();
  await expect(root.locator('#timeContext')).toContainText('Historical snapshot');
  await expect(root.locator('#liveBtn')).toBeDisabled();
  const replay = await request.get(base + '/api/v1/history/' + original.id);
  const snapshot: TeiaSnapshot = await replay.json();
  expect(Date.parse(snapshot.generatedAt)).toBe(Date.parse(original.generatedAt));
  await root.locator('.node').first().focus(); await page.keyboard.press('Enter');
  await expect(root.locator('#pane-details')).toContainText('Historical identity and presence');
  await root.locator('#exportBtn').click(); await root.locator('#shareBtn').click();
  const shared = await root.locator('#shareURL').inputValue();
  expect(JSON.parse(new URL(shared).searchParams.get('teiaView')!).history).toBe(original.id);
  const context = await browser.newContext();
  const fresh = await context.newPage();
  await fresh.goto(shared);
  await expect(fresh.locator('#teia #timeContext')).toContainText('Historical snapshot');
  await expect(fresh.locator('#teia #timeContext')).toContainText(new Date(original.generatedAt).toISOString());
  await expect(fresh.locator('#teia .node.sel')).toHaveCount(1);
  await fresh.locator('#teia #historyBtn').click();
  await fresh.locator('#teia #historyFrom').fill('2000-01-01T00:00');
  await fresh.locator('#teia #historyTo').fill('2000-01-02T00:00');
  await fresh.getByRole('button', { name: 'Find snapshots' }).click();
  await expect(fresh.locator('#teia #historyStatus')).toContainText('does not mean no traffic');
  await fresh.getByRole('button', { name: 'Return to current state' }).click();
  await expect(fresh.locator('#teia #timeContext')).toContainText('Current state');
  await expect(fresh.locator('#teia #liveBtn')).toBeEnabled();
  await context.close();
});

test('saved Grafana history dashboard restores a snapshot independently of the dashboard time range', async ({ page, request }, info) => {
  const listing = await (await request.get(base + '/api/v1/history')).json();
  // The newest capture: once history reaches its byte or count limit, the
  // oldest is pruned every round and could vanish before the panel asks.
  const original = listing.snapshots.at(-1);
  await expect.poll(async () => (await request.get('/api/dashboards/uid/teia-history')).status(), { timeout: 40_000 }).toBe(200);
  await page.goto('/d-solo/teia-history/teia?panelId=1&var-snapshot=' + encodeURIComponent(original.id) + '&from=now-30d&to=now');
  const root = page.getByTestId('teia-panel');
  await expect(root.locator('#timeContext')).toContainText('Historical snapshot', { timeout: 30_000 });
  await expect(root.locator('#timeContext')).toContainText(new Date(original.generatedAt).toISOString());
  await expect(root.locator('#liveBtn')).toBeDisabled();
  await root.locator('.node').first().focus(); await page.keyboard.press('Enter');
  await expect(root.locator('#pane-details')).toContainText('Historical identity and presence');
  await page.screenshot({ path: info.outputPath('grafana-history.png'), animations: 'disabled' });
});

for (const surface of ['standalone', 'grafana'] as const) {
  test(`${surface}: a service opens real Loki logs and a dashboard with preserved context`, async ({ page, context }, info) => {
    test.setTimeout(60_000);
    await reachPublicGrafana(context);
    await page.goto(surface === 'standalone' ? base + '/?flow=0&live=0' : '/d-solo/teia-overview/teia?panelId=1&theme=light&refresh=');
    const root = surface === 'standalone' ? page.locator('#teia') : page.getByTestId('teia-panel');
    const node = root.locator('.node[data-id="catalog"]');
    await expect(node).toBeVisible({ timeout: 30_000 });
    await node.focus(); await page.keyboard.press('Enter');
    const logLink = root.getByRole('link', { name: 'Search service logs' });
    const logURL = new URL((await logLink.getAttribute('href'))!);
    const panes = JSON.parse(logURL.searchParams.get('panes')!);
    const pane = Object.values(panes)[0] as { queries: Array<{ expr: string }>; range: { from: string; to: string } };
    expect(pane.queries[0].expr).toBe('{service="catalog"}');
    expect(Number(pane.range.to)).toBeGreaterThan(Number(pane.range.from));
    const logsPromise = page.waitForEvent('popup'); await logLink.click();
    const logs = await logsPromise;
    await expect(logs.getByText('Logs', { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await expect(logs.getByText(/\d+ lines displayed/).first()).toBeVisible({ timeout: 30_000 });
    await expect(logs.locator('body')).toContainText(/"msg":\s*"access"/);
    await logs.screenshot({ path: info.outputPath('loki-evidence.png'), animations: 'disabled' });
    await logs.close();
    const dashboardLink = root.getByRole('link', { name: 'Service metrics dashboard' });
    const dashboardURL = new URL((await dashboardLink.getAttribute('href'))!);
    expect(dashboardURL.searchParams.get('var-service')).toBe('catalog');
    expect(Number(dashboardURL.searchParams.get('to'))).toBeGreaterThan(Number(dashboardURL.searchParams.get('from')));
    const dashboardPromise = page.waitForEvent('popup'); await dashboardLink.click();
    const dashboard = await dashboardPromise;
    await expect(dashboard.getByRole('heading', { name: 'Incoming calls to selected service' })).toBeVisible({ timeout: 30_000 });
    expect(new URL(dashboard.url()).searchParams.get('var-service')).toBe('catalog');
    await dashboard.screenshot({ path: info.outputPath('metrics-evidence.png'), animations: 'disabled' });
    await dashboard.close();
  });

  test(`${surface}: a connection opens real Tempo evidence with its original endpoints and time`, async ({ page, context, request }, info) => {
    test.setTimeout(60_000);
    await reachPublicGrafana(context);
    const response = await request.get(base + '/api/v1/snapshot');
    const snapshot: TeiaSnapshot = await response.json();
    const edge = snapshot.edges.find(e => e.from === 'catalog' && e.to === 'catalog-db' && e.links?.some(l => l.kind === 'traces')) as TeiaEdge;
    expect(edge).toBeDefined();
    await page.goto(surface === 'standalone' ? base + '/?flow=0&live=0' : '/d-solo/teia-overview/teia?panelId=1&theme=light&refresh=');
    const root = surface === 'standalone' ? page.locator('#teia') : page.getByTestId('teia-panel');
    // Accessible edge name begins with its stable endpoint pair. When the
    // layout puts both cards on one row, the connection is a straight
    // horizontal line whose box has no height, which Playwright reports as
    // hidden although it is drawn and focusable: wait for it in the tree.
    const target = root.getByRole('button', { name: /^catalog to catalog-db,/ });
    await expect(target).toBeAttached({ timeout: 30_000 });
    await target.focus(); await page.keyboard.press('Enter');
    await expect(root.locator('#pane-details h2')).toContainText('catalog → catalog-db');
    const link = root.getByRole('link', { name: 'Search traces for these endpoints' });
    const url = new URL((await link.getAttribute('href'))!);
    const panes = JSON.parse(url.searchParams.get('panes')!);
    const pane = Object.values(panes)[0] as { queries: Array<{ query: string }>; range: { from: string; to: string } };
    expect(pane.queries[0].query).toContain('catalog-db');
    expect(pane.queries[0].query).toContain('catalog');
    expect(Number(pane.range.to)).toBeGreaterThan(Number(pane.range.from));
    const popupPromise = page.waitForEvent('popup'); await link.click();
    const popup = await popupPromise;
    await expect(popup.getByText('Search', { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await expect(popup.getByText('Trace ID', { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await popup.screenshot({ path: info.outputPath('tempo-evidence.png'), animations: 'disabled' });
    await popup.close();
    // Verify the other public destinations exist with the supplied context.
    for (const item of edge.links || []) if (item.kind === 'dashboard') {
      const dashboard = new URL(item.url);
      expect(dashboard.searchParams.get('var-service')).toBe('catalog');
      expect(dashboard.searchParams.get('var-target')).toBe('catalog-db');
      expect((await request.get('/api/dashboards/uid/teia-evidence')).ok()).toBe(true);
    }
  });
}
