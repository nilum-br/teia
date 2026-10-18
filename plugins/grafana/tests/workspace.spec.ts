import { expect, test } from '@playwright/test';
import { graphFixture, mountFixture, type FixtureWindow } from './teia.fixture';

test('focused directed paths, table search and reset preserve full topology', async ({ page }) => {
  const snap = graphFixture(60);
  const root = await mountFixture(page, snap);
  const errors: string[] = []; page.on('pageerror', e => errors.push(e.message));
  await root.locator('#exploreBtn').click();
  await root.locator('#investigationMode').selectOption('neighbors');
  await root.locator('#investigationFocus').fill('service-0000');
  await root.locator('#investigationDirection').selectOption('out');
  await root.locator('#investigationDisplay').selectOption('list');
  await root.getByRole('button', {name:'Apply investigation',exact:true}).click();
  await expect(root.locator('#entityTable')).toBeVisible();
  await expect(root.locator('#investigationSummary')).toContainText('outside view');
  await root.locator('#search').fill('service-0000');
  await expect(root.locator('#entityTable tbody tr')).toHaveCount(3);
  await root.locator('#entityTable').getByRole('button', {name:'service-0000',exact:true}).click();
  await expect(root.locator('#pane-details')).toContainText('service-0000');
  await root.locator('#drawerClose').click();
  await root.getByRole('button', {name:'Reset investigation',exact:true}).click();
  await expect(root.locator('.node')).toHaveCount(60);
  await root.locator('#search').fill('');
  await root.locator('#exploreBtn').click();
  await root.locator('#investigationMode').selectOption('path');
  await root.locator('#investigationFocus').fill('service-0059');
  await root.locator('#investigationTarget').fill('service-0000');
  await root.getByRole('button', {name:'Apply investigation',exact:true}).click();
  await expect(root.locator('#investigationSummary')).toContainText('No directed path');
  await expect(root.locator('.node')).toHaveCount(0);
  expect(errors).toEqual([]);
});

for (const theme of ['light','dark'] as const) for (const width of [600,1600]) {
  test(`comparison upload and accessible workspace ${theme} ${width}`, async ({page}, info) => {
    await page.setViewportSize({width,height:900});
    const before = graphFixture(6), after = structuredClone(before);
    after.generatedAt = '2026-10-15T13:00:00Z'; after.nodes[0].health = 'healthy'; after.nodes[0].reasons = [];
    after.nodes.push({...after.nodes[1],id:'new-service'});
    const root = await mountFixture(page,before,{theme});
    const errors: string[] = []; page.on('pageerror',e=>errors.push(e.message));
    await root.locator('#exploreBtn').focus(); await page.keyboard.press('Enter');
    await root.locator('[data-workspace="compare"]').click();
    await root.getByLabel('Upload before snapshot').setInputFiles({name:'before.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(before))});
    await root.getByLabel('Upload after snapshot').setInputFiles({name:'after.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(after))});
    await expect(root.locator('#comparisonResult')).toContainText('Only after');
    await expect(root.locator('#comparisonResult .change-changed')).toContainText('health');
    await expect(root.locator('.comparison-map')).toHaveCount(2);
    await expect(root.locator('.comparison-map').first().locator('#timeContext')).toContainText('Frozen capture');
    // The maps mark the differences on the names, like tracked changes; amber, red,
    // "+" and dashes keep their meaning on the map.
    const beforeMap = root.locator('.comparison-map').first(), afterMap = root.locator('.comparison-map').nth(1);
    const added = afterMap.locator('.node[data-id="new-service"]');
    await expect(added).toHaveClass(/\bdiff-added\b/);
    await expect(added).toHaveAttribute('aria-label', /only in the after capture/);
    await expect(added.locator('.title')).toHaveCSS('text-decoration-line', 'underline');
    await expect(added.locator('.title')).not.toHaveText(/^\+/);
    await expect(added.locator('.card')).toHaveCSS('stroke-dasharray', 'none');
    for (const map of [beforeMap, afterMap]) {
      await expect(map.locator('.node[data-id="service-0000"]')).toHaveClass(/\bdiff-changed\b/);
      await expect(map.locator('.node[data-id="service-0000"] .title')).toHaveText(/^~ /);
    }
    await expect(beforeMap.locator('.node[data-id="service-0001"]')).toHaveClass(/\bnodiff\b/);
    await expect(beforeMap.locator('.node.diff-added')).toHaveCount(0);
    await expect(root.locator('#comparisonResult')).toContainText('underlined is only after, struck through is only before');
    const dialog = root.locator('#investigateDialog[open]');
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    await page.screenshot({path:info.outputPath('comparison.png'),animations:'disabled'});
    await root.locator('#comparisonResult').scrollIntoViewIfNeeded();
    await page.screenshot({path:info.outputPath('comparison-maps.png'),animations:'disabled'});
    await root.locator('#closeInvestigateBtn').first().click();
    await expect(root.locator('#exploreBtn').first()).toBeFocused();
    expect(errors).toEqual([]);
  });
}

test('source wizard rejects embedded credentials and downloads a usable fragment', async ({page}) => {
  const root = await mountFixture(page,graphFixture());
  await root.locator('#exploreBtn').click(); await root.locator('[data-workspace="setup"]').click();
  await root.locator('#setupURL').fill('http://user:secret@example.test');
  await root.getByRole('button',{name:'Prepare configuration',exact:true}).click();
  await expect(root.locator('#workspaceStatus')).toContainText('without credentials');
  await root.locator('#setupURL').fill('http://prometheus:9090');
  const download = page.waitForEvent('download');
  await root.getByRole('button',{name:'Prepare configuration',exact:true}).click();
  expect((await download).suggestedFilename()).toBe('teia-source.yaml');
  await expect(root.locator('#setupPreview')).toContainText('presets: [auto]');
});

test('query field wizard sends required columns without fabricating optional metrics', async ({page}) => {
  const root = await mountFixture(page,graphFixture());
  await page.evaluate(() => {
    const w = window as FixtureWindow & { mapped?: unknown };
    w.fixture.setOptions({inputFrames:[{key:'ref:A',fields:['workload']},{key:'ref:B',fields:['caller','callee']}],fieldMapping:{},onMappingChange:mapping=>{w.mapped=mapping;}});
  });
  await root.locator('#exploreBtn').click(); await root.locator('[data-workspace="setup"]').click();
  await root.locator('#mapping-nodes').selectOption('ref:A');
  await root.locator('#mapping-edges').selectOption('ref:B');
  await root.getByRole('button',{name:'Save field mapping',exact:true}).click();
  await expect(root.locator('#workspaceStatus')).toContainText('required identity');
  await root.locator('#mapping-nodes-id').selectOption('workload');
  await root.locator('#mapping-edges-source').selectOption('caller');
  await root.locator('#mapping-edges-target').selectOption('callee');
  await root.getByRole('button',{name:'Save field mapping',exact:true}).click();
  expect(await page.evaluate(()=>(window as unknown as {mapped:unknown}).mapped)).toEqual({nodes:{frame:'ref:A',id:'workload'},edges:{frame:'ref:B',source:'caller',target:'callee'}});
});

test('query review exports frozen input and a fixed CLI command', async ({page}) => {
  const snap=graphFixture(14); snap.edges[0].declared=false; snap.edges[0].state='ghost';
  const root=await mountFixture(page,snap);
  await page.evaluate(id => (window as FixtureWindow).fixture.focus(id), snap.edges[0].from+'->'+snap.edges[0].to);
  await root.getByRole('button',{name:'Review contract proposal',exact:true}).click();
  await root.locator('#reviewRationale').fill('The owning service requires this dependency.');
  await root.getByRole('button',{name:'Prepare review proposal',exact:true}).click();
  await expect(root.locator('#reviewOutput')).toContainText('teia review propose --snapshot snapshot.json --request request.json');
  const download=page.waitForEvent('download'); await root.getByRole('button',{name:'Download request.json',exact:true}).click();
  expect((await download).suggestedFilename()).toBe('request.json');
});

test('plain query frames without declared intent do not offer a contract proposal', async ({page}) => {
  const snap=graphFixture(14); snap.edges[0].declared=false; snap.drift=false;
  const root=await mountFixture(page,snap);
  await page.evaluate(id=>(window as FixtureWindow).fixture.focus(id),snap.edges[0].from+'->'+snap.edges[0].to);
  await expect(root.locator('#pane-details h2')).toContainText(snap.edges[0].to);
  await expect(root.getByRole('button',{name:'Review contract proposal',exact:true})).toHaveCount(0);
});

test('table pagination retains keyboard focus on narrow screens', async ({page},info) => {
  await page.setViewportSize({width:600,height:800});
  const root=await mountFixture(page,graphFixture(100));
  await root.locator('#exploreBtn').click(); await root.locator('#investigationDisplay').selectOption('list');
  await root.getByRole('button',{name:'Apply investigation',exact:true}).click();
  await expect(root.locator('#entityTable tbody tr')).toHaveCount(50);
  // The table replaces the map, so map controls must not cover its rows.
  for (const selector of ['.zoom', '#legend']) await expect(root.locator(selector)).toBeHidden();
  await root.locator('#tableNext').focus(); await page.keyboard.press('Enter');
  await expect(root.locator('#tableNext')).toBeFocused();
  await expect(root.locator('#entityTable')).toContainText('page 2 of');
  expect(await root.locator('#entityTable').evaluate(table => table.scrollTop)).toBe(0);
  await expect(root.locator('#tableNext')).toBeInViewport();
  // The pager after the last row moves focus to the visible top pager of the next page.
  await root.locator('#tableNextEnd').click();
  await expect(root.locator('#entityTable')).toContainText('page 3 of');
  await expect(root.locator('#tableNext')).toBeFocused();
  expect(await root.locator('#entityTable').evaluate(table => table.scrollTop)).toBe(0);
  await page.screenshot({path:info.outputPath('table-mobile.png'),animations:'disabled'});
});

test('component details open a bounded neighborhood and a prefilled directed path', async ({page},info) => {
  const root=await mountFixture(page,graphFixture(60));
  await page.evaluate(id=>(window as FixtureWindow).fixture.focus(id),'service-0012');
  await root.getByRole('button',{name:'Show neighborhood',exact:true}).click();
  await expect(root.locator('#investigationSummary')).toContainText('components outside view');
  const shown=await root.locator('.node').evaluateAll(nodes=>nodes.map(n=>n.getAttribute('data-id')));
  expect(shown).toContain('service-0012');
  expect(shown.length).toBeLessThan(60);
  await expect(root.locator('.node[data-id="service-0012"]')).toHaveClass(/\bsel\b/);
  await page.screenshot({path:info.outputPath('neighborhood.png'),animations:'disabled'});
  await root.getByRole('button',{name:'Find path…',exact:true}).click();
  await expect(root.locator('#investigationMode')).toHaveValue('path');
  await expect(root.locator('#investigationFocus')).toHaveValue('service-0012');
  await expect(root.locator('#investigationTarget')).toBeFocused();
  await page.keyboard.type('service-0059');
  await root.getByRole('button',{name:'Apply investigation',exact:true}).click();
  // Ten layers of six: a directed route from layer 2 to layer 9 has eight components.
  await expect(root.locator('.node')).toHaveCount(8);
  await expect(root.locator('.edge')).toHaveCount(7);
  await expect(root.locator('#investigationSummary')).not.toContainText('No directed path');
  await page.screenshot({path:info.outputPath('path.png'),animations:'disabled'});
});

test('stored observations compare and produce a validated review with exact timestamps', async ({page,request},info) => {
  const base=process.env.TEIA_URL || 'http://localhost:8480';
  const listing=await (await request.get(base+'/api/v1/history')).json();
  const record=listing.snapshots.at(-1);
  const snapshot=await (await request.get(base+'/api/v1/history/'+record.id)).json();
  const edge=snapshot.edges.find((e:{from:string;to:string;declared:boolean;observed:boolean}) => e.observed&&!e.declared&&!e.from.startsWith('ip:')&&!e.to.startsWith('ip:'));
  expect(edge).toBeTruthy();
  const view={version:1,history:record.id,select:edge.from+'->'+edge.to,flow:false};
  await page.goto(base+'/?teiaView='+encodeURIComponent(JSON.stringify(view)));
  const root=page.locator('#teia');
  await expect(root.locator('#timeContext')).toContainText('Historical snapshot');
  await root.getByRole('button',{name:'Review contract proposal',exact:true}).click();
  await root.locator('#reviewRationale').fill('The reviewed dependency is required by the owning service.');
  const submitted=page.waitForRequest(r=>r.url().endsWith('/review/proposals')&&r.method()==='POST');
  await root.getByRole('button',{name:'Prepare review proposal',exact:true}).click();
  expect((await submitted).postDataJSON().generatedAt).toBe(snapshot.generatedAt);
  await expect(root.locator('#workspaceStatus')).toContainText('Proposal prepared');
  await expect(root.locator('#reviewOutput')).toContainText('flows.yaml');
  await expect(root.getByRole('button',{name:'Prepare review proposal',exact:true})).toBeEnabled();
  await page.screenshot({path:info.outputPath('review.png'),animations:'disabled'});
  await root.locator('[data-workspace="compare"]').click();
  await root.locator('#compare-before option').nth(1).waitFor({state:'attached'});
  const id=await root.locator('#compare-before option').nth(1).getAttribute('value');
  await root.locator('#compare-before').selectOption(id!); await root.locator('#compare-after').selectOption(id!);
  await expect(root.locator('#comparisonResult')).toContainText('0 changed');
});

test('proposal conflicts require a fresh review instead of retrying silently', async ({page}) => {
  const snapshot=graphFixture(14); snapshot.edges[0].declared=false; snapshot.edges[0].state='ghost';
  snapshot.generatedAt='2026-10-15T12:00:00.123456789Z';
  await page.route('**/api/v1/snapshot',route=>route.fulfill({json:snapshot}));
  await page.route('**/api/v1/changes',route=>route.fulfill({json:[]}));
  let submitted=0;
  await page.route('**/api/v1/review/proposals',route=>{
    submitted++; expect(route.request().postDataJSON().generatedAt).toBe(snapshot.generatedAt);
    return route.fulfill({status:409,json:{error:'capture changed'}});
  });
  const root=await mountFixture(page,snapshot,{api:'/api/v1/',stream:false});
  await page.evaluate(id=>(window as FixtureWindow).fixture.focus(id),snapshot.edges[0].from+'->'+snapshot.edges[0].to);
  await root.getByRole('button',{name:'Review contract proposal',exact:true}).click();
  await root.locator('#reviewRationale').fill('This dependency is required.');
  await root.getByRole('button',{name:'Prepare review proposal',exact:true}).click();
  await expect(root.locator('#workspaceStatus')).toContainText('The capture changed');
  await expect(root.getByRole('button',{name:'Prepare review proposal',exact:true})).toBeEnabled();
  expect(submitted).toBe(1);
});

for (const surface of ['standalone','grafana'] as const) {
  test(`${surface}: installed workspace and snapshot comparison`, async ({page},info) => {
    await page.goto(surface==='standalone' ? (process.env.TEIA_URL || 'http://localhost:8480')+'/?flow=0&live=0' : '/d-solo/teia-overview/teia?panelId=1&theme=light&refresh=');
    const root=surface==='standalone'?page.locator('#teia'):page.getByTestId('teia-panel');
    await expect(root.locator('.node').first()).toBeVisible({timeout:30000});
    await root.locator('#exploreBtn').click(); await root.locator('[data-workspace="compare"]').click();
    await root.getByRole('button',{name:'Capture current snapshot as before',exact:true}).click();
    await root.getByRole('button',{name:'Capture current snapshot as after',exact:true}).click();
    await expect(root.locator('#comparisonResult .comparison-map')).toHaveCount(2);
    await expect(root.locator('#comparisonResult')).toContainText('0 changed');
    await page.screenshot({path:info.outputPath(surface+'-workspace.png'),animations:'disabled'});
  });
}
