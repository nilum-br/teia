import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
const { core } = createRequire(import.meta.url)('./ui/teia-ui.js');
const { sanitize, investigateSnapshot, compareSnapshots, operationSnapshot } = core;
const graph = sanitize({ nodes: ['a','b','c','d','e'].map(id => ({ id })), edges: [ ['a','b'],['b','c'],['c','a'],['b','d'],['e','a'] ].map(([from,to]) => ({ from,to })) });

test('bounded investigation follows direction, handles cycles and reports hidden boundaries', () => {
  const outward = investigateSnapshot(graph, { mode: 'neighbors', focus: 'a', direction: 'out', depth: 2 });
  assert.deepEqual(outward.snapshot.nodes.map(n => n.id), ['a','b','c','d']);
  assert.equal(outward.hiddenNodes, 1); assert.equal(outward.boundaryEdges, 1);
  assert.deepEqual(investigateSnapshot(graph, { mode: 'neighbors', focus: 'a', direction: 'in' }).snapshot.nodes.map(n => n.id), ['a','c','e']);
  const limited = investigateSnapshot(graph, { mode: 'neighbors', focus: 'a', depth: 3, limit: 2 });
  assert.equal(limited.snapshot.nodes.length, 2); assert.equal(limited.limited, true);
  const path = investigateSnapshot(graph, { mode: 'path', focus: 'e', target: 'd' });
  assert.deepEqual(path.snapshot.edges.map(e => `${e.from}->${e.to}`), ['a->b','b->d','e->a']);
  assert.equal(investigateSnapshot(graph, { mode: 'path', focus: 'd', target: 'e' }).found, false);
  const long = investigateSnapshot(graph, { mode: 'path', focus: 'e', target: 'd', limit: 2 });
  assert.equal(long.limited, true); assert.equal(long.snapshot.nodes.length, 0);
  assert.equal(graph.nodes.length, 5);
});

test('comparison separates presence and health from collection and rate jitter', () => {
  const before = sanitize({ nodes: [{id:'a',health:'healthy',labels:{z:'2',a:'1'}},{id:'b'}], edges:[{from:'a',to:'b',declared:true,observed:true}] });
  const after = sanitize({ nodes: [{id:'a',health:'failing',labels:{a:'1',z:'2'}},{id:'c'}], edges:[{from:'a',to:'c',declared:false,observed:true}] });
  const diff = compareSnapshots(before, after);
  assert.deepEqual(diff.counts, { added: 2, removed: 2, changed: 1, unchanged: 0 });
  assert.deepEqual(diff.nodes[0].changes.map(c => c.field), ['health']);
  const jitter = compareSnapshots(before, sanitize({ ...before, round:99, generatedAt:'2026-10-15T13:00:00Z', nodes:before.nodes.map(n => ({...n,requestRate:123})) }));
  assert.equal(jitter.counts.changed, 0);
});

test('comparison reports identity decisions, not their collection times or absent metadata', () => {
  const decision = (method, collectedAt) => ({ input: 'ip:10.0.0.7', resolvedId: method === 'ambiguous' ? 'ip:10.0.0.7' : 'orders', method, source: 'inventory', collectedAt });
  const capture = (method, collectedAt, edgeMethod = method) => sanitize({
    nodes: [{ id: 'orders', identity: [decision(method, collectedAt)] }, { id: 'db' }],
    edges: [{ from: 'orders', to: 'db', identity: { from: [decision(edgeMethod, collectedAt)], to: [] } }],
  });
  const moved = compareSnapshots(capture('ip-temporal', '2026-10-15T12:00:00Z'), capture('ip-temporal', '2026-10-15T12:05:00Z'));
  assert.equal(moved.counts.changed, 0, 'collection time alone is not a change');
  const reassigned = compareSnapshots(capture('ip-temporal', '2026-10-15T12:00:00Z'), capture('ambiguous', '2026-10-15T12:05:00Z'));
  assert.deepEqual(reassigned.nodes.find(r => r.id === 'orders').changes.map(c => c.field), ['identity']);
  assert.deepEqual(reassigned.edges[0].changes[0].after, ['caller ip:10.0.0.7 → ip:10.0.0.7 (ambiguous)']);
  const legacy = sanitize({ nodes: [{ id: 'orders' }, { id: 'db' }], edges: [{ from: 'orders', to: 'db' }] });
  assert.equal(compareSnapshots(legacy, capture('name', '2026-10-15T12:00:00Z')).counts.changed, 0, 'missing metadata is not comparable');
});

test('comparison refuses captures that hold different calls', () => {
  const capture = (journey, nodes) => sanitize({ nodes: nodes.map(id => ({ id })), edges: nodes.slice(1).map(id => ({ from: 'web', to: id })),
    slice: journey ? { selector: { journey }, matched: true, metrics: 'scoped', sources: ['tempo'] } : undefined });
  const whole = capture('', ['web', 'orders', 'catalog']), checkout = capture('checkout', ['web', 'orders']), browse = capture('browse', ['web', 'catalog']);
  const mixed = compareSnapshots(whole, checkout);
  assert.equal(mixed.comparable, false, 'a slice is not the whole map minus what changed');
  assert.deepEqual(mixed.scope, { before: '', after: 'journey:checkout' });
  assert.deepEqual([mixed.nodes, mixed.edges, mixed.counts], [[], [], { added: 0, removed: 0, changed: 0, unchanged: 0 }]);
  assert.equal(compareSnapshots(checkout, browse).comparable, false);
  const same = compareSnapshots(checkout, capture('checkout', ['web', 'orders', 'db']));
  assert.equal(same.comparable, true);
  assert.deepEqual(same.counts, { added: 2, removed: 0, changed: 0, unchanged: 3 });
  assert.equal(compareSnapshots(whole, whole).comparable, true);
});

test('operation projection excludes global health, rates, other operations and async links', () => {
  const operations = JSON.parse(readFileSync(new URL('../journey/testdata/operation-runtime.json', import.meta.url)));
  const names = [...new Set(operations.scopes.flatMap(s => s.nodes.map(n => n.id)))];
  const base = sanitize({ nodes:names.map(id => ({id,health:'failing',requestRate:999,errorRate:0.9,reasons:[{cause:'errors',health:'failing',value:0.9,limit:0.05}],measures:[{cause:'locks',what:'waiting',value:10}]})), edges:[], operations, findings:[{title:'Global fault',subject:names[0]}] });
  for (const scope of operations.scopes) {
    const projected = operationSnapshot(base, scope.id);
    assert.deepEqual(new Set(projected.nodes.map(n => n.id)), new Set(scope.nodes.map(n => n.id)));
    assert.equal(projected.findings.length, 0); assert.equal(projected.drift, false);
    for (const n of [...projected.nodes,...projected.edges]) {
      assert.equal(n.health, 'unknown'); assert.equal(n.requestRate, 0); assert.equal(n.errorsUnknown, true);
      assert.equal(n.reasons.length, 0); assert.equal(n.measures.length, 0);
    }
    for (const trace of scope.traces) {
      const selected = operationSnapshot(base, scope.id, trace.traceId);
      assert.ok(selected.edges.every(e => scope.edges.some(original => original.from === e.from && original.to === e.to && original.calls.some(c => c.traceId === trace.traceId))));
    }
  }
  assert.equal(operationSnapshot(base, 'missing').nodes.length, 0);
  assert.equal(operationSnapshot(base, operations.scopes[0].id, 'missing-trace').nodes.length, 0);
  assert.equal(base.nodes[0].health, 'failing');
});

test('an operation a slice did not sample says so instead of reading as missing data', () => {
  const operations = JSON.parse(readFileSync(new URL('../journey/testdata/operation-runtime.json', import.meta.url)));
  const [kept, ...others] = operations.scopes;
  const slice = { selector: { journey: 'checkout' }, matched: true, metrics: 'scoped', sources: ['tempo'] };
  const sliced = sanitize({ nodes: kept.nodes.map(n => ({ id: n.id })), edges: [], operations: { ...operations, scopes: [kept] }, slice });
  assert.equal(operationSnapshot(sliced, kept.id).operationContext.unavailable, false);
  const absent = operationSnapshot(sliced, others[0].id).operationContext;
  assert.deepEqual([absent.operation, absent.unavailable], ['not sampled in this slice', true]);
  const whole = sanitize({ nodes: [], edges: [], operations: { ...operations, scopes: [] } });
  assert.equal(operationSnapshot(whole, kept.id).operationContext.operation, 'unavailable in this capture');
});
