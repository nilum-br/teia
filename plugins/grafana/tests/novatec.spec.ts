import { expect, test } from '@playwright/test';

test.beforeEach(async ({ request }) => {
  if (process.env.TEIA_NOVATEC !== 'true') { return; }
  await expect.poll(async () => {
    try { return (await request.get('/api/health')).status(); } catch { return 0; }
  }, { timeout: 15_000 }).toBe(200);
});

// Installed comparison only. Ordinary CI/lab runs do not require a competitor plugin.
for (const scenario of [{ name: 'prepared table', panel: 1 }, { name: 'official dummy data', panel: 2 }]) {
test(`installed Novatec comparison renders ${scenario.name}`, async ({ page, request }, info) => {
  test.skip(process.env.TEIA_NOVATEC !== 'true', 'requires the optional signed Novatec installation');
  const installedResponse = await request.get('/api/plugins/novatec-sdg-panel/settings');
  expect(installedResponse.ok()).toBe(true);
  const installed = await installedResponse.json();
  expect(installed.info.version).toBe('4.2.0');
  expect(installed.signature).toBe('valid');
  const health = await (await request.get('/api/health')).json();
  await info.attach('environment', {
    body: JSON.stringify({ grafana: health.version, plugin: installed.info.version, signature: installed.signature, scenario: scenario.name }),
    contentType: 'application/json',
  });
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.setViewportSize({ width: 1024, height: 768 });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto(`/d-solo/teia-novatec-comparison/compare?panelId=${scenario.panel}`);
  try {
    await expect(page.locator('canvas').first()).toBeVisible({ timeout: 15_000 });
  } finally {
    await page.screenshot({ path: info.outputPath('novatec-overview.png'), animations: 'disabled' });
    await info.attach('browser-errors', { body: JSON.stringify(errors, null, 2), contentType: 'application/json' });
  }
  expect(errors).toEqual([]);
  if (scenario.panel === 1) {
    await page.locator('button:has(.fa-plus)').click();
    await page.locator('button:has(.fa-plus)').click();
    await expect(page.getByText('Layer 2/2', { exact: true })).toBeVisible();
    await page.locator('button:has(.fa-dot-circle-o)').click();
    await page.screenshot({ path: info.outputPath('novatec-expanded.png'), animations: 'disabled' });
    // Fixed viewport and non-animated layout: click the visible payment node.
    await page.mouse.click(512, 388);
    await expect(page.locator('.statistics.show')).toBeVisible();
    await expect(page.locator('.statistics.show')).toContainText('payments');
    await expect(page.locator('.statistics.show')).toContainText('600 ms');
    await expect(page.locator('.statistics.show')).toContainText('Bad (> 300ms)');
    await expect(page.locator('.statistics.show .table--selection tr').filter({ hasText: 'Error Rate' }).first()).toContainText('25');
    const drilldown = page.locator('.statistics.show a[target="_blank"]');
    const destination = new URL((await drilldown.getAttribute('href'))!, page.url());
    expect(destination.searchParams.get('var-service')).toBe('payments');
    expect(Number(destination.searchParams.get('to'))).toBeGreaterThan(Number(destination.searchParams.get('from')));
    await info.attach('selected-statistics', { body: await page.locator('.statistics.show').innerText(), contentType: 'text/plain' });
    await page.screenshot({ path: info.outputPath('novatec-selected.png'), animations: 'disabled' });
    await page.mouse.click(512, 630);
    await expect(page.locator('.statistics.show .header--selection')).toContainText('payment-db');
    // Main errors row is omitted, but the incoming-neighbour table displays 0%.
    await expect(page.locator('.statistics.show .table--selection tr').filter({ hasText: 'Errors' })).toHaveCount(0);
    await expect(page.locator('.statistics.show')).toContainText('0%');
    await page.screenshot({ path: info.outputPath('novatec-missing-errors.png'), animations: 'disabled' });
    const opened = page.waitForEvent('popup'); await drilldown.click();
    const popup = await opened;
    await expect(popup.getByText('Selected service: payment-db', { exact: true })).toBeVisible();
    await popup.close();
  }
});
}
