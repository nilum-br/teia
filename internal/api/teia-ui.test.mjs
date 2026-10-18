// Tests for the pure part of the map component (TeiaUI.core).
//
//   node --test internal/api/teia-ui.test.mjs
//
// They live next to api.go rather than in ui/, which is embedded and served.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';

const { core } = createRequire(import.meta.url)('./ui/teia-ui.js');
const { sanitize, signature, indexSnapshot, nodeSignals, labelKeys, buildView, matcher, layered, steadyLayout, provenanceText, particleBudget, safeApiBase, categoryOf, cardSubtitle, fmtMs, fmtSec, fmtBytes, reasonTag, compact, placeTag, parseJourney, journeyOf, scopeSnapshot, scopePins, journeyWhy, journeyNames, scopeOf, facetsOf, filtersFromParams, filtersToParams, filterLabel, journeyKeyOf, journeyPorts, packColumn, frameLayout, scopeStats } = core;

const node = (id, extra = {}) => ({ id, kind: 'service', health: 'healthy', visibility: 'full', sources: [], ...extra });
const edge = (from, to, extra = {}) => ({ from, to, state: 'ok', health: 'healthy', declared: true, observed: true, requestRate: 1, errorRate: 0, signals: ['traces'], sources: [], ...extra });
const snap = (nodes, edges, extra = {}) => sanitize({ round: 1, generatedAt: '2026-10-14T12:00:00Z', nodes, edges, ...extra });
const view = (s, opts) => buildView(s, indexSnapshot(s), opts);

describe('portable investigation contracts', () => {
  it('uses the backend scope, entry edges and boundary definitions for the shared ownership fixture', () => {
    const fixture = JSON.parse(readFileSync(new URL('../journey/testdata/shared.json', import.meta.url)));
    const scopes = Object.entries(fixture.expected).map(([name, scope]) => ({ name, key: 'journey', ...scope }));
    const s = sanitize({ ...fixture.snapshot, journeys: { version: 1, mode: 'ownership', key: 'journey', maxDepth: 2, maxNodes: 200, scopes } });
    for (const expected of scopes) {
      const j = journeyOf(s, expected.name);
      assert.equal(j.backend, true);
      assert.deepEqual(Object.fromEntries(j.roles), expected.roles);
      assert.deepEqual(j.entryEdges, expected.entryEdges);
      assert.equal(j.truncated, expected.truncated);
      const ports = journeyPorts(s, j, { maxPerSide: 100 });
      assert.deepEqual(ports.flatMap(p => p.edges.map(e => `${p.kind}:${e.from}->${e.to}`)).sort(),
        expected.boundaryPorts.map(p => `${p.kind}:${p.edgeId}`).sort());
      const fallback = journeyOf(sanitize(fixture.snapshot), expected.name);
      assert.deepEqual(Object.fromEntries(fallback.roles), expected.roles);
      assert.equal(fallback.truncated, expected.truncated);
    }
    assert.deepEqual(journeyNames(s), ['browse', 'checkout']);
    assert.equal(journeyOf(sanitize(fixture.snapshot), 'checkout', { maxNodes: 1 }).roles.size, 1);
  });

  it('preserves rule provenance and rejects unsafe evidence links while retaining per-member context', () => {
    const links = [{ label: 'Logs', url: 'https://example.test/a?service=a%26b', kind: 'logs', description: 'Service and time search' },
      { label: 'Bad', url: 'javascript:alert(1)' }, { label: 'Secret', url: 'https://user:pass@example.test/' }];
    const rule = { cause: 'latency', name: 'batch', match: 'identity', degraded: 2000, failing: 4000, unit: 'milliseconds' };
    const reason = { cause: 'latency', health: 'degraded', what: 'p95', value: 2500, limit: 2000, rule: 'batch', unit: 'milliseconds' };
    const s = snap([node('a', { labels: { team: 'x' }, links, healthRules: [rule], reasons: [reason] }), node('b', { labels: { team: 'x' } })], []);
    const g = view(s, { groupBy: 'team' }).nodes.get('group:x');
    assert.equal(g.links.length, 1);
    assert.equal(g.links[0].label, 'a: Logs');
    assert.equal(g.healthRules[0].subject, 'a');
    assert.equal(g.reasons[0].rule, 'batch');
    const origin = new Map([['a', s.nodes[0]]]);
    assert.equal(core.mapReasons({ to: 'a', reasons: [reason] }, origin).length, 0);
    assert.equal(core.mapReasons({ to: 'a', reasons: [{ ...reason, value: 3000 }] }, origin).length, 1);
  });

  it('keeps equal namespace names separate across clusters and preserves external identities while expanding', () => {
    const s = snap([node('a', { labels: { cluster: 'one', namespace: 'shop', 'service.name': 'web' } }),
      node('b', { labels: { cluster: 'two', namespace: 'shop', 'service.name': 'web' } }), node('external', { kind: 'external' })], [edge('a', 'external'), edge('b', 'external')]);
    const groupBy = 'hierarchy:' + JSON.stringify(['cluster', 'namespace', 'service.name']);
    const expanded = new Set();
    let v = view(s, { groupBy, expanded });
    assert.notEqual(v.groupOf('a'), v.groupOf('b'));
    assert.equal(v.nodes.has('external'), true);
    for (const n of v.nodes.values()) if (n.group) expanded.add(n.expansionKey);
    v = view(s, { groupBy, expanded });
    assert.notEqual(v.groupOf('a'), v.groupOf('b'));
    assert.equal(v.nodes.get(v.groupOf('a')).title, 'shop');
    for (const g of core.groupingPath(s.nodes[0], groupBy)) expanded.add(g.expansionKey);
    v = view(s, { groupBy, expanded });
    assert.equal(v.nodes.has('a'), true);
    assert.equal(v.edges.has('a->external'), true);
    assert.equal(v.nodes.get(v.groupOf('b')).members[0].id, 'b');
  });

  it('ranks measured errors before missing errors and retains honest temporal modes', () => {
    const s = snap([node('a'), node('b'), node('c')], [edge('a', 'b', { errorRate: 0, errorsUnknown: true }), edge('a', 'c', { errorRate: 0 })]);
    assert.deepEqual(core.neighbourOrder([...view(s).edges.values()], 'errors').map(e => e.to), ['c', 'b']);
    const old = sanitize({ ...s, timeMode: 'historical', history: { id: 'test', availability: 'stored-snapshot' } });
    assert.equal(old.timeMode, 'historical');
    assert.notEqual(signature(old), signature(s));
  });
});

describe('identity provenance', () => {
  it('retains source decisions and candidate times without fabricating observations or confidence', () => {
    const resolution = { input: 'ip:10.0.0.1', resolvedId: 'orders', method: 'inferred', source: 'network', sourceType: 'prometheus',
      detail: 'Retained inventory', timeBasis: 'unknown', confidence: 99, retained: true,
      candidates: [{ id: 'orders', source: 'inventory', sourceType: 'kubernetes', since: '2026-10-15T10:00:00Z', collectedAt: '2026-10-15T11:00:00Z', retained: true }],
      alternatives: ['orders', 'ip:10.0.0.1'] };
    const s = snap([node('orders', { identity: [resolution] }), node('db')], [edge('orders', 'db', { identity: { from: [resolution], to: [] } })]);
    const normalized = s.nodes[0].identity[0];
    assert.equal(normalized.collectedAt, '');
    assert.equal(normalized.observedAt, '');
    assert.equal(normalized.candidates[0].collectedAt, '2026-10-15T11:00:00.000Z');
    assert.equal(normalized.retained, true);
    assert.equal(Object.hasOwn(normalized, 'confidence'), false);
    assert.deepEqual(s.edges[0].identity.from, s.nodes[0].identity);
    assert.notEqual(signature(s), signature(sanitize({ ...s, nodes: s.nodes.map(n => ({ ...n, identity: [] })) })));
  });

  it('drops malformed records and treats absent legacy metadata as unavailable', () => {
    const valid = { input: 'orders', resolvedId: 'orders', method: 'name', source: 'tempo', candidates: [null, {}, { id: 'candidate', source: 'inventory', since: 'not a time', retained: 'true' }], observedAt: 'not a time', timeBasis: 'guessed', retained: 'true', windowSeconds: Infinity };
    const s = snap([node('orders', { identity: [null, 9, 'oops', {}, { ...valid, method: '99%-certain' }, valid] }), node('db')],
      [edge('orders', 'db', { identity: [] })]);
    assert.equal(s.nodes[0].identity.length, 1);
    assert.equal(s.nodes[0].identity[0].retained, false);
    assert.equal(s.nodes[0].identity[0].observedAt, '');
    assert.equal(s.nodes[0].identity[0].timeBasis, 'unknown');
    assert.equal(s.nodes[0].identity[0].windowSeconds, 0);
    assert.equal(s.nodes[0].identity[0].candidates[0].since, '');
    assert.equal(s.nodes[0].identity[0].candidates[0].retained, false);
    assert.deepEqual(s.nodes[1].identity, []);
    assert.equal(s.edges[0].identity, undefined);
  });

  it('preserves per-member and caller/callee decisions while grouping', () => {
    const decision = id => ({ input: id, resolvedId: id, method: 'name', source: 'tempo' });
    const s = snap([node('a', { labels: { team: 'shop' }, identity: [decision('a')] }), node('b', { labels: { team: 'shop' }, identity: [decision('b')] }), node('db')],
      [edge('a', 'db', { identity: { from: [decision('a')], to: [decision('db')] } }), edge('b', 'db', { identity: { from: [decision('b')], to: [decision('db')] } })]);
    const grouped = view(s, { groupBy: 'team' });
    assert.deepEqual(grouped.nodes.get('group:shop').members.map(n => n.identity[0].resolvedId), ['a', 'b']);
    assert.deepEqual(grouped.edges.get('group:shop->db').members.map(e => e.identity.from[0].resolvedId), ['a', 'b']);
    assert.deepEqual(grouped.edges.get('group:shop->db').members.map(e => e.identity.to[0].resolvedId), ['db', 'db']);
  });
});

describe('sanitize', () => {
  it('survives anything that is not a snapshot', () => {
    for (const raw of [null, undefined, 42, 'oops', [], { nodes: 'x', edges: {} }]) {
      const s = sanitize(raw);
      assert.deepEqual([s.nodes, s.edges, s.findings, s.sources], [[], [], [], []]);
      assert.equal(s.drift, true);
    }
  });

  it('drops invalid and duplicate nodes and self-loops, and adds nodes edges mention', () => {
    const s = sanitize({
      nodes: [node('a'), node('a'), { kind: 'service' }, node('')],
      edges: [edge('a', 'b'), edge('a', 'a'), { from: 'a' }],
    });
    assert.deepEqual(s.nodes.map(n => n.id), ['a', 'b']);
    assert.equal(s.nodes[1].kind, 'unknown');
    assert.equal(s.edges.length, 1);
  });

  it('replaces values it does not know with neutral ones', () => {
    const s = sanitize({
      nodes: [{ id: 'a', health: 'on-fire', visibility: 'x-ray', requestRate: NaN, errorRate: Infinity, sources: 'p', labels: 3 }],
      edges: [{ from: 'a', to: 'b', state: 'OK', health: 7, signals: ['traces', 3] }],
      findings: [{ title: 'fix me', severity: 'urgent' }, { severity: 'critical' }],
    });
    assert.deepEqual(
      { health: s.nodes[0].health, visibility: s.nodes[0].visibility, rate: s.nodes[0].requestRate, err: s.nodes[0].errorRate, sources: s.nodes[0].sources, labels: s.nodes[0].labels },
      { health: 'unknown', visibility: 'partial', rate: 0, err: 0, sources: [], labels: {} },
    );
    assert.equal(s.edges[0].state, 'ok');
    assert.equal(s.edges[0].health, 'unknown');
    assert.deepEqual(s.edges[0].signals, ['traces']);
    assert.deepEqual(s.findings.map(f => [f.title, f.severity]), [['fix me', 'info']]);
  });

  it('keeps drift off only when the data says so', () => {
    assert.equal(sanitize({ drift: false }).drift, false);
    assert.equal(sanitize({}).drift, true);
  });
});

describe('signature', () => {
  it('is stable across rounds that change nothing drawn', () => {
    const a = snap([node('a')], [edge('a', 'b')]);
    const b = snap([node('a')], [edge('a', 'b')], { round: 9, generatedAt: '2026-10-14T13:00:00Z' });
    assert.equal(signature(a), signature(b));
    assert.notEqual(signature(a), signature(snap([node('a')], [edge('a', 'b', { requestRate: 2 })])));
  });
});

describe('nodeSignals', () => {
  it('combines explicit labels, source types and the edges that touch the node', () => {
    const s = snap(
      [node('orders', { sources: ['docker', 'flows'], labels: { 'teia.signal.mesh': 'true', 'teia.signal.ebpf': 'true' } }), node('orders-db', { kind: 'database', sources: ['pg'] })],
      [edge('orders', 'orders-db', { signals: ['database'] })],
      { sources: [{ name: 'docker', type: 'docker' }, { name: 'flows', type: 'declared' }, { name: 'pg', type: 'postgres' }] },
    );
    const idx = indexSnapshot(s);
    assert.deepEqual(nodeSignals(s.nodes[0], idx).sort(), ['database', 'declared', 'inventory', 'mesh']);
    assert.deepEqual(nodeSignals(s.nodes[1], idx).sort(), ['database']);
  });
});

describe('labelKeys', () => {
  it('offers user labels shared by more than one node', () => {
    const s = snap([
      node('a', { labels: { team: 'x', 'teia.signal.mesh': 'true', image: 'i' } }),
      node('b', { labels: { team: 'y', tier: 'web' } }),
    ], []);
    assert.deepEqual(labelKeys(s), ['team']);
  });
});

describe('buildView', () => {
  const s = snap(
    [node('web', { labels: { team: 'front' } }), node('api', { labels: { team: 'core' }, health: 'degraded' }), node('worker', { labels: { team: 'core' } }), node('db', { kind: 'database' })],
    [
      edge('web', 'api', { requestRate: 10, errorRate: 0.1, health: 'failing' }),
      edge('web', 'worker', { requestRate: 30, errorRate: 0, state: 'ghost', declared: false }),
      edge('api', 'worker', { requestRate: 5 }),
      edge('worker', 'db', { requestRate: 2, signals: ['database'] }),
    ],
  );

  it('draws every node and edge when nothing is grouped', () => {
    const v = view(s);
    assert.equal(v.nodes.size, 4);
    assert.equal(v.edges.size, 4);
    assert.equal(v.groupOf('web'), null);
  });

  it('folds a label into one card and sums traffic between groups', () => {
    const v = view(s, { groupBy: 'team' });
    assert.deepEqual([...v.nodes.keys()].sort(), ['db', 'group:core', 'group:front']);
    const core = v.nodes.get('group:core');
    assert.equal(core.members.length, 2);
    assert.equal(core.health, 'degraded');
    assert.equal(core.internal, 1);
    const e = v.edges.get('group:front->group:core');
    assert.equal(e.requestRate, 40);
    assert.equal(e.errorRate, 1 / 40);
    assert.equal(e.state, 'ghost');
    assert.equal(e.health, 'failing');
    assert.equal(e.members.length, 2);
  });

  it('opens an expanded group back into its members', () => {
    const v = view(s, { groupBy: 'team', expanded: new Set(['core']) });
    assert.ok(v.nodes.has('api') && v.nodes.has('worker') && v.nodes.has('group:front'));
    assert.equal(v.groupOf('api'), null);
    assert.equal(v.groupOf('web'), 'group:front');
  });
});

describe('matcher', () => {
  const v = view(snap([
    node('Checkout-API', { labels: { team: 'Payments' }, tech: 'service' }),
    node('orders-db', { kind: 'database', tech: 'postgresql', health: 'failing' }),
  ], []));
  const find = q => [...v.nodes.values()].filter(matcher(q)).map(n => n.id);

  it('matches nothing special for an empty query', () => assert.equal(matcher('  '), null));
  it('matches names regardless of case', () => assert.deepEqual(find('checkout'), ['Checkout-API']));
  it('matches fields', () => {
    assert.deepEqual(find('category:relational'), ['orders-db']);
    assert.deepEqual(find('health:failing'), ['orders-db']);
  });
  it('matches labels, and every term must hold', () => {
    assert.deepEqual(find('team:payments'), ['Checkout-API']);
    assert.deepEqual(find('team:payments kind:database'), []);
  });
  it('does not treat object properties as fields', () => assert.deepEqual(find('constructor:x'), []));
});

describe('layered', () => {
  const layout = (nodes, edges) => layered(view(snap(nodes.map(id => node(id)), edges.map(([a, b]) => edge(a, b)))));
  const columns = pos => new Set(Object.values(pos).map(p => p.x)).size;

  it('puts callers to the left of what they call', () => {
    const pos = layout(['web', 'api', 'db'], [['web', 'api'], ['api', 'db']]);
    assert.ok(pos.web.x < pos.api.x && pos.api.x < pos.db.x);
  });

  it('keeps cycles to a few columns instead of exploding', () => {
    const ids = Array.from({ length: 30 }, (_, i) => 'n' + i);
    const ring = ids.map((id, i) => [id, ids[(i + 1) % ids.length]]);
    const pos = layout(ids, [...ring, ['n5', 'n1'], ['n20', 'n3']]);
    assert.equal(Object.keys(pos).length, 30);
    for (const p of Object.values(pos)) assert.ok(Number.isFinite(p.x) && Number.isFinite(p.y));
    assert.ok(columns(pos) <= 30);
    const a = layout(['a', 'b'], [['a', 'b'], ['b', 'a']]);
    assert.equal(columns(a), 2);
  });

  it('leaves no empty columns', () => {
    const pos = layout(['web', 'api', 'db', 'cron'], [['web', 'api'], ['api', 'db'], ['cron', 'db']]);
    const xs = [...new Set(Object.values(pos).map(p => p.x))].sort((a, b) => a - b);
    xs.forEach((x, i) => assert.equal(x, i * 300));
    assert.equal(pos.cron.x, pos.api.x);
  });

  it('lays out a large graph quickly', () => {
    const ids = Array.from({ length: 500 }, (_, i) => 's' + i);
    const edges = [];
    for (let i = 0; i < 1000; i++) edges.push([ids[i % 500], ids[(i * 7 + 13) % 500]]);
    const t = performance.now();
    const pos = layout(ids, edges.filter(([a, b]) => a !== b));
    assert.equal(Object.keys(pos).length, 500);
    assert.ok(performance.now() - t < 2000, 'layout took too long');
  });
});

// Every value in the details says where it comes from and when, in one form.
describe('provenanceText', () => {
  const clock = at => at.slice(11, 19);
  const ev = (source, signal, metrics, extra = {}) => ({ source, origin: '', signal, metrics, collectedAt: '2026-10-18T21:27:40.000Z',
    evaluatedAt: '2026-10-18T21:27:39.000Z', observedAt: '', windowSeconds: 120, sampleCount: 0, retained: false, ...extra });

  it('names only the sources that report the value, with the window they cover', () => {
    const evidence = [ev('prometheus', 'traces', ['requestRate', 'latency']), ev('loki/access-logs', 'logs', ['requestRate']), ev('docker', 'docker', [])];
    assert.equal(provenanceText(evidence, 'requestRate', clock), 'From prometheus (traces) and loki/access-logs (logs), the 2 min to 21:27:39.');
    assert.equal(provenanceText(evidence, 'latency', clock), 'From prometheus (traces), the 2 min to 21:27:39.');
    // frames from other tools may call them traffic and errors
    const frames = [ev('fixture-metrics', 'metrics', ['traffic', 'errors', 'latency'], { windowSeconds: 300 })];
    for (const metric of ['requestRate', 'errorRate', 'latency']) assert.equal(provenanceText(frames, metric, clock), 'From fixture-metrics (metrics), the 5 min to 21:27:39.');
  });

  it('tells sampled traces, retained data and unknown times apart', () => {
    const tempo = ev('tempo', 'traces', ['latency'], { evaluatedAt: '', observedAt: '2026-10-18T21:27:08.000Z', sampleCount: 50, windowSeconds: 600 });
    assert.equal(provenanceText([ev('prometheus', 'traces', ['latency']), tempo], 'latency', clock),
      'From prometheus (traces), the 2 min to 21:27:39; tempo (traces), 50 sampled calls in the 10 min to 21:27:08.');
    assert.equal(provenanceText([ev('prometheus', 'prometheus', ['requestRate'], { retained: true })], 'requestRate', clock),
      'From prometheus, kept from an earlier round: not current.');
    assert.equal(provenanceText([ev('x', 'x', ['requestRate'], { evaluatedAt: '', collectedAt: '' })], 'requestRate', clock), 'From x, time unknown.');
    assert.equal(provenanceText([ev('prometheus', 'prometheus', ['backlog: messages waiting'], { origin: 'kafka', windowSeconds: 0 })], 'backlog: messages waiting', clock),
      'From prometheus (kafka), at 21:27:39.');
    assert.equal(provenanceText([], 'requestRate', clock), 'Source not reported.');
  });

  it('reads every entry when no value is named (the evidence of a reason)', () => {
    assert.equal(provenanceText([ev('prometheus', 'mesh', ['errorRate'])], '', clock), 'From prometheus (mesh), the 2 min to 21:27:39.');
  });
});

// The time line is read as sentences; " · " only separates data (docs/design.md).
describe('temporalText', () => {
  const { temporalText } = core;
  const now = Date.parse('2026-10-18T12:00:30Z');
  const timed = snap([node('a', { evidence: [{ source: 'prometheus', signal: 'traces', metrics: ['requestRate'], windowSeconds: 120 }, { source: 'tempo', signal: 'traces', metrics: ['latency'], windowSeconds: 600 }] })], [],
    { generatedAt: '2026-10-18T12:00:00Z', observedAt: '2026-10-18T11:59:50Z', timeMode: 'current' });

  it('says what time the map shows, in sentences', () => {
    assert.equal(temporalText(timed, { mode: 'panel' }, now),
      'Current state, updated 30 s ago, from sources looking back 2 min and 10 min. Original observation 40 s ago. The dashboard range does not change this snapshot.');
    assert.equal(temporalText(sanitize({ ...timed, observedAt: '' }), { mode: 'page' }, now),
      'Current state, updated 30 s ago, from sources looking back 2 min and 10 min. Original observation time unknown.');
  });

  it('keeps stored, frozen and sampled views apart', () => {
    assert.match(temporalText(sanitize({ ...timed, timeMode: 'historical', history: { availability: 'stored-snapshot', id: 'h1', recordedAt: '2026-10-18T12:00:05Z' } }), {}, now),
      /^Historical snapshot of 2026-10-18T12:00:00.000Z, recorded 2026-10-18T12:00:05Z: one observation, not continuous coverage. Live updates are paused.$/);
    assert.match(temporalText(timed, { comparison: true }, now), /^Frozen capture of 2026-10-18T12:00:00.000Z \(current\): one observation, not continuous coverage.$/);
    for (const opts of [{ mode: 'panel' }, { comparison: true }, {}]) assert.doesNotMatch(temporalText(timed, opts, now), / · /);
  });
});

// A round that adds or drops a call must not rearrange the map someone is
// reading; with the plain layered layout one call moved most of the cards.
describe('steadyLayout', () => {
  const ids = ['web', 'api', 'auth', 'orders', 'db', 'cache', 'queue', 'mail', 'pay', 'gw'];
  const calls = [['web', 'api'], ['api', 'auth'], ['api', 'orders'], ['orders', 'db'], ['orders', 'cache'], ['orders', 'queue'], ['queue', 'mail'], ['orders', 'pay'], ['pay', 'gw']];
  const v = (nodes, edges) => view(snap(nodes.map(id => node(id)), edges.map(([a, b]) => edge(a, b))));
  const before = layered(v(ids, calls));
  const same = (a, b) => Object.keys(b).filter(id => a[id]).every(id => a[id].x === b[id].x && a[id].y === b[id].y);
  const apart = pos => Object.values(pos).every((p, i, all) => all.every((q, j) => i === j || p.x !== q.x || Math.abs(p.y - q.y) >= 112 - 1e-9));

  it('moves nothing when a call appears or disappears', () => {
    const more = v(ids, [...calls, ['mail', 'gw']]), fewer = v(ids, calls.filter(([a, b]) => a !== 'orders' || b !== 'queue'));
    assert.ok(same(before, steadyLayout(before, more)));
    assert.ok(same(before, steadyLayout(before, fewer)));
    // the plain layout moves them
    assert.ok(!same(before, layered(more)) && !same(before, layered(fewer)));
  });

  it('moves nothing when a component goes away', () => {
    const pos = steadyLayout(before, v(ids.filter(id => id !== 'cache'), calls.filter(c => !c.includes('cache'))));
    assert.ok(same(before, pos));
    assert.equal(pos.cache, undefined);
  });

  it('gives a new component a free row next to its caller, without moving the others', () => {
    const pos = steadyLayout(before, v([...ids, 'audit'], [...calls, ['orders', 'audit']]));
    assert.ok(same(before, pos));
    assert.equal(pos.audit.x, before.orders.x + 300);
    assert.ok(apart(pos), 'no two cards share a slot');
  });

  it('lays out afresh with no layout yet, after a large change, or in a framed scope', () => {
    const grown = v([...ids, 'a1', 'a2', 'a3', 'a4'], [...calls, ...['a1', 'a2', 'a3', 'a4'].map(id => ['web', id])]);
    assert.deepEqual(steadyLayout(null, grown), layered(grown));
    assert.deepEqual(steadyLayout(before, grown), layered(grown));
    const pin = new Map([['db', 'last']]);
    const one = v([...ids, 'audit'], [...calls, ['orders', 'audit']]);
    assert.deepEqual(steadyLayout(before, one, { pin }), layered(one, { pin }));
  });

  it('starts from a shared seed when it covers every card', () => {
    const seed = { ...before, extra: { x: 900, y: 0 } };
    assert.deepEqual(steadyLayout(null, v(ids, calls), { seed }), Object.fromEntries(ids.map(id => [id, before[id]])));
    assert.deepEqual(steadyLayout(null, v([...ids, 'other'], calls), { seed }), layered(v([...ids, 'other'], calls)));
  });
});

describe('particleBudget', () => {
  it('gives busier edges more dots within the budget', () => {
    const edges = [{ id: 'a', observed: true, requestRate: 100 }, { id: 'b', observed: true, requestRate: 0.1 }, { id: 'c', observed: false, requestRate: 0 }];
    const b = particleBudget(edges, 20);
    assert.ok(b.get('a') > b.get('b'));
    assert.equal(b.has('c'), false);
  });

  it('never goes far past the budget, however many edges there are', () => {
    const edges = Array.from({ length: 2000 }, (_, i) => ({ id: 'e' + i, observed: true, requestRate: 1 }));
    const total = [...particleBudget(edges, 360).values()].reduce((a, b) => a + b, 0);
    assert.ok(total <= 400, `total ${total}`);
  });
});

describe('safeApiBase', () => {
  it('accepts http and https and adds the trailing slash', () => {
    assert.equal(safeApiBase('http://teia:8080/api/v1', 'http://grafana/'), 'http://teia:8080/api/v1/');
    assert.equal(safeApiBase('api/v1/', 'https://teia.example/'), 'https://teia.example/api/v1/');
  });
  it('refuses other schemes and garbage', () => {
    for (const bad of ['javascript:alert(1)//', 'data:text/html,x', 'file:///etc/passwd']) assert.equal(safeApiBase(bad, 'http://x/'), null);
    assert.equal(safeApiBase('', 'http://x/'), null);
    assert.equal(safeApiBase('http://[bad', undefined), null);
  });
});

describe('categoryOf', () => {
  it('uses the technology first, then the kind', () => {
    assert.equal(categoryOf({ kind: 'database', tech: 'mongodb' }), 'document');
    assert.equal(categoryOf({ kind: 'database' }), 'relational');
    assert.equal(categoryOf({ kind: 'toString' }), 'unknown');
    assert.equal(categoryOf({ group: true }), 'group');
  });
});

describe('latency', () => {
  it('keeps each sampled edge timing through grouping instead of inventing group freshness', () => {
    const sample = (count, from, to) => ({ source: 'tempo', signal: 'traces', sampleCount: count,
      collectedAt: '2026-10-15T12:00:00Z', observedFrom: from, observedAt: to, metrics: ['topology', 'latency'] });
    const s = snap([node('web', { labels: { team: 'front' } }), ...['api', 'worker'].map(id => node(id, { labels: { team: 'core' } }))], [
      edge('web', 'api', { evidence: [sample(2, '2026-10-15T10:00:00Z', '2026-10-15T10:01:00Z')] }),
      edge('web', 'worker', { evidence: [sample(3, undefined, '2026-10-15T11:59:00Z')] }),
    ]);
    const entries = view(s, { groupBy: 'team' }).edges.get('group:front->group:core').evidence;
    assert.equal(entries.length, 2);
    assert.deepEqual(entries.map(e => e.sampleCount), [2, 3]);
    assert.equal(entries[0].observedFrom, '2026-10-15T10:00:00.000Z');
    assert.equal(entries[0].observedAt, '2026-10-15T10:01:00.000Z');
    assert.equal(entries[1].observedAt, '', 'a missing bound must not claim the whole sample is recent');
    assert.equal(s.observedAt, '', 'per-edge times do not establish a graph-wide timestamp');
  });

  it('qualifies weighted percentiles and ignores missing p50 and zero-rate members', () => {
    const nodes = [node('a', { labels: { team: 'x' } }), ...['b', 'c', 'd', 'e'].map(id => node(id, { labels: { team: 'y' } }))];
    const s = snap(nodes, [
      edge('a', 'b', { requestRate: 100, latencyP50Ms: 10, latencyP95Ms: 1000 }),
      edge('a', 'c', { requestRate: 300, latencyP95Ms: 3000 }),
      edge('a', 'd', { requestRate: 600 }),
      edge('a', 'e', { requestRate: 0, latencyP95Ms: 999999 }),
    ]);
    for (const aggregate of [view(s, { groupBy: 'team' }).edges.get('group:x->group:y'), scopeStats(s, { roles: new Map(nodes.map(n => [n.id, 'member'])) })]) {
      assert.equal(aggregate.latencyP95Ms, 2500);
      assert.equal(aggregate.latencyP50Ms, 10);
      assert.equal(aggregate.latencyApproximate, true);
      assert.equal(aggregate.latencyCoverage, 0.4);
    }
    assert.equal(view(s).edges.get('a->e').latencyP95Ms, 999999, 'a single sampled edge retains its observation without a rate');
    assert.match(reasonTag({ cause: 'latency', value: 2500, approximate: true }), /≈ p95/);
  });
  it('survives sanitising, and junk becomes "not measured"', () => {
    const s = sanitize({ nodes: [{ id: 'a', latencyP95Ms: 'slow' }], edges: [{ from: 'a', to: 'b', latencyP50Ms: 12, latencyP95Ms: NaN }] });
    assert.equal(s.nodes[0].latencyP95Ms, 0);
    assert.equal(s.edges[0].latencyP50Ms, 12);
    assert.equal(s.edges[0].latencyP95Ms, 0);
  });

  it('folds into groups weighted by traffic, ignoring members that measured none', () => {
    const s = snap(
      [node('web', { labels: { team: 'front' } }), node('api', { labels: { team: 'core' } }), node('worker', { labels: { team: 'core' } })],
      [
        edge('web', 'api', { requestRate: 3, latencyP50Ms: 40, latencyP95Ms: 100 }),
        edge('web', 'worker', { requestRate: 1, latencyP50Ms: 80, latencyP95Ms: 300 }),
        edge('api', 'worker', { requestRate: 5 }),
      ],
    );
    const e = view(s, { groupBy: 'team' }).edges.get('group:front->group:core');
    assert.equal(e.latencyP95Ms, 150);
    assert.equal(e.latencyP50Ms, 50);
    assert.equal(view(s).edges.get('api->worker').latencyP95Ms, 0);
  });

  it('is written the way people read it', () => {
    assert.deepEqual([0.42, 9.96, 12.4, 999.7, 1234].map(fmtMs), ['0.4 ms', '10 ms', '12 ms', '1.0 s', '1.2 s']);
  });
});

describe('unmeasured failures', () => {
  it('preserves the measured subset in both grouped edges and scope summaries', () => {
    const nodes = [node('a', { labels: { team: 'x' } }), node('b', { labels: { team: 'y' } }), node('c', { labels: { team: 'y' } })];
    const edges = [edge('a', 'b', { requestRate: 100, errorRate: 0.5 }), edge('a', 'c', { requestRate: 900, errorsUnknown: true })];
    const s = snap(nodes, edges), scope = { roles: new Map(nodes.map(n => [n.id, 'member'])) };
    for (const aggregate of [view(s, { groupBy: 'team' }).edges.get('group:x->group:y'), scopeStats(s, scope)]) {
      assert.equal(aggregate.errorRate, 0.5);
      assert.equal(aggregate.errorCoverage, 0.1);
      assert.equal(aggregate.errorsUnknown, false);
    }
    const unknown = snap(nodes, edges.map(e => ({ ...e, errorsUnknown: true })));
    assert.equal(scopeStats(unknown, scope).errorsUnknown, true);
    assert.equal(scopeStats(unknown, scope).errorCoverage, 0);
    // A zero-rate reading must not make an otherwise unmeasured group known.
    const zero = snap(nodes, [edges[1], edge('a', 'b', { requestRate: 0 })]);
    assert.equal(view(zero, { groupBy: 'team' }).edges.get('group:x->group:y').errorsUnknown, true);
  });

  it('carries partial coverage through another aggregation and into card text', () => {
    const nodes = [node('a'), node('b'), node('c')];
    const s = snap(nodes, [edge('a', 'b', { requestRate: 1000, errorRate: 0.5, errorCoverage: 0.1 }), edge('a', 'c', { requestRate: 100, errorRate: 0 })]);
    const stats = scopeStats(s, { roles: new Map(nodes.map(n => [n.id, 'member'])) });
    assert.equal(stats.errorRate, 0.25);
    assert.equal(stats.errorCoverage, 200 / 1100);
    assert.match(cardSubtitle({ kind: 'service', requestRate: 1000, errorRate: 0.5, errorCoverage: 0.1 }), /50% err \(10% covered\)/);
  });
  it('survive sanitising and say so on the card', () => {
    const s = sanitize({ nodes: [{ id: 'app-b', kind: 'service', requestRate: 10, errorsUnknown: true }], edges: [{ from: 'app-a', to: 'app-b', requestRate: 10, errorsUnknown: true }] });
    assert.equal(s.nodes.find(n => n.id === 'app-b').errorsUnknown, true);
    assert.equal(s.edges[0].errorsUnknown, true);
    assert.equal(cardSubtitle({ kind: 'service', requestRate: 10, errorRate: 0, errorsUnknown: true, latencyP95Ms: 0 }), 'service · err ? · 10.0/s');
  });
  it('make a grouped line unknown only when every member is', () => {
    const s = snap([node('a', { labels: { team: 'x' } }), node('b', { labels: { team: 'y' } }), node('c', { labels: { team: 'y' } })],
      [edge('a', 'b', { errorsUnknown: true }), edge('a', 'c')]);
    assert.equal(view(s, { groupBy: 'team' }).edges.get('group:x->group:y').errorsUnknown, false);
  });
});

describe('cardSubtitle', () => {
  it('shows what it is and its golden signals', () => {
    assert.equal(cardSubtitle({ kind: 'database', tech: 'postgresql', requestRate: 2, latencyP95Ms: 12, errorRate: 0 }), 'PostgreSQL · 2.00/s · 12 ms');
  });
  it('drops what it is before the signals when they do not fit', () => {
    assert.equal(cardSubtitle({ kind: 'service', requestRate: 2, latencyP95Ms: 120, errorRate: 0.051 }), '2.00/s · 120 ms · 5.1% err');
  });
  it('says what it is when there is no traffic', () => {
    assert.equal(cardSubtitle({ kind: 'service', tech: 'job', requestRate: 0, latencyP95Ms: 0, errorRate: 0 }), 'Job');
  });
});

describe('reasons', () => {
  const slow = { cause: 'latency', health: 'degraded', value: 950, limit: 500, summary: 'p95 950 ms, over 500 ms, from inventory', via: 'inventory' };
  const failing = { cause: 'errors', health: 'failing', value: 0.07, limit: 0.05, summary: '7.0% errors, over 5.0%' };

  it('keeps what the map can draw, worst first', () => {
    const s = sanitize({ nodes: [{ id: 'a', reasons: [slow, failing, { cause: 'gremlins', health: 'failing' }, { cause: 'errors', health: 'healthy' }, null] }] });
    assert.deepEqual(s.nodes[0].reasons.map(r => r.cause), ['errors', 'latency']);
    assert.equal(sanitize({ nodes: [{ id: 'b', reasons: 'nope' }] }).nodes[0].reasons.length, 0);
  });

  it('says what crossed its limit, and where it comes from', () => {
    assert.equal(reasonTag(slow), 'p95 950 ms from inventory');
    assert.equal(reasonTag(failing, 1), '7.0% errors +1');
    assert.equal(reasonTag({ cause: 'backlog', value: 45210 }), '45k waiting');
    assert.equal(reasonTag({ cause: 'saturation', value: 0.93 }), '93% used');
    assert.equal(reasonTag({ cause: 'saturation', value: 0.95, what: 'connections' }), '95% connections');
    assert.equal(reasonTag({ cause: 'locks', value: 3, via: 'legacy-reports' }), '3 on locks held by legacy-reports');
    assert.equal(reasonTag({ cause: 'locks', value: 3, subject: 'orders' }), '3 on locks · orders');
  });

  it('says how far behind a queue is and how often a workload restarted', () => {
    assert.equal(reasonTag({ cause: 'lag', value: 720, what: 'behind', via: '' }), '12 min behind');
    assert.equal(reasonTag({ cause: 'lag', value: 300, what: 'without progress' }), '5 min without progress');
    assert.equal(reasonTag({ cause: 'restarts', value: 3 }), '3 restarts');
  });

  it('writes durations and sizes the way people read them', () => {
    assert.deepEqual([45, 720, 9000].map(fmtSec), ['45 s', '12 min', '2.5 h']);
    assert.deepEqual([512, 48 * 1024 * 1024, 1.5 * 1024 ** 3].map(fmtBytes), ['512 B', '48.0 MiB', '1.5 GiB']);
  });

  it('writes big numbers short', () => {
    assert.deepEqual([7, 1234, 45210, 1_250_000, 12_000_000].map(compact), ['7', '1.2k', '45k', '1.3M', '12M']);
  });

  it('gives a group the reasons of its members, named after them', () => {
    const s = snap(
      [node('api', { labels: { team: 'core' }, health: 'degraded', reasons: [slow] }), node('db', { labels: { team: 'core' } }), node('web', { labels: { team: 'front' } })],
      [edge('web', 'api', { health: 'failing', reasons: [failing] }), edge('api', 'db')],
    );
    const v = view(s, { groupBy: 'team' });
    const g = v.nodes.get('group:core');
    assert.deepEqual(g.reasons.map(r => [r.cause, r.subject]), [['latency', 'api']]);
    assert.deepEqual(v.edges.get('group:front->group:core').reasons.map(r => [r.cause, r.subject]), [['errors', 'web→api']]);
  });

  it('keeps readings beyond traffic, dropping what it cannot name', () => {
    const s = sanitize({ nodes: [{ id: 'db', measures: [{ cause: 'saturation', what: 'connections', value: 12, max: 97 }, { cause: 'saturation', value: 3 }, { cause: 'gremlins', what: 'x' }] }] });
    assert.deepEqual(s.nodes[0].measures, [{ cause: 'saturation', what: 'connections', value: 12, max: 97, unit: '', via: '' }]);
  });

  it('can be searched with why:', () => {
    const v = view(snap([node('api', { reasons: [slow] }), node('db')], []));
    assert.deepEqual([...v.nodes.values()].filter(matcher('why:latency')).map(n => n.id), ['api']);
  });
});

describe('placeTag', () => {
  const card = { x0: 0, y0: 0, x1: 100, y1: 60 };
  it('takes the middle when it is free', () => {
    const at = placeTag([{ x: 200, y: 30 }, { x: 300, y: 30 }], 80, 18, [card]);
    assert.deepEqual([at.x, at.y], [200, 30]);
  });
  it('moves along the line past a card or a tag', () => {
    const at = placeTag([{ x: 50, y: 30 }, { x: 140, y: 30 }, { x: 260, y: 30 }], 80, 18, [card, { x0: 100, y0: 20, x1: 180, y1: 40 }]);
    assert.deepEqual([at.x, at.y], [260, 30]);
  });
  it('falls back to the middle when there is no room anywhere', () => {
    const at = placeTag([{ x: 50, y: 30 }], 80, 18, [card]);
    assert.deepEqual([at.x, at.y, at.box.x0], [50, 30, 10]);
  });
  it('covers as little as it can when every spot is taken', () => {
    const at = placeTag([{ x: 50, y: 30 }, { x: 130, y: 30 }], 80, 18, [card]);
    assert.deepEqual([at.x, at.y], [130, 30]);
  });
});

describe('view edges', () => {
  it('carry the measures of the communications they draw', () => {
    const s = snap([node('worker'), node('topic')], [edge('worker', 'topic', { measures: [{ cause: 'backlog', what: 'messages waiting', value: 1500 }] })]);
    assert.equal(view(s).edges.get('worker->topic').measures[0].value, 1500);
  });
});

describe('journeys', () => {
  // The lab's shop, with its journey labels.
  const j = v => ({ labels: { journey: v } });
  const shop = snap([
    node('web', j('checkout,browse')), node('orders', j('checkout')), node('payments', j('checkout')), node('email-worker', j('checkout')),
    node('orders-db', { kind: 'database', ...j('checkout') }), node('payment-gateway', { kind: 'external' }), node('order-events', { kind: 'queue' }),
    node('email-provider', { kind: 'external' }), node('catalog', j('browse')), node('recommendations', j('browse')), node('inventory', j('browse')),
    node('catalog-cache', { kind: 'cache', ...j('browse') }), node('catalog-db', { kind: 'database' }),
    node('legacy-reports', j('reporting')), node('partner-sync', j('partner-feed')),
  ], [
    edge('web', 'orders'), edge('web', 'catalog'), edge('web', 'recommendations'), edge('orders', 'payments'), edge('orders', 'orders-db'),
    edge('orders', 'order-events'), edge('payments', 'payment-gateway'), edge('email-worker', 'order-events'), edge('email-worker', 'email-provider'),
    edge('catalog', 'catalog-cache'), edge('catalog', 'catalog-db'), edge('recommendations', 'inventory'), edge('legacy-reports', 'orders-db'),
    edge('partner-sync', 'catalog'),
  ]);
  const ids = set => [...set].sort();

  it('reads a journey, any label, or what a node depends on', () => {
    assert.deepEqual(parseJourney('checkout'), { text: 'checkout', key: 'journey', value: 'checkout' });
    assert.deepEqual(parseJourney('team=payments'), { text: 'team=payments', key: 'team', value: 'payments' });
    assert.deepEqual(parseJourney(' team: payments '), { text: 'team: payments', key: 'team', value: 'payments' });
    assert.deepEqual(parseJourney('from:orders'), { text: 'from:orders', entry: 'orders' });
    assert.equal(parseJourney('  '), null);
  });

  it('takes its members and what they call, and stops at datastores and at other journeys', () => {
    const c = journeyOf(shop, 'checkout');
    assert.deepEqual(ids(c.roles.keys()), ['email-provider', 'email-worker', 'order-events', 'orders', 'orders-db', 'payment-gateway', 'payments', 'web']);
    assert.equal(c.roles.get('orders-db'), 'member');
    assert.equal(c.roles.get('payment-gateway'), 'dependency');
  });

  it('draws only what is inside its frame: outsiders become ports', () => {
    const v = scopeSnapshot(shop, journeyOf(shop, 'checkout'));
    assert.deepEqual(v.nodes.map(n => n.id).sort(), ['email-provider', 'email-worker', 'order-events', 'orders', 'orders-db', 'payment-gateway', 'payments', 'web']);
    assert.ok(!v.edges.some(e => e.from === 'legacy-reports' || e.to === 'catalog'), 'no line leaves the frame');
  });

  it('shares a datastore without swallowing its other clients', () => {
    const r = journeyOf(shop, 'reporting');
    assert.deepEqual(ids(r.roles.keys()), ['legacy-reports', 'orders-db']);
  });

  it('follows from a node with no ownership boundary', () => {
    const f = journeyOf(shop, 'from:orders');
    assert.deepEqual(ids(f.roles.keys()), ['order-events', 'orders', 'orders-db', 'payment-gateway', 'payments']);
  });

  it('ends on cycles, cuts unlabelled chains at maxDepth, and caps its size', () => {
    const chain = snap([node('m', j('x')), node('s1'), node('s2'), node('s3'), node('s4')],
      [edge('m', 's1'), edge('s1', 's2'), edge('s2', 's3'), edge('s3', 's4'), edge('s2', 'm')]);
    assert.deepEqual(ids(journeyOf(chain, 'x').roles.keys()), ['m', 's1', 's2']);
    const capped = journeyOf(chain, 'x', { maxNodes: 2 });
    assert.equal(capped.truncated, true);
    assert.equal(capped.roles.size, 2);
  });

  it('heads with the origin of its worst health', () => {
    const fail = { cause: 'errors', health: 'failing', value: 0.07, limit: 0.05, summary: '7.0% errors, over 5.0%' };
    const s = snap([
      node('web', { ...j('checkout'), health: 'failing', reasons: [{ ...fail, via: 'orders' }] }),
      node('orders', { ...j('checkout'), health: 'failing', reasons: [{ ...fail, via: 'payments' }] }),
      node('payments', { ...j('checkout'), health: 'failing', reasons: [fail] }),
    ], [edge('web', 'orders'), edge('orders', 'payments', { health: 'failing', reasons: [fail] })]);
    const why = journeyWhy(s, journeyOf(s, 'checkout'));
    assert.equal(why.health, 'failing');
    assert.equal(why.subject, 'orders->payments');
    assert.equal(why.more, 1); // payments itself
    const entry = scopeStats(s, journeyOf(s, 'checkout'));
    assert.equal(entry.errorRate, 0, 'successful entry calls coexist with a failing downstream dependency');
    assert.equal(entry.requestRate, 1, 'internal downstream calls are not additional entries');
  });

  it('counts entry fan-out as calls rather than deduplicated transactions', () => {
    const s = snap([node('web', j('shop')), node('api', j('shop')), node('worker', j('shop'))], [
      edge('web', 'api', { requestRate: 10 }), edge('web', 'worker', { requestRate: 10 }),
      edge('api', 'worker', { requestRate: 200, errorRate: 0.5 }),
    ]);
    const summary = scopeStats(s, journeyOf(s, 'shop'));
    assert.equal(summary.requestRate, 20);
    assert.equal(summary.errorRate, 0);
  });

  it('is picked on its own bar, never offered as a grouping (a service lists several)', () => {
    assert.equal(labelKeys(shop, 'journey').includes('journey'), false);
  });

  it('finds the journey label a platform already has', () => {
    const partOf = snap([node('web', { labels: { 'app.kubernetes.io/part-of': 'shop' } }), node('erp', { labels: { system: 'finance' } })], []);
    assert.equal(journeyKeyOf(partOf), 'app.kubernetes.io/part-of');
    assert.equal(journeyKeyOf(snap([node('erp', { labels: { system: 'finance' } })], [])), 'system');
    assert.equal(journeyKeyOf(shop), 'journey');
    assert.equal(journeyKeyOf(shop, 'team'), 'team');
    assert.deepEqual([...journeyOf(partOf, 'shop', { key: 'app.kubernetes.io/part-of' }).roles.keys()], ['web']);
  });

  it('pins what the outside enters to the start and the resources that call nothing to the end', () => {
    const scope = journeyOf(shop, 'browse');
    const inner = view(sanitize(scopeSnapshot(shop, scope)));
    const pin = scopePins(inner, journeyPorts(shop, scope));
    assert.equal(pin.get('catalog'), undefined, 'web calls catalog inside the frame: not an entry');
    assert.equal(pin.get('catalog-cache'), 'last');
    assert.equal(pin.get('web'), undefined);
    const lone = snap([node('api', j('y')), node('db', { kind: 'database', ...j('y') }), node('client')], [edge('client', 'api'), edge('api', 'db')]);
    const ls = journeyOf(lone, 'y');
    const lp = scopePins(view(sanitize(scopeSnapshot(lone, ls))), journeyPorts(lone, ls));
    assert.deepEqual([lp.get('api'), lp.get('db')], ['first', 'last']);
  });

  it('pins entries to the first column and datastores, queues and third parties to the last', () => {
    const scope = journeyOf(shop, 'checkout');
    const inner = view(sanitize({ nodes: shop.nodes.filter(n => scope.roles.has(n.id)), edges: shop.edges.filter(e => scope.roles.has(e.from) && scope.roles.has(e.to)) }));
    const pin = new Map([['web', 'first'], ...['payment-gateway', 'orders-db', 'order-events', 'email-provider'].map(id => [id, 'last'])]);
    const pos = layered(inner, { pin });
    const xs = new Set(['payment-gateway', 'orders-db', 'order-events', 'email-provider'].map(id => pos[id].x));
    assert.equal(xs.size, 1, 'one lane for every resource');
    const last = [...xs][0];
    for (const id of ['web', 'orders', 'payments', 'email-worker']) assert.ok(pos[id].x < last, `${id} before the lane`);
    assert.equal(pos.web.x, 0);
  });

  it('turns outsiders into ports: callers left, exits folded by journey, tenants beside their datastore', () => {
    const ports = journeyPorts(shop, journeyOf(shop, 'checkout'));
    const exit = ports.find(p => p.kind === 'exit');
    assert.deepEqual([exit.side, exit.label, exit.of.sort(), exit.anchors], ['out', '↗ browse · 2', ['catalog', 'recommendations'], ['web']]);
    const tenant = ports.find(p => p.kind === 'tenant');
    assert.deepEqual([tenant.side, tenant.of, tenant.anchors], ['out', ['legacy-reports'], ['orders-db']]);
    const browse = journeyPorts(shop, journeyOf(shop, 'browse'));
    assert.ok(browse.some(p => p.kind === 'caller' && p.side === 'in' && p.of[0] === 'partner-sync'));
  });

  it('keeps the busiest ports on a side and folds the rest', () => {
    const many = snap([node('db', { kind: 'database', labels: { journey: 'x' } }), ...Array.from({ length: 12 }, (_, i) => node('c' + i))],
      Array.from({ length: 12 }, (_, i) => edge('c' + i, 'db', { requestRate: i + 1 })));
    const ports = journeyPorts(many, journeyOf(many, 'x'), { maxPerSide: 5 });
    assert.equal(ports.length, 5);
    const more = ports.find(p => p.kind === 'more');
    assert.deepEqual([more.label, more.of.length, more.requestRate], ['+8 more', 8, 36]);
  });

  it('stacks chips in order, without overlap, where they want to be when they can', () => {
    assert.deepEqual(packColumn([{ desired: 0, size: 20 }, { desired: 100, size: 20 }], 8), [0, 100]);
    const ys = packColumn([{ desired: 0, size: 20 }, { desired: 5, size: 20 }, { desired: 10, size: 20 }], 8);
    for (let i = 1; i < ys.length; i++) assert.ok(ys[i] - ys[i - 1] >= 28 - 1e-9, `gap ${ys[i] - ys[i - 1]}`);
    assert.ok(Math.abs((ys[0] + ys[1] + ys[2]) / 3 - 5) < 1e-9, 'the run centres on what it wants');
  });

  it('frames the cards, puts every chip outside the frame and routes around cards', () => {
    const pos = { a: { x: 0, y: 0 }, b: { x: 300, y: 0 }, c: { x: 300, y: 112 } };
    const ports = [
      { id: 'in', side: 'in', kind: 'caller', label: 'caller', anchors: ['b'], of: ['x'] },
      { id: 'out', side: 'out', kind: 'exit', label: '↗ other', anchors: ['a'], of: ['y'] },
    ];
    const f = frameLayout(pos, ports, { lane: ['c'] });
    for (const id of Object.keys(pos)) assert.ok(pos[id].x - 98 > f.frame.x0 && pos[id].x + 98 < f.frame.x1, `${id} inside`);
    const inChip = f.chips.find(c => c.port.id === 'in'), outChip = f.chips.find(c => c.port.id === 'out');
    assert.ok(inChip.x + inChip.w / 2 < f.frame.x0 && outChip.x - outChip.w / 2 > f.frame.x1, 'chips outside the frame');
    assert.equal(inChip.routes[0].mode, 'top', 'a caller of b would cross a: it takes a corridor');
    assert.equal(outChip.routes[0].mode, 'top', 'an exit from a would cross b');
    assert.ok(f.band && f.band.x0 < 300 && f.band.x1 > 300, 'a lane band behind c');
  });

  it('says what enters the scope: from callers outside and where it starts, not from tenants', () => {
    const rated = snap(shop.nodes, shop.edges.map(e => ({ ...e, requestRate: 1 })));
    assert.equal(scopeStats(rated, journeyOf(rated, 'reporting')).parts, 2);
    // browse: web starts it (2 calls inside), partner-sync calls in (1)
    assert.equal(scopeStats(rated, journeyOf(rated, 'browse')).rate, 3);
    // checkout: web and email-worker start it; legacy-reports only shares orders-db
    assert.equal(scopeStats(rated, journeyOf(rated, 'checkout')).rate, 3);
  });

  it('lists the journeys the label names', () => {
    assert.deepEqual(journeyNames(shop), ['browse', 'checkout', 'partner-feed', 'reporting']);
  });

  it('searches an exact label value with key=value', () => {
    const vv = view(shop);
    const hits = [...vv.nodes.values()].filter(matcher('journey=checkout')).map(n => n.id);
    assert.ok(hits.includes('web') && hits.includes('orders') && !hits.includes('catalog'));
  });
});

describe('filters', () => {
  const lab = snap([
    node('web', { labels: { journey: 'checkout,browse', 'k8s.namespace': 'shop', team: 'storefront' } }),
    node('orders', { labels: { journey: 'checkout', 'k8s.namespace': 'shop', team: 'checkout' }, health: 'degraded' }),
    node('payments', { labels: { journey: 'checkout', 'k8s.namespace': 'pay', team: 'checkout' }, health: 'failing' }),
    node('orders-db', { kind: 'database', tech: 'postgresql', labels: { 'k8s.namespace': 'shop' } }),
    node('catalog', { labels: { journey: 'browse', 'k8s.namespace': 'shop', team: 'catalog' } }),
    node('catalog-cache', { kind: 'cache', tech: 'redis', labels: { journey: 'browse', team: 'catalog' } }),
  ], [edge('web', 'orders'), edge('web', 'catalog'), edge('orders', 'payments'), edge('orders', 'orders-db'), edge('catalog', 'catalog-cache')]);
  const kept = scope => [...scope.roles.keys()].sort();

  it('keeps the whole map without filters', () => {
    assert.equal(scopeOf(lab, { journeys: [], facets: {} }), null);
    assert.equal(scopeOf(lab, { journeys: [], facets: { team: [] } }), null);
  });

  it('keeps the nodes holding one of a facet\'s values, and narrows across facets', () => {
    assert.deepEqual(kept(scopeOf(lab, { journeys: [], facets: { 'k8s.namespace': ['shop'] } })), ['catalog', 'orders', 'orders-db', 'web']);
    assert.deepEqual(kept(scopeOf(lab, { journeys: [], facets: { team: ['checkout', 'catalog'] } })), ['catalog', 'catalog-cache', 'orders', 'payments']);
    assert.deepEqual(kept(scopeOf(lab, { journeys: [], facets: { team: ['checkout'], 'k8s.namespace': ['shop'] } })), ['orders']);
    assert.deepEqual(kept(scopeOf(lab, { journeys: [], facets: { health: ['failing', 'degraded'] } })), ['orders', 'payments']);
    assert.deepEqual(kept(scopeOf(lab, { journeys: [], facets: { tech: ['postgresql'] } })), ['orders-db']);
    assert.deepEqual(kept(scopeOf(lab, { journeys: [], facets: { category: ['cache'] } })), ['catalog-cache']);
  });

  it('adds journeys up, then narrows them with the other facets', () => {
    assert.deepEqual(kept(scopeOf(lab, { journeys: ['checkout', 'browse'], facets: {} })), ['catalog', 'catalog-cache', 'orders', 'orders-db', 'payments', 'web']);
    assert.deepEqual(kept(scopeOf(lab, { journeys: ['checkout'], facets: { 'k8s.namespace': ['shop'] } })), ['orders', 'orders-db', 'web']);
    assert.equal(scopeOf(lab, { journeys: ['nope'], facets: {} }).empty, true);
  });

  it('lists every facet with counts and the worst health of each value', () => {
    const fs = facetsOf(lab);
    assert.deepEqual(fs.map(f => f.key), ['journey', 'k8s.namespace', 'team', 'health', 'visibility', 'category', 'tech']);
    const journeys = fs.find(f => f.key === 'journey').values;
    assert.deepEqual(journeys.map(v => [v.value, v.health]), [['browse', 'healthy'], ['checkout', 'failing']]);
    const ns = fs.find(f => f.key === 'k8s.namespace').values;
    assert.deepEqual(ns.map(v => [v.value, v.count, v.health]), [['pay', 1, 'failing'], ['shop', 4, 'degraded']]);
    assert.equal(fs.find(f => f.key === 'category').values.find(v => v.value === 'relational').label, 'Relational database');
  });

  it('round-trips through a shareable link', () => {
    const f = filtersFromParams(new URLSearchParams('journey=checkout,browse&filter=k8s.namespace:shop,health:failing,health:degraded,journey:reporting'));
    assert.deepEqual(f, { journeys: ['checkout', 'browse', 'reporting'], facets: { 'k8s.namespace': ['shop'], health: ['failing', 'degraded'] } });
    assert.deepEqual(filtersToParams(f), { journey: 'checkout,browse,reporting', filter: 'k8s.namespace:shop,health:failing,health:degraded' });
  });

  it('names each filter in words', () => {
    assert.deepEqual([filterLabel('journey', 'checkout'), filterLabel('journey', 'from:orders'), filterLabel('health', 'failing'), filterLabel('tech', 'redis'), filterLabel('team', 'catalog')],
      ['journey checkout', 'depends on orders', 'failing', 'Redis', 'team catalog']);
  });
});

describe('reason tags', () => {
  it('never calls a method named by the data', () => {
    assert.equal(reasonTag({ cause: 'errors', value: 0.25 }), '25% errors');
    for (const cause of ['constructor', 'toString', '__proto__', 'hasOwnProperty', 'unheard-of']) {
      assert.doesNotThrow(() => reasonTag({ cause, value: 1 }), cause);
      assert.equal(reasonTag({ cause, value: 1, summary: 'too slow' }), 'too slow');
    }
  });
});

describe('clusters', () => {
  it('keeps the cluster a snapshot describes, and never mistakes one cluster for another', () => {
    const nodes = [node('app-a'), node('app-b')], edges = [edge('app-a', 'app-b', { requestRate: 2 })];
    const east = snap(nodes, edges, { cluster: 'east' });
    const west = snap(nodes, edges, { cluster: 'west' });
    assert.equal(east.cluster, 'east');
    assert.equal(snap(nodes, edges).cluster, '', 'a single scope has no cluster');
    assert.equal(snap(nodes, edges, { cluster: 7 }).cluster, '', 'only a string names a cluster');
    assert.notEqual(signature(east), signature(west), 'the same topology in two clusters is two different drawings');
  });
});

describe('dimension slices', () => {
  it('keeps the slice scope and dimension values, and redraws when the slice changes', () => {
    const edges = [edge('app-a', 'app-b', { dimensions: { journey: ['notification', 'whatsapp'] } })];
    const whole = snap([node('app-a'), node('app-b')], edges, { dimensions: { journey: ['notification', 'whatsapp', 7] } });
    assert.deepEqual(whole.dimensions, { journey: ['notification', 'whatsapp'] });
    assert.equal(whole.slice, undefined);
    assert.deepEqual(whole.edges[0].dimensions, { journey: ['notification', 'whatsapp'] });
    const sliced = snap([node('app-a'), node('app-b')], edges, { slice: { selector: { journey: 'notification' }, matched: true, metrics: 'scoped', sources: ['prometheus/calls'], excluded: ['prometheus/mesh'] } });
    assert.deepEqual(sliced.slice, { selector: { journey: 'notification' }, matched: true, metrics: 'scoped', sources: ['prometheus/calls'], excluded: ['prometheus/mesh'] });
    assert.notEqual(signature(whole), signature(sliced));
    assert.equal(snap([], [], { slice: { selector: 'journey:x' } }).slice, undefined);
  });

  it('highlights typed key:value searches on labels', () => {
    const s = snap([node('app-a', { labels: { 'journey.notification': 'true' } }), node('redis', { kind: 'cache' })], []);
    const m = matcher('journey.notification:true');
    assert.deepEqual([...view(s).nodes.values()].filter(m).map(n => n.id), ['app-a']);
  });
});

describe('autoGroupKey', () => {
  const map = (count, labels) => ({ nodes: Array.from({ length: count }, (_, i) => ({ id: 'svc-' + i, labels: labels(i) })) });
  const keys = snap => core.labelKeys(snap);
  it('leaves a map it can draw whole ungrouped', () => {
    const snap = map(core.AUTO_GROUP_NODES, i => ({ 'k8s.namespace': 'ns-' + (i % 20) }));
    assert.equal(core.autoGroupKey(snap, keys(snap)), '');
  });
  it('opens a larger map grouped by namespace, then team', () => {
    const big = map(2000, i => ({ 'k8s.namespace': 'ns-' + (i % 20), team: 't' + (i % 4) }));
    assert.equal(core.autoGroupKey(big, keys(big)), 'k8s.namespace');
    const teams = map(2000, i => ({ team: 't' + (i % 4) }));
    assert.equal(core.autoGroupKey(teams, keys(teams)), 'team');
  });
  it('does not group by a label few components carry, one value, or one per component', () => {
    for (const labels of [i => (i % 10 === 0 ? { 'k8s.namespace': 'ns-' + (i % 30) } : {}), () => ({ 'k8s.namespace': 'shop' }), i => ({ 'k8s.namespace': 'ns-' + i })]) {
      const snap = map(2000, labels);
      assert.equal(core.autoGroupKey(snap, keys(snap)), '');
    }
  });
});
