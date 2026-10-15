import { expect, test } from '@playwright/test';
import { resolve } from 'node:path';
import { graphFixture, mountFixture, type FixtureWindow } from './teia.fixture';

for (const theme of ['light', 'dark'] as const) for (const width of [640, 960, 1600]) {
  test(`keyboard, focus and controls ${theme} ${width}`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 960 });
    await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: theme });
    const root = await mountFixture(page, graphFixture(), { theme });
    await root.locator('#filterBtn').focus(); await page.keyboard.press('Enter');
    const panel = root.locator('#filterPanel');
    await expect(panel).toBeVisible();
    const facet = panel.locator('[data-filter-key]').first();
    const key = await facet.getAttribute('data-filter-key');
    await facet.focus(); await page.keyboard.press('Space');
    expect(await panel.locator('[data-filter-key]').evaluateAll((els, key) => els.some(e => e.getAttribute('data-filter-key') === key && e.getRootNode() instanceof ShadowRoot && (e.getRootNode() as ShadowRoot).activeElement === e), key)).toBe(true);
    await panel.locator('input').focus(); await page.keyboard.press('Escape');
    await expect(panel).toBeHidden(); await expect(root.locator('#filterBtn')).toBeFocused();
    await root.getByRole('button', { name: 'Clear all', exact: true }).click();
    await root.locator('.node').first().focus(); await page.keyboard.press('Enter');
    await expect(root.locator('#pane-details')).toBeVisible();
    await root.locator('#tab-details').focus(); await page.keyboard.press('End');
    await expect(root.locator('.tab').last()).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(root.locator('#stepsBtn')).toBeFocused();
    await root.locator('.edge').first().focus(); await page.keyboard.press('Enter');
    await expect(root.locator('#pane-details')).toContainText('service-');
    await root.locator('#drawerClose').click();
    for (const selector of ['#filterBtn', '#groupBy', '#liveBtn', '#flowBtn', '#exportBtn', '#stepsBtn']) {
      const box = await root.locator(selector).boundingBox();
      expect(box?.height, selector).toBeGreaterThanOrEqual(24);
      await expect(root.locator(selector)).toBeInViewport({ ratio: 1 });
    }
    await expect(root.locator('#flowBtn')).toHaveAttribute('aria-pressed', 'false');
    const contrast = await root.evaluate(host => {
      const style = getComputedStyle(host);
      const luminance = (key: string) => {
        const hex = style.getPropertyValue(key).trim().replace('#', '');
        const rgb = [0, 2, 4].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(v => v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
        return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722;
      };
      const ratio = (a: string, b: string) => { const x = luminance(a), y = luminance(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
      return [...['--ink', '--ink-2', '--muted', '--ok-text', '--warn-text', '--err-text'].flatMap(color => ['--panel', '--panel-2', '--bg'].map(bg => ({ color, bg, ratio: ratio(color, bg), minimum: 4.5 }))),
        ...['--edge', '--healthy', '--degraded', '--failing', '--unknown'].map(color => ({ color, bg: '--bg', ratio: ratio(color, '--bg'), minimum: 3 }))];
    });
    for (const item of contrast) expect(item.ratio, `${item.color} on ${item.bg}`).toBeGreaterThanOrEqual(item.minimum);
    await info.attach('contrast', { body: JSON.stringify(contrast, null, 2), contentType: 'application/json' });
    // 200% CSS zoom exercises reflow at the browser's reduced CSS viewport.
    await page.evaluate(() => { document.documentElement.style.zoom = '2'; });
    await root.getByRole('button', { name: 'Fit the map', exact: true }).click();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await expect(root.locator('#exportBtn')).toBeInViewport({ ratio: 1 });
    expect((await root.locator('#stage').boundingBox())!.height).toBeGreaterThanOrEqual(180);
    await page.screenshot({ path: info.outputPath('keyboard-200-percent.png'), animations: 'disabled' });
  });
}

test('dense overview retains every item and Read names exposes a complete readable identity', async ({ page }, info) => {
  await page.setViewportSize({ width: 1600, height: 960 });
  const root = await mountFixture(page, graphFixture(1000));
  await expect(root.locator('.node')).toHaveCount(1000);
  await expect(root.locator('.edge')).toHaveCount(1800);
  await expect(root.locator('#mapHelp')).toContainText('1000 components');
  await root.getByRole('button', { name: 'Read names', exact: true }).click();
  const title = root.locator('.node.sel .title');
  await expect(title).toBeVisible();
  await expect(title).toHaveText('service-0000');
  await expect(title).toBeInViewport({ ratio: 1 });
  const renderedSize = await title.evaluate(element => {
    const matrix = (element as SVGGraphicsElement).getScreenCTM()!;
    return parseFloat(getComputedStyle(element).fontSize) * Math.hypot(matrix.a, matrix.b);
  });
  expect(renderedSize).toBeGreaterThanOrEqual(12);
  await page.screenshot({ path: info.outputPath('1000-focused.png'), animations: 'disabled' });
});

test('expanded evidence and rule provenance survive refresh; links are safe and reachable', async ({ page }) => {
  const snap = graphFixture();
  const a = snap.nodes[0];
  a.reasons = [{ cause: 'latency', health: 'degraded', value: 2500, limit: 2000, rule: 'batch-specific', unit: 'milliseconds', summary: 'Batch latency crossed its limit' }];
  a.healthRules = [{ cause: 'latency', name: 'batch-specific', match: 'identity', degraded: 2000, failing: 4000, unit: 'milliseconds' }];
  a.links = [{ label: 'Search traces', kind: 'traces', description: 'Service and time search; no specific trace ID.', url: 'https://evidence.test/search?service=a%26b&from=2026-10-15T11%3A55%3A00Z' }];
  const root = await mountFixture(page, snap);
  await root.locator('.node').first().focus(); await page.keyboard.press('Enter');
  const pane = root.locator('#pane-details');
  await expect(pane).toContainText('batch-specific');
  await pane.getByText('Collection and sample times', { exact: true }).click();
  await expect(pane.getByText(/Oldest input sample:/)).toBeVisible();
  await page.evaluate(() => {
    const w = window as FixtureWindow;
    w.fixture.update({ ...w.fixtureSnapshot, round: 2, nodes: w.fixtureSnapshot.nodes.map(n => ({ ...n, requestRate: 11 })) });
  });
  await expect(pane.getByText(/Oldest input sample:/)).toBeVisible();
  expect(await pane.textContent()).not.toContain('null');
  const link = pane.getByRole('link', { name: 'Search traces' });
  expect(new URL((await link.getAttribute('href'))!).searchParams.get('service')).toBe('a&b');
  await page.context().route('https://evidence.test/**', route => route.fulfill({ body: 'trace search fixture' }));
  const popup = page.waitForEvent('popup'); await link.click();
  await expect((await popup).locator('body')).toContainText('trace search fixture');
});

test('hierarchical expand and collapse preserves selected identity and a shared view restores in a clean context', async ({ page, browser }) => {
  const snap = graphFixture();
  const root = await mountFixture(page, snap, { mode: 'panel', storageKey: 'test-panel', theme: 'light' });
  const group = 'hierarchy:' + JSON.stringify(['cluster', 'k8s.namespace', 'service.name']);
  await root.locator('#groupBy').selectOption(group);
  await root.locator('.node').first().focus(); await page.keyboard.press('Enter');
  const title = await root.locator('#pane-details h2').textContent();
  await root.getByRole('button', { name: 'Expand group', exact: true }).click();
  await expect(root.locator('#pane-details')).toContainText('Collapse to ' + title);
  await root.locator('#drawerClose').click();
  await root.locator('#exportBtn').click(); await root.locator('#shareBtn').click();
  const shared = await root.locator('#shareURL').inputValue();
  expect(new URL(shared).searchParams.has('teiaView-test-panel')).toBe(true);
  const context = await browser.newContext();
  const fresh = await context.newPage();
  await fresh.route('**/teia-ui.js', route => route.fulfill({ path: resolve('../../internal/api/ui/teia-ui.js'), contentType: 'text/javascript; charset=utf-8' }));
  await fresh.route('**/__ui-fixture*', route => route.fulfill({ contentType: 'text/html', body: '<meta charset="utf-8"><style>html,body{height:100%;margin:0}#fixture{height:100%}</style><div id="fixture"></div><script src="/teia-ui.js"></script>' }));
  await fresh.goto(shared);
  await fresh.evaluate(s => {
    const w = window as FixtureWindow;
    w.fixture = w.TeiaUI.mount(document.getElementById('fixture')!, { mode: 'panel', storageKey: 'test-panel' });
    w.fixture.update(s);
    w.fixture.setOptions({ groupBy: '', journeyKey: '', flow: undefined, values: undefined });
  }, snap);
  await expect(fresh.locator('#fixture #groupBy')).toHaveValue(group);
  await expect(fresh.locator('#fixture .node.sel')).toHaveCount(1);
  await expect(fresh.locator('#fixture #pane-details')).toContainText('Collapse to ' + title);
  await fresh.locator('#fixture #drawerClose').click();
  await fresh.getByTitle('Show only the journey checkout', { exact: true }).click();
  expect(JSON.parse(new URL(fresh.url()).searchParams.get('teiaView-test-panel')!).journey).toBe('checkout');
  await context.close();
});

test('refresh retains focus on filters and boundary ports, and shared port selection can be restored', async ({ page }) => {
  const snap = graphFixture();
  const root = await mountFixture(page, snap, { journey: 'checkout' });
  const refresh = () => page.evaluate(() => {
    const w = window as FixtureWindow;
    w.fixtureSnapshot = { ...w.fixtureSnapshot, round: w.fixtureSnapshot.round + 1, nodes: w.fixtureSnapshot.nodes.map(n => ({ ...n, requestRate: w.fixtureSnapshot.round })) };
    w.fixture.update(w.fixtureSnapshot);
  });
  await root.locator('#filterBtn').focus(); await refresh();
  await expect(root.locator('#filterBtn')).toBeFocused();
  await root.locator('.port').first().focus(); await refresh();
  await expect(root.locator('.port').first()).toBeFocused();
  await page.keyboard.press('Enter');
  const label = await root.locator('#pane-details h2').textContent();
  await root.locator('#exportBtn').click(); await root.locator('#shareBtn').click();
  const url = await root.locator('#shareURL').inputValue();
  await page.evaluate(({ url, snap }) => {
    const w = window as FixtureWindow;
    w.fixture.destroy(); history.replaceState(null, '', url);
    w.fixture = w.TeiaUI.mount(document.getElementById('fixture')!, { mode: 'page', flow: false });
    w.fixture.update(snap);
  }, { url, snap });
  await expect(root.locator('.port.sel')).toHaveCount(1);
  await expect(root.locator('#pane-details h2')).toHaveText(label!);
});
