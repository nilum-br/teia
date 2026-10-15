import { expect, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  if (process.env.TEIA_CANDIDATE !== 'true') { return; }
  await expect.poll(async () => {
    try { return (await request.get('/api/health')).status(); } catch { return 0; }
  }, { timeout: 15_000 }).toBe(200);
});

// Opt-in: this fixture runs against examples/grafana, not the full telemetry lab.
test('candidate installation renders typed sample frames and keeps unknown errors', async ({ page, request }) => {
  test.skip(process.env.TEIA_CANDIDATE !== 'true', 'requires the isolated candidate installation');
  const health = await (await request.get('/api/health')).json();
  if (process.env.GRAFANA_EXPECTED_VERSION) {
    expect(health.version).toBe(process.env.GRAFANA_EXPECTED_VERSION);
  }
  const installedResponse = await request.get('/api/plugins/nilum-teia-panel/settings');
  expect(installedResponse.ok()).toBe(true);
  const installed = await installedResponse.json();
  if (process.env.TEIA_EXPECTED_PLUGIN_VERSION) {
    expect(installed.info.version).toBe(process.env.TEIA_EXPECTED_PLUGIN_VERSION);
  }
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/d-solo/teia-candidate-sample/sample?panelId=1&from=now-15m&to=now');
  const root = page.getByTestId('teia-panel');
  await expect(root.locator('.node')).toHaveCount(3, { timeout: 30_000 });
  await expect(root.locator('.edge')).toHaveCount(2);
  await root.locator('.node[data-id="payments"]').click();
  await expect(root.locator('#pane-details')).toContainText('payments');
  await expect(root.locator('#pane-details')).toContainText('20%');
  await root.locator('#drawerClose').click();
  await root.getByRole('button', { name: 'Fit the map', exact: true }).click();
  await root.locator('.node[data-id="payment-db"]').click();
  await expect(root.locator('#pane-details')).toContainText(/unknown|not measured/i);
  expect(errors).toEqual([]);
});

test('candidate onboarding gives actionable no-data, invalid-frame and source-error states', async ({ page }) => {
  test.skip(process.env.TEIA_CANDIDATE !== 'true', 'requires the isolated candidate installation');
  await page.goto('/d-solo/teia-candidate-onboarding/states?panelId=1');
  await expect(page.getByTestId('teia-panel').locator('#live')).toContainText('No query data');
  await page.goto('/d-solo/teia-candidate-onboarding/states?panelId=2');
  await expect(page.getByTestId('teia-panel').locator('#live')).toContainText('No supported node/edge fields. Check the query mapping.');
  await page.goto('/d-solo/teia-candidate-onboarding/states?panelId=3');
  await expect(page.getByTestId('teia-panel').locator('#live')).toContainText(/data\s*source.*(not found|missing)/i);
});
