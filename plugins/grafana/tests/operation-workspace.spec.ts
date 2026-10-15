import { expect, test, type Locator } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { mountFixture, type FixtureWindow } from './teia.fixture';
import type { TeiaSnapshot } from '../src/teia/teia-ui';

const operations: NonNullable<TeiaSnapshot['operations']> = JSON.parse(readFileSync(
  resolve('../../internal/journey/testdata/operation-runtime.json'), 'utf8'));

function snapshot(): TeiaSnapshot {
  const ids = ['web', 'orders', 'catalog', 'db', 'outside'];
  return {
    round: 1, generatedAt: '2026-10-15T12:05:00Z', timeMode: 'current', operations,
    nodes: ids.map(id => ({ id, kind: id === 'db' ? 'database' : 'service', health: 'failing', visibility: 'full',
      sources: ['metrics'], requestRate: 999, errorRate: 0.9, labels: { journey: 'checkout,browse' } })),
    edges: [['web', 'orders'], ['web', 'catalog'], ['orders', 'db'], ['catalog', 'db'], ['outside', 'db']]
      .map(([from, to]) => ({ from, to, health: 'failing', state: 'ok', declared: true, observed: true,
        requestRate: 999, errorRate: 0.9, sources: ['metrics'], signals: ['metrics'] })),
    sources: [], findings: [], coverage: { nodes: 5, edges: 5, visibility: 1, declared: 1, observed: 1 },
  };
}

async function openOperations(root: Locator) {
  await root.getByRole('button', { name: 'Investigate', exact: true }).click();
  await root.getByRole('button', { name: 'Operations and traces', exact: true }).click();
  return root.getByRole('dialog', { name: 'Investigation workspace' });
}

async function names(root: Locator) {
  return (await root.locator('.node .title').allTextContents()).sort();
}

test('sampled operations separate shared services and return to the unchanged platform view', async ({ page }) => {
  const root = await mountFixture(page, snapshot());
  const checkout = operations.scopes.find(s => s.operation === 'POST /checkout')!;
  const browse = operations.scopes.find(s => s.operation === 'GET /browse')!;
  let dialog = await openOperations(root);
  await dialog.getByRole('combobox', { name: 'Operation', exact: true }).selectOption(checkout.id);
  await expect(dialog.locator('#operationDetails')).toContainText('2 sampled roots');
  await expect(dialog.locator('#operationDetails')).toContainText('1 traces with gaps');
  await expect(dialog).toContainText('traffic denominator are unknown');
  await dialog.getByRole('button', { name: 'Show sampled operation', exact: true }).click();
  expect(await names(root)).toEqual(['db', 'orders', 'web']);
  await expect(root.locator('.edge')).toHaveCount(2);
  await root.locator('.node').first().focus(); await page.keyboard.press('Enter');
  await expect(root.locator('#pane-details')).not.toContainText('999');
  await expect(root.locator('#pane-details')).not.toContainText('90%');
  await root.locator('#drawerClose').click();
  dialog = await openOperations(root);
  await dialog.getByRole('combobox', { name: 'Operation', exact: true }).selectOption(browse.id);
  await dialog.getByRole('button', { name: 'Show sampled operation', exact: true }).click();
  expect(await names(root)).toEqual(['catalog', 'db', 'web']);
  dialog = await openOperations(root);
  await dialog.getByRole('button', { name: 'Return to topology', exact: true }).click();
  expect(await names(root)).toEqual(['catalog', 'db', 'orders', 'outside', 'web']);
  await expect(root.locator('.edge')).toHaveCount(5);
});

test('partial trace preserves its gap and a vanished operation never falls back to platform calls', async ({ page }) => {
  const root = await mountFixture(page, snapshot());
  const checkout = operations.scopes.find(s => s.operation === 'POST /checkout')!;
  const partial = checkout.traces.find(t => t.structuralStatus === 'gaps-detected')!;
  const dialog = await openOperations(root);
  await dialog.getByRole('combobox', { name: 'Operation', exact: true }).selectOption(checkout.id);
  await dialog.getByRole('combobox', { name: 'Trace', exact: true }).selectOption(partial.traceId);
  await expect(dialog.locator('#operationSpans')).toContainText(partial.missingParentSpanIds[0]);
  await expect(dialog.locator('#operationSpans')).toContainText('Linked sample does not mean a complete trace');
  await dialog.getByRole('button', { name: 'Show sampled operation', exact: true }).click();
  expect(await names(root)).toEqual(['web']);
  await expect(root.locator('.edge')).toHaveCount(0);
  await page.evaluate(() => {
    const w = window as FixtureWindow;
    w.fixture.update({ ...w.fixtureSnapshot, round: 2, operations: { ...w.fixtureSnapshot.operations!, scopes: [],
      sources: [{ source: 'tempo', status: 'unavailable', requestedTraces: 0, fetchedTraces: 0, failedTraces: 0,
        queriedServices: 0, availableServices: 0, truncated: false }] } });
  });
  await expect(root.locator('.node')).toHaveCount(0);
  await expect(root.locator('.edge')).toHaveCount(0);
  await expect(root.locator('#investigationSummary')).toContainText('unavailable in this capture');
});

test('isolated operation lab preserves real shared-database trace attribution', async ({ page, request }, info) => {
  test.skip(process.env.TEIA_OPERATION_LAB !== '1', 'Requires the opt-in checkout/browse lab instrumentation.');
  test.setTimeout(90_000);
  const base = process.env.TEIA_URL || 'http://localhost:8482';
  let actual: TeiaSnapshot;
  await expect.poll(async () => {
    const response = await request.get(base + '/api/v1/snapshot');
    actual = await response.json();
    return ['POST /checkout', 'GET /browse'].every(operation => actual.operations?.scopes.some(s =>
      s.entryService === 'web' && s.operation === operation && s.edges.some(e => e.to === 'orders-db')));
  }, { timeout: 60_000, intervals: [2_000] }).toBe(true);
  const checkout = actual!.operations!.scopes.find(s => s.entryService === 'web' && s.operation === 'POST /checkout')!;
  const browse = actual!.operations!.scopes.find(s => s.entryService === 'web' && s.operation === 'GET /browse')!;
  expect(checkout.nodes.some(n => n.id === 'catalog')).toBe(false);
  expect(browse.nodes.some(n => n.id === 'orders')).toBe(false);
  expect(browse.nodes.some(n => n.id === 'inventory')).toBe(false);
  for (const scope of [checkout, browse]) {
    expect(scope.traces.length).toBeGreaterThan(0);
    expect(scope.traces.every(t => /^[a-f0-9]{1,32}$/.test(t.traceId))).toBe(true);
    expect(scope.edges.every(e => e.calls.every(c => scope.traces.some(t => t.traceId === c.traceId)))).toBe(true);
  }
  const root = await mountFixture(page, actual!);
  const dialog = await openOperations(root);
  await dialog.getByRole('combobox', { name: 'Operation', exact: true }).selectOption(checkout.id);
  await dialog.getByRole('button', { name: 'Show sampled operation', exact: true }).click();
  expect(await names(root)).toContain('orders-db');
  expect(await names(root)).not.toContain('catalog');
  await page.screenshot({ path: info.outputPath('real-checkout-operation.png'), animations: 'disabled' });
});
