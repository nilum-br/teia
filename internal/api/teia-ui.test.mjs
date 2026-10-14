// Tests for the pure part of the map component (TeiaUI.core).
//
//   node --test internal/api/teia-ui.test.mjs
//
// They live next to api.go rather than in ui/, which is embedded and served.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { describe, it } from 'node:test';

const { core } = createRequire(import.meta.url)('./ui/teia-ui.js');
const { sanitize, signature, indexSnapshot, nodeSignals, labelKeys, buildView, matcher, layered, particleBudget, safeApiBase, categoryOf, cardSubtitle, fmtMs } = core;

const node = (id, extra = {}) => ({ id, kind: 'service', health: 'healthy', visibility: 'full', sources: [], ...extra });
const edge = (from, to, extra = {}) => ({ from, to, state: 'ok', health: 'healthy', declared: true, observed: true, requestRate: 1, errorRate: 0, signals: ['traces'], sources: [], ...extra });
const snap = (nodes, edges, extra = {}) => sanitize({ round: 1, generatedAt: '2026-10-14T12:00:00Z', nodes, edges, ...extra });
const view = (s, opts) => buildView(s, indexSnapshot(s), opts);

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

