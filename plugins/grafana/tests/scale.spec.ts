import { expect, test } from '@playwright/test';
import { graphFixture, mountFixture, type FixtureWindow } from './teia.fixture';
import { mkdirSync, writeFileSync } from 'node:fs';

// Opt-in, one worker: browser rendering + painted frames, not a layout-only timer.
// TEIA_BENCHMARK=true npm run e2e -- scale.spec.ts --workers=1
test('rendered operating envelope at 100, 500 and 1000 components', async ({ page, browser }, info) => {
  test.skip(process.env.TEIA_BENCHMARK !== 'true', 'Dedicated browser benchmark');
  test.setTimeout(300_000);
  await page.setViewportSize({ width: 1600, height: 960 });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const results = [];
  const cdp = await page.context().newCDPSession(page);
  const quantiles = (values: number[]) => {
    const sorted = [...values].sort((a, b) => a - b);
    return { median: sorted[Math.floor(sorted.length / 2)], p95: sorted[Math.ceil(sorted.length * 0.95) - 1], samples: values };
  };
  for (const count of [100, 500, 1000]) {
    const snap = graphFixture(count);
    const mountStart = Date.now();
    const root = await mountFixture(page, snap);
    await expect(root.locator('.node')).toHaveCount(count);
    await root.locator('.node').first().evaluate(element => element.getBoundingClientRect());
    const initialLoadMs = Date.now() - mountStart;
    const measured = await page.evaluate(async () => {
      const w = window as FixtureWindow;
      const host = document.getElementById('fixture')!, root = host.shadowRoot!;
      const painted = () => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
      const timing = async (run: () => void) => { const start = performance.now(); run(); await painted(); return performance.now() - start; };
      const initialRender: number[] = [], memory: number[] = [];
      for (let i = 0; i < 10; i++) {
        initialRender.push(await timing(() => {
          w.fixture.destroy();
          w.fixture = w.TeiaUI.mount(host, { mode: 'page', flow: false });
          w.fixture.update(w.fixtureSnapshot);
        }));
      }
      const selection: number[] = [], filtering: number[] = [], refresh: number[] = [], grouping: number[] = [];
      for (let round = 0; round < 20; round++) {
        const root = host.shadowRoot!;
        await painted();
        selection.push(await timing(() => {
          root.querySelectorAll('.node')[round % 10].dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
        }));
        if (!root.querySelector('.node.sel') || !root.querySelector('aside.open')) throw new Error('Selection did not open details');
        (root.querySelector('#drawerClose') as HTMLElement).click();
        await painted();
        filtering.push(await timing(() => {
          (root.querySelector('#filterBtn') as HTMLElement).click();
          const button = [...root.querySelectorAll('[data-filter-key]')].find(b => b.getAttribute('data-filter-key') === JSON.stringify(['health', 'failing']));
          if (!button) throw new Error('Health filter missing from benchmark');
          (button as HTMLElement).click();
          root.querySelector('input')?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, composed: true }));
        }));
        // Restore through the visible Clear all control, not a private implementation hook.
        const clear = [...root.querySelectorAll('button')].find(b => b.textContent?.trim() === 'Clear all');
        clear?.click();
        if (root.querySelectorAll('.node').length !== w.fixtureSnapshot.nodes.length) throw new Error('Filter was not cleared');
        await painted();
        refresh.push(await timing(() => w.fixture.update({ ...w.fixtureSnapshot, round: round + 2,
          nodes: w.fixtureSnapshot.nodes.map(n => ({ ...n, requestRate: 10 + round })) })));
        grouping.push(await timing(() => {
          const selector = root.querySelector('#groupBy') as HTMLSelectElement;
          selector.value = 'hierarchy:' + JSON.stringify(['cluster', 'k8s.namespace', 'service.name']);
          selector.dispatchEvent(new Event('change', { bubbles: true }));
        }));
        const selector = root.querySelector('#groupBy') as HTMLSelectElement;
        selector.value = ''; selector.dispatchEvent(new Event('change', { bubbles: true }));
        const perf = performance as Performance & { memory?: { usedJSHeapSize: number } };
        if (round % 5 === 0 && perf.memory) memory.push(perf.memory.usedJSHeapSize);
      }
      return { initialRender, memory, selection, filtering, refresh, grouping, nodes: root.querySelectorAll('.node').length, edges: root.querySelectorAll('.edge').length,
        domElements: root.querySelectorAll('*').length, userAgent: navigator.userAgent, hardwareConcurrency: navigator.hardwareConcurrency };
    });
    await cdp.send('HeapProfiler.collectGarbage');
    const heap = await cdp.send('Runtime.getHeapUsage');
    const retainedHeapTrend = [heap.usedSize];
    for (let batch = 0; batch < 4; batch++) {
      await page.evaluate(async batch => {
        const w = window as FixtureWindow;
        for (let i = 0; i < 5; i++) {
          w.fixture.update({ ...w.fixtureSnapshot, round: 100 + batch * 5 + i, nodes: w.fixtureSnapshot.nodes.map(n => ({ ...n, requestRate: 100 + batch * 5 + i })) });
          await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
        }
      }, batch);
      await cdp.send('HeapProfiler.collectGarbage');
      retainedHeapTrend.push((await cdp.send('Runtime.getHeapUsage')).usedSize);
    }
    results.push({ count, edges: snap.edges.length, initialLoadMs, browser: browser.version(), animation: false, viewport: '1600x960',
      selection: quantiles(measured.selection), filtering: quantiles(measured.filtering), refresh: quantiles(measured.refresh), grouping: quantiles(measured.grouping),
      initialRender: quantiles(measured.initialRender), memoryTrend: measured.memory, retainedHeapTrend,
      rendered: { nodes: measured.nodes, edges: measured.edges, dom: measured.domElements }, heapBytes: heap.usedSize,
      userAgent: measured.userAgent, hardwareConcurrency: measured.hardwareConcurrency });
    await page.screenshot({ path: info.outputPath(`scale-${count}.png`), animations: 'disabled' });
    expect(measured.nodes).toBe(count);
    expect(measured.edges).toBe(snap.edges.length);
  }
  await info.attach('scale-results', { body: JSON.stringify({ referenceBudgetMs: 200, measurements: results }, null, 2), contentType: 'application/json' });
  mkdirSync('artifacts/scale', { recursive: true });
  writeFileSync('artifacts/scale/results.json', JSON.stringify({ referenceBudgetMs: 200, measurements: results }, null, 2));
  console.log(JSON.stringify(results.map(r => ({ count: r.count, initial: r.initialRender.p95, select: r.selection.p95, filter: r.filtering.p95, refresh: r.refresh.p95, group: r.grouping.p95, heapBytes: r.heapBytes }))));
});
