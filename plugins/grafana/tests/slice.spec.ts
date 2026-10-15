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
    // A sliced round carries no sampled operations: they follow whole traces.
    slice: journey ? { selector: { journey }, matched: true, metrics: 'scoped', sources: ['tempo'] } : undefined,
    operations: journey ? undefined : operations,
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
  await root.getByRole('button', { name: 'journey: checkout', exact: true }).click();
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
  await root.getByRole('button', { name: 'journey: checkout', exact: true }).click();
  await expect.poll(() => names(root)).toEqual(['db', 'orders', 'web']);
  await root.locator('#liveBtn').click();
  await expect(root.locator('#liveBtn')).toHaveAttribute('aria-pressed', 'true');
  expect(await names(root)).toEqual(['db', 'orders', 'web']);
  await expect(root.locator('.fchip.slice')).toContainText('Calls of journey = checkout');
});

for (const width of [1600, 600]) {
  test(`a slice closes a sampled operation and lists none until every call is shown (${width}px)`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 });
    await serve(page);
    const root = await mountFixture(page, snapshot(), { api: '/api/v1/', stream: false });
    await expect(root.locator('#liveBtn')).toHaveAttribute('aria-pressed', 'false');
    let dialog = await workspace(root, 'Operations and traces');
    await dialog.getByRole('combobox', { name: 'Operation', exact: true }).selectOption(operations.scopes.find(s => s.operation === 'POST /checkout')!.id);
    await dialog.getByRole('button', { name: 'Show sampled operation', exact: true }).click();
    await expect(root.locator('#investigationSummary')).toContainText('Sampled operation');
    // The browse slice has no operation to show: the checkout operation closes.
    await root.getByRole('button', { name: 'journey: browse', exact: true }).click();
    await expect.poll(() => names(root)).toEqual(['catalog', 'db', 'web']);
    await expect(root.locator('#investigationSummary')).toBeHidden();
    dialog = await workspace(root, 'Operations and traces');
    await expect(dialog).toContainText('Operations follow whole sampled traces, which a slice does not split');
    await expect(dialog.getByRole('combobox', { name: 'Operation', exact: true })).toHaveCount(0);
    await page.screenshot({ path: info.outputPath(`operations-in-slice-${width}.png`), animations: 'disabled' });
    await dialog.getByRole('button', { name: 'Show every call', exact: true }).click();
    await expect(dialog.getByRole('combobox', { name: 'Operation', exact: true })).toBeFocused();
    await expect(root.locator('.fchip.slice')).toHaveCount(0);
    expect(await names(root)).toEqual(['catalog', 'db', 'orders', 'web']);
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
  await root.getByRole('button', { name: 'journey: checkout', exact: true }).click();
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
