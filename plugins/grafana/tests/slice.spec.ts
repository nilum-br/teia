import { expect, test, type Locator, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { mountFixture, type FixtureWindow } from './teia.fixture';
import type { TeiaSnapshot } from '../src/teia/teia-ui';

const operations: NonNullable<TeiaSnapshot['operations']> = JSON.parse(readFileSync(
  resolve('../../internal/journey/testdata/operation-runtime.json'), 'utf8'));

type SliceSnapshot = TeiaSnapshot & {
  dimensions: Record<string, string[]>;
  slice?: { selector: Record<string, string>; matched: boolean; metrics: string; sources: string[] };
};

// Checkout calls web -> orders -> db and browse web -> catalog -> db.
const CALLS: Record<string, string[][]> = { checkout: [['web', 'orders'], ['orders', 'db']], browse: [['web', 'catalog'], ['catalog', 'db']] };
// The entry operation each journey's requests sampled.
const OPERATION: Record<string, string> = { checkout: 'POST /checkout', browse: 'GET /browse' };

// snapshot is the whole map, or what /api/v1/snapshot?slice=journey:<journey> answers.
function snapshot(journey = ''): SliceSnapshot {
  const calls = journey ? CALLS[journey] : Object.values(CALLS).flat();
  const ids = [...new Set(calls.flat())];
  return {
    round: 1, generatedAt: '2026-10-15T12:05:00Z', timeMode: 'current',
    nodes: ids.map(id => ({ id, kind: id === 'db' ? 'database' : 'service', health: 'healthy', visibility: 'full', sources: ['traces'] })),
    edges: calls.map(([from, to]) => ({ from, to, health: 'healthy', state: 'ok', declared: true, observed: true, sources: ['traces'], signals: ['traces'] })),
    sources: [], findings: [], coverage: { nodes: ids.length, edges: calls.length, visibility: 1, declared: 1, observed: 1 },
    dimensions: { journey: Object.keys(CALLS).sort() },
    slice: journey ? { selector: { journey }, matched: true, metrics: 'scoped', sources: ['tempo'] } : undefined,
    // teia narrows operations to the traces whose entry carried the slice.
    operations: journey ? { ...operations, scopes: operations.scopes.filter(s => s.operation === OPERATION[journey]) } : operations,
  };
}

// serve answers the snapshot the UI asks for, whole or sliced.
async function serve(page: Page) {
  await page.route('**/api/v1/snapshot*', route => {
    const selection = new URL(route.request().url()).searchParams.get('slice') || '';
    return route.fulfill({ json: snapshot(selection.replace(/^journey:/, '')) });
  });
  await page.route('**/api/v1/changes', route => route.fulfill({ json: [] }));
}

async function names(root: Locator) {
  return (await root.locator('.node .title').allTextContents()).sort();
}

// pickSlice chooses a slice value: in the bar when it has room, otherwise in
// the Filter panel, which narrow maps keep it to so the map keeps its space.
async function pickSlice(root: Locator, label: string) {
  const inline = root.locator('.slice-pick').getByRole('button', { name: label, exact: true });
  if (await inline.isVisible()) {
    await inline.click();
    return;
  }
  await root.locator('#filterBtn').click();
  await root.locator('#filterPanel').getByRole('button', { name: label }).click();
  await root.locator('#filterPanel').getByRole('button', { name: 'Done', exact: true }).click();
}

async function workspace(root: Locator, tab: string) {
  await root.getByRole('button', { name: 'Investigate', exact: true }).click();
  await root.getByRole('button', { name: tab, exact: true }).click();
  return root.getByRole('dialog', { name: 'Investigation workspace' });
}

test('a slice chosen while live updates are paused is drawn at once', async ({ page }) => {
  await serve(page);
  // Without a stream the map stops following updates after its first load.
  const root = await mountFixture(page, snapshot(), { api: '/api/v1/', stream: false });
  await expect(root.locator('#liveBtn')).toHaveAttribute('aria-pressed', 'false');
  expect(await names(root)).toEqual(['catalog', 'db', 'orders', 'web']);
  await pickSlice(root, 'journey: checkout');
  await expect(root.locator('.fchip.slice')).toContainText('Calls of journey = checkout');
  await expect.poll(() => names(root)).toEqual(['db', 'orders', 'web']);
  await root.getByRole('button', { name: 'Show every call', exact: true }).click();
  await expect(root.locator('.fchip.slice')).toHaveCount(0);
  await expect.poll(() => names(root)).toEqual(['catalog', 'db', 'orders', 'web']);
});

test('an update held while paused does not replace a slice chosen after it', async ({ page }) => {
  await serve(page);
  const root = await mountFixture(page, snapshot(), { api: '/api/v1/', stream: false });
  await expect(root.locator('#liveBtn')).toHaveAttribute('aria-pressed', 'false');
  // A whole-map update arrives while paused and waits for Live.
  await page.evaluate(() => { const w = window as FixtureWindow; w.fixture.update(w.fixtureSnapshot); });
  await pickSlice(root, 'journey: checkout');
  await expect.poll(() => names(root)).toEqual(['db', 'orders', 'web']);
  await root.locator('#liveBtn').click();
  await expect(root.locator('#liveBtn')).toHaveAttribute('aria-pressed', 'true');
  expect(await names(root)).toEqual(['db', 'orders', 'web']);
  await expect(root.locator('.fchip.slice')).toContainText('Calls of journey = checkout');
});

for (const width of [1600, 600]) {
  test(`a slice narrows a sampled operation, and one it did not sample says so (${width}px)`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 });
    await serve(page);
    const root = await mountFixture(page, snapshot(), { api: '/api/v1/', stream: false });
    await expect(root.locator('#liveBtn')).toHaveAttribute('aria-pressed', 'false');
    let dialog = await workspace(root, 'Operations and traces');
    await dialog.getByRole('combobox', { name: 'Operation', exact: true }).selectOption(operations.scopes.find(s => s.operation === 'POST /checkout')!.id);
    await dialog.getByRole('button', { name: 'Show sampled operation', exact: true }).click();
    await expect.poll(() => names(root)).toEqual(['db', 'orders', 'web']);
    // The checkout slice keeps the checkout operation on the map.
    await pickSlice(root, 'journey: checkout');
    await expect(root.locator('.fchip.slice')).toContainText('Calls of journey = checkout');
    await expect(root.locator('#investigationSummary')).toContainText('Sampled operation: POST /checkout');
    expect(await names(root)).toEqual(['db', 'orders', 'web']);
    // Browse did not sample it: no platform calls stand in for it.
    await pickSlice(root, 'journey: browse');
    await expect(root.locator('#investigationSummary')).toContainText('not sampled in this slice');
    await expect(root.locator('.node')).toHaveCount(0);
    dialog = await workspace(root, 'Operations and traces');
    await expect(dialog).toContainText('Only calls with journey = browse: the sampled traces whose entry carried it');
    const choose = dialog.getByRole('combobox', { name: 'Operation', exact: true });
    expect((await choose.locator('option').allTextContents()).slice(1).map(o => o.split(' · ')[1])).toEqual(['GET /browse']);
    await expect(choose).toHaveValue('');
    await expect(choose.locator('option:checked')).toHaveText('Choose an operation');
    await page.screenshot({ path: info.outputPath(`operations-in-slice-${width}.png`), animations: 'disabled' });
  });
}

test('a capture of a slice is compared only with a capture of the same slice', async ({ page }, info) => {
  await serve(page);
  await page.route('**/api/v1/history', route => route.fulfill({ json: { enabled: true, snapshots: [] } }));
  const root = await mountFixture(page, snapshot(), { api: '/api/v1/', stream: false });
  await expect(root.locator('#liveBtn')).toHaveAttribute('aria-pressed', 'false');
  let dialog = await workspace(root, 'Compare snapshots');
  await dialog.getByRole('button', { name: 'Capture current snapshot as before', exact: true }).click();
  await dialog.getByRole('button', { name: 'Close investigation', exact: true }).click();
  await pickSlice(root, 'journey: checkout');
  await expect.poll(() => names(root)).toEqual(['db', 'orders', 'web']);
  dialog = await workspace(root, 'Compare snapshots');
  await expect(dialog).toContainText('“Capture current snapshot” takes that slice');
  await dialog.getByRole('button', { name: 'Capture current snapshot as after', exact: true }).click();
  // Everything outside checkout would read as removed: the scopes differ, nothing changed.
  await expect(dialog.locator('#comparisonResult')).toContainText('Not compared: “before” holds every call and “after” holds only calls with journey = checkout');
  await expect(dialog.locator('.comparison-map')).toHaveCount(0);
  await expect(dialog.locator('#compare-label-after')).toContainText('only calls with journey = checkout');
  await page.screenshot({ path: info.outputPath('compare-scopes.png'), animations: 'disabled' });
  await dialog.getByRole('button', { name: 'Capture current snapshot as before', exact: true }).click();
  await expect(dialog.locator('#comparisonResult')).toContainText('0 only after · 0 only before · 0 changed');
  await expect(dialog.locator('.comparison-map')).toHaveCount(2);
});

// The lab's web entry spans carry business.journey, and its dashboard passes
// the slice variable to teia's queries: the panel's buttons set the variable.
for (const width of [1600, 600]) {
  test(`grafana: the panel picks a slice through the dashboard variable (${width}px)`, async ({ page }, info) => {
    test.setTimeout(120_000);
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/d/teia-overview/teia?var-slice=&refresh=&theme=light');
    const root = page.getByTestId('teia-panel');
    const node = (id: string) => root.locator(`.node[data-id="${id}"]`);
    await expect(node('catalog')).toHaveCount(1, { timeout: 60_000 });
    // A narrow panel leaves the choice to Filter; a wide one offers it in the bar too.
    await expect(root.locator('.slice-pick').first()).toBeVisible({ visible: width > 760 });
    await pickSlice(root, 'journey: checkout');
    await expect(page).toHaveURL(/var-slice=journey(%3A|:)checkout/);
    await expect(node('catalog')).toHaveCount(0, { timeout: 60_000 });
    await expect(node('orders')).toHaveCount(1);
    await expect(root.locator('.fchip.slice')).toContainText('Calls of journey = checkout');
    await expect(root.locator('.slice-note').first()).toContainText('metrics of these calls only');
    await page.screenshot({ path: info.outputPath(`grafana-slice-${width}.png`), animations: 'disabled' });
    await root.getByRole('button', { name: 'Show every call', exact: true }).click();
    await expect(node('catalog')).toHaveCount(1, { timeout: 60_000 });
    await expect(root.locator('.fchip.slice')).toHaveCount(0);
  });
}
