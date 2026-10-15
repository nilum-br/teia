import { expect, test } from '@playwright/test';

// Exercise the actual shared UI and installed Grafana plugin. Screenshots are
// review artifacts, not golden images: the live lab changes on every collection.
const standaloneURL = process.env.TEIA_URL || 'http://localhost:8480';

for (const surface of ['standalone', 'grafana'] as const) {
  const sizes = surface === 'standalone'
    ? [{ width: 1600, height: 960 }, { width: 1024, height: 768 }]
    : [{ width: 900, height: 500 }, { width: 600, height: 400 }];
  for (const theme of ['light', 'dark'] as const) {
    for (const size of sizes) {
      test(`${surface} ${theme} ${size.width}x${size.height}: map and evidence`, async ({ page }, info) => {
        test.setTimeout(60_000);
        const pageErrors: string[] = [];
        page.on('pageerror', error => pageErrors.push(error.message));
        await page.setViewportSize(size);
        await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
        await page.goto(surface === 'standalone'
          ? `${standaloneURL}/?theme=${theme}&flow=0&live=0`
          : `/d-solo/teia-overview/teia?panelId=1&theme=${theme}&from=now-15m&to=now&refresh=`);
        const root = surface === 'standalone' ? page.locator('#teia') : page.getByTestId('teia-panel');
        await expect(root.locator('.node').first()).toBeVisible({ timeout: 30_000 });
        await expect(root.locator('#timeContext')).toContainText('Current state');
        if (surface === 'grafana') {
          await expect(root.locator('#timeContext')).toContainText('dashboard range does not change this snapshot');
        }
        const flow = root.locator('#flowBtn');
        if (await flow.getAttribute('aria-pressed') === 'true') {
          await flow.click();
        }
        for (const selector of ['#groupBy', '#valuesBtn', '#flowBtn', '#liveBtn', '#stepsBtn', '#exportBtn', '.open-legend']) {
          await expect(root.locator(selector)).toBeInViewport({ ratio: 1 });
        }
        const capture = async (name: string) => {
          const path = info.outputPath(`${name}.png`);
          await page.screenshot({ path, animations: 'disabled' });
          await info.attach(name, { path, contentType: 'image/png' });
        };
        await capture('overview');

        await root.locator('.node[data-id="catalog-cache"]').click();
        const details = root.locator('#pane-details');
        await expect(details).toBeVisible();
        await expect(root.locator('#drawerClose')).toBeInViewport({ ratio: 1 });
        await expect(details).toContainText('catalog-cache');
        await expect(details).toContainText('Oldest input sample:');
        await expect(details).toContainText('Query evaluated:');
        await expect.poll(async () => {
          const node = await root.locator('.node[data-id="catalog-cache"]').boundingBox();
          const sidebar = await root.locator('aside').boundingBox();
          return !!node && !!sidebar && node.x + node.width <= sidebar.x;
        }).toBe(true);
        await capture('selected-node');
        const sampleEvidence = details.locator('p').filter({ hasText: 'Oldest input sample:' }).first();
        await sampleEvidence.scrollIntoViewIfNeeded();
        await expect(sampleEvidence).toBeInViewport();
        expect(await page.evaluate(() => window.scrollX)).toBe(0);
        await capture('sample-evidence');
        await root.locator('#drawerClose').click();

        await root.getByRole('button', { name: 'Fit the map', exact: true }).click();
        const edgePoint = await root.locator('.edge .hit').first().evaluate((element: SVGPathElement) => {
          const point = element.getPointAtLength(element.getTotalLength() / 2);
          const screen = point.matrixTransform(element.getScreenCTM()!);
          return { x: screen.x, y: screen.y };
        });
        await page.mouse.click(edgePoint.x, edgePoint.y);
        await expect(root.locator('.edge.sel')).toHaveCount(1);
        await expect(root.locator('.edge.sel')).not.toHaveClass(/\bdim\b/);
        await expect(root.locator('.node:not(.dim)')).toHaveCount(2);
        await expect.poll(async () => {
          const endpoints = await root.locator('.node:not(.dim)').evaluateAll(nodes => nodes.map(n => n.getBoundingClientRect().right));
          const sidebar = await root.locator('aside').boundingBox();
          return !!sidebar && endpoints.every(right => right <= sidebar.x);
        }).toBe(true);
        await expect(details.locator('h2')).toBeInViewport();
        await capture('selected-edge');
        await root.locator('#drawerClose').click();

        await root.getByTitle('Show only the journey checkout', { exact: true }).click();
        await root.getByRole('button', { name: 'Fit the map', exact: true }).click();
        await capture('journey');
        await root.locator('.port').first().click();
        await expect(root.locator('.port.sel')).toHaveCount(1);
        await expect(details).toContainText('Outside (');
        await expect(details.locator('h2')).toBeInViewport();
        await root.locator('#drawerClose').click();
        await root.getByRole('button', { name: 'Clear all', exact: true }).click();
        await root.getByLabel('Group nodes by a label', { exact: true }).selectOption('team');
        await root.getByRole('button', { name: 'Fit the map', exact: true }).click();
        await capture('grouped');
        expect(pageErrors).toEqual([]);
      });
    }
  }
}

test('edge value and reason labels open their communication details', async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 960 });
  await page.goto(`${standaloneURL}/?flow=0&live=0`);
  const root = page.locator('#teia');
  for (const selector of ['.value-tag', '.why-tag']) {
    await root.locator(selector).first().click();
    await expect(root.locator('.edge.sel')).toHaveCount(1);
    await expect(root.locator('#pane-details h2')).toBeInViewport();
    await root.locator('#drawerClose').click();
  }
});

test('manual pan, zoom and selection survive sidebar controls and resumed updates', async ({ page }) => {
  test.setTimeout(40_000);
  await page.setViewportSize({ width: 1600, height: 960 });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const initial = page.waitForResponse(response => response.url().endsWith('/api/v1/snapshot') && response.ok());
  await page.goto(`${standaloneURL}/?flow=0&live=0`);
  const initialRound: number = (await (await initial).json()).round;
  const root = page.locator('#teia');
  const cache = root.locator('.node[data-id="catalog-cache"]');
  await expect(cache).toBeVisible();
  await cache.click();
  await root.getByRole('button', { name: 'Zoom in', exact: true }).click();
  const stage = (await root.locator('#stage').boundingBox())!;
  await page.mouse.move(stage.x + 8, stage.y + 30);
  await page.mouse.down();
  await page.mouse.move(stage.x + 48, stage.y + 50);
  await page.mouse.up();
  const viewport = root.locator('#viewport');
  const manualView = await viewport.getAttribute('transform');
  await root.locator('#drawerClose').click();
  await expect(root.locator('#stepsBtn')).toBeFocused();
  await expect(viewport).toHaveAttribute('transform', manualView!);
  await expect(cache).toHaveClass(/\bsel\b/);
  await root.locator('#liveBtn').click();
  await expect.poll(async () => {
    const live = await root.locator('#live').textContent();
    return Number(live?.match(/round (\d+)/)?.[1] || 0);
  }, { timeout: 25_000 }).toBeGreaterThan(initialRound);
  await expect(root.locator('#liveBtn')).toHaveAttribute('aria-pressed', 'true');
  await expect(viewport).toHaveAttribute('transform', manualView!);
  await expect(cache).toHaveClass(/\bsel\b/);
  await root.locator('#liveBtn').click();
  await expect(root.locator('#live')).toContainText('paused');
});

test('saved Grafana dashboard remains current state at a 30-day range', async ({ page }, info) => {
  await page.setViewportSize({ width: 900, height: 500 });
  await page.goto('/d-solo/teia-overview/teia?panelId=1&theme=dark&from=now-30d&to=now&refresh=');
  const root = page.getByTestId('teia-panel');
  await expect(root.locator('.node').first()).toBeVisible({ timeout: 30_000 });
  await expect(root.locator('#timeContext')).toContainText('Current state');
  await expect(root.locator('#timeContext')).toContainText('dashboard range does not change this snapshot');
  await page.screenshot({ path: info.outputPath('current-30d.png'), animations: 'disabled' });
});
