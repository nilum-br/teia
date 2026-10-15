import { expect, test, type Locator, type Page } from '@playwright/test';
import { mountFixture, type FixtureWindow } from './teia.fixture';
import type { TeiaSnapshot } from '../src/teia/teia-ui';

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
    slice: journey ? { selector: { journey }, matched: true, metrics: 'scoped', sources: ['tempo'] } : undefined,
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
