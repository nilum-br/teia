import { toDataFrame } from '@grafana/data';
import { coverageOf, framesToSnapshot, roleOf } from './frames';

const nodes = toDataFrame({
  refId: 'nodes',
  fields: [
    { name: 'id', values: ['web', 'orders'] },
    { name: 'kind', values: ['service', 'service'] },
    { name: 'health', values: ['healthy', 'degraded'] },
    { name: 'visibility', values: ['full', 'partial'] },
    { name: 'labels', values: ['{"team":"storefront"}', 'not json'] },
    { name: 'sources', values: ['prometheus,docker', ''] },
  ],
});
const edges = toDataFrame({
  refId: 'edges',
  fields: [
    { name: 'source', values: ['web', 'orders'] },
    { name: 'target', values: ['orders', 'orders-db'] },
    { name: 'state', values: ['ok', 'ghost'] },
    { name: 'requestRate', values: [2, 1.5] },
    { name: 'errorRate', values: [0.02, 0] },
    { name: 'declared', values: [true, false] },
    { name: 'observed', values: [true, true] },
    { name: 'signals', values: ['traces,mesh', 'database'] },
    { name: 'latencyP50Ms', values: [12, null] },
    { name: 'latencyP95Ms', values: [48, 'n/a'] },
    { name: 'reasons', values: ['[{"cause":"latency","health":"degraded","value":900,"limit":500,"summary":"p95 900 ms","via":"payments"}]', 'not json'] },
  ],
});

describe('framesToSnapshot', () => {
  it('rejects a named query whose columns cannot identify its topology', () => {
    expect(framesToSnapshot([toDataFrame({ refId: 'nodes', fields: [{ name: 'service', values: ['a'] }] })], 1).snapshot).toBeUndefined();
    expect(framesToSnapshot([nodes, toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['a'] }] })], 1).snapshot).toBeUndefined();
    // A valid schema with no rows is a real empty result, not a mapping error.
    expect(framesToSnapshot([toDataFrame({ refId: 'nodes', fields: [{ name: 'id', values: [] }] })], 1).snapshot?.nodes).toEqual([]);
  });

  it('keeps a stored snapshot historical and marks mixed current/history frames explicitly', () => {
    const history = { id: 'stored-one', availability: 'stored-snapshot', recordedAt: '2026-10-15T12:00:00Z', nodes: {}, edges: {} };
    const h = toDataFrame({ refId: 'nodes', fields: [
      { name: 'id', values: ['a', 'b'] }, { name: 'timeMode', values: ['historical', 'historical'] },
      { name: 'historyId', values: ['stored-one', 'stored-one'] },
      { name: 'snapshotAt', values: ['2026-10-15T11:59:00Z', '2026-10-15T11:59:00Z'] }, { name: 'history', values: [JSON.stringify(history), ''] },
    ] });
    expect(framesToSnapshot([h], 1).snapshot).toMatchObject({ timeMode: 'historical', history, generatedAt: '2026-10-15T11:59:00.000Z' });
    const live = toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['a'] }, { name: 'target', values: ['b'] }, { name: 'timeMode', values: ['current'] }] });
    expect(framesToSnapshot([h, live], 2).snapshot).toMatchObject({ timeMode: 'mixed', history: undefined });
    const different = toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['a'] }, { name: 'target', values: ['b'] },
      { name: 'timeMode', values: ['historical'] }, { name: 'historyId', values: ['stored-two'] }] });
    expect(framesToSnapshot([h, different], 3).snapshot).toMatchObject({ timeMode: 'mixed', history: undefined });
    const advice = toDataFrame({ refId: 'findings', fields: [{ name: 'title', values: ['Other snapshot finding'] },
      { name: 'timeMode', values: ['historical'] }, { name: 'historyId', values: ['stored-two'] }] });
    expect(framesToSnapshot([h, advice], 4).snapshot).toMatchObject({ timeMode: 'mixed', history: undefined });
    const emptyCoverage = toDataFrame({ refId: 'coverage', fields: [{ name: 'timeMode', values: ['historical'] },
      { name: 'historyId', values: ['stored-one'] }, { name: 'history', values: [JSON.stringify(history)] }] });
    expect(framesToSnapshot([toDataFrame({ refId: 'nodes', fields: [] }), emptyCoverage], 5).snapshot).toMatchObject({ timeMode: 'historical', history, nodes: [] });
  });
  it('names the cluster of the frames, and never lets one cluster pass for another', () => {
    const east = toDataFrame({ refId: 'nodes', fields: [{ name: 'id', values: ['app-a', 'app-b'] }, { name: 'cluster', values: ['east', 'east'] }] });
    const eastEdges = toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['app-a'] }, { name: 'target', values: ['app-b'] }, { name: 'cluster', values: ['east'] }] });
    expect(framesToSnapshot([east, eastEdges], 1).snapshot?.cluster).toBe('east');
    const westEdges = toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['app-a'] }, { name: 'target', values: ['app-b'] }, { name: 'cluster', values: ['west'] }] });
    expect(framesToSnapshot([east, westEdges], 2).snapshot?.cluster).toBe('several clusters: east, west');
    expect(framesToSnapshot([nodes, edges], 3).snapshot?.cluster).toBeUndefined();
  });
  it('accepts historical context in custom metadata for every role and rejects a mismatched auxiliary query', () => {
    const history = { id: 'stored-one', availability: 'stored-snapshot', recordedAt: '2026-10-15T12:00:00Z', nodes: {}, edges: {} };
    const metadata = { timeMode: 'historical', historyId: history.id, snapshotAt: '2026-10-15T11:59:00Z', history };
    const frames = [
      toDataFrame({ refId: 'nodes', fields: [{ name: 'id', values: ['a'] }] }),
      toDataFrame({ refId: 'findings', fields: [{ name: 'title', values: ['A finding'] }] }),
      toDataFrame({ refId: 'sources', fields: [{ name: 'name', values: ['prometheus'] }] }),
      toDataFrame({ refId: 'coverage', fields: [{ name: 'visibility', values: [1] }] }),
      toDataFrame({ refId: 'changes', fields: [{ name: 'kind', values: ['node-health'] }] }),
    ];
    for (const frame of frames) frame.meta = { custom: { teia: metadata } };
    expect(framesToSnapshot(frames, 1).snapshot).toMatchObject({ timeMode: 'historical', history });
    frames[2].meta = { custom: { teia: { ...metadata, historyId: 'stored-two' } } };
    expect(framesToSnapshot(frames, 2).snapshot).toMatchObject({ timeMode: 'mixed', history: undefined });
  });

  it('maps teia frames and adds nodes only edges mention', () => {
    const { snapshot } = framesToSnapshot([nodes, edges], 3);
    expect(snapshot).toBeDefined();
    expect(snapshot!.round).toBe(3);
    expect(snapshot!.nodes.map((n) => n.id)).toEqual(['web', 'orders', 'orders-db']);
    expect(snapshot!.nodes[0].labels).toEqual({ team: 'storefront' });
    expect(snapshot!.nodes[1].labels).toEqual({});
    expect(snapshot!.edges[0]).toMatchObject({ from: 'web', to: 'orders', state: 'ok', signals: ['traces', 'mesh'], health: 'degraded' });
    expect(snapshot!.coverage.declared).toBe(0.5);
    expect(snapshot!.edges[0]).toMatchObject({ latencyP50Ms: 12, latencyP95Ms: 48 });
    expect(snapshot!.edges[1]).toMatchObject({ latencyP50Ms: 0, latencyP95Ms: 0, reasons: [] });
    expect(snapshot!.edges[0].reasons).toEqual([{ cause: 'latency', health: 'degraded', value: 900, limit: 500, summary: 'p95 900 ms', via: 'payments' }]);
  });

  it('reads plain Node Graph frames, such as Tempo service graphs', () => {
    const plainNodes = toDataFrame({
      fields: [
        { name: 'id', values: ['a', 'b'] },
        { name: 'secondarystat', values: [3, 1] },
        { name: 'arc__failed', values: [0.1, 0] },
      ],
    });
    const plainEdges = toDataFrame({ fields: [{ name: 'id', values: ['a_b'] }, { name: 'source', values: ['a'] }, { name: 'target', values: ['b'] }] });
    expect(roleOf(plainNodes)).toBe('nodes');
    expect(roleOf(plainEdges)).toBe('edges');
    const { snapshot } = framesToSnapshot([plainNodes, plainEdges], 1);
    expect(snapshot!.nodes[0].health).toBe('failing');
    // Without declared flows there is nothing to drift from.
    expect(snapshot!.drift).toBe(false);
    expect(snapshot!.edges[0].state).toBe('ok');
  });

  it('uses secondarystat as the edge rate in plain Node Graph frames', () => {
    const plainEdges = toDataFrame({
      fields: [
        { name: 'source', values: ['a'] },
        { name: 'target', values: ['b'] },
        { name: 'secondarystat', values: [4] },
      ],
    });
    const { snapshot } = framesToSnapshot([plainEdges], 1);
    expect(snapshot!.edges[0].requestRate).toBe(4);
    expect(snapshot!.edges[0]).toMatchObject({ health: 'unknown', errorsUnknown: true, errorCoverage: 0 });
  });

  it('replaces values it does not know with safe ones', () => {
    const odd = toDataFrame({
      refId: 'nodes',
      fields: [
        { name: 'id', values: ['a', 'a', ''] },
        { name: 'health', values: ['on-fire', 'healthy', 'healthy'] },
        { name: 'visibility', values: ['x-ray', 'full', 'full'] },
        { name: 'labels', values: ['{"team":"x","nested":{"a":1},"n":2}', '[1,2]', 'null'] },
      ],
    });
    const oddEdges = toDataFrame({
      refId: 'edges',
      fields: [
        { name: 'source', values: ['a', 'a'] },
        { name: 'target', values: ['b', 'a'] },
        { name: 'state', values: ['OK', 'ok'] },
        { name: 'health', values: ['purple', 'healthy'] },
      ],
    });
    const findings = toDataFrame({
      refId: 'findings',
      fields: [
        { name: 'title', values: ['do this', ''] },
        { name: 'severity', values: ['urgent', 'info'] },
      ],
    });
    const { snapshot } = framesToSnapshot([odd, oddEdges, findings], 1);
    expect(snapshot!.nodes.map((n) => n.id)).toEqual(['a', 'b']);
    expect(snapshot!.nodes[0]).toMatchObject({ health: 'unknown', visibility: 'partial', labels: { team: 'x', n: '2' } });
    expect(snapshot!.edges).toHaveLength(1);
    expect(snapshot!.edges[0]).toMatchObject({ state: 'ok', health: 'unknown' });
    expect(snapshot!.findings).toEqual([expect.objectContaining({ title: 'do this', severity: 'info' })]);
  });

  it('treats a field named __proto__ as data', () => {
    const tricky = toDataFrame({
      refId: 'nodes',
      fields: [
        { name: 'id', values: ['a'] },
        { name: '__proto__', values: ['{"polluted":true}'] },
        { name: 'constructor', values: ['x'] },
      ],
    });
    const { snapshot } = framesToSnapshot([tricky], 1);
    expect(snapshot!.nodes[0].id).toBe('a');
    expect(({} as Record<string, unknown>).polluted).toBeUndefined();
  });

  it('returns nothing without nodes or edges', () => {
    expect(framesToSnapshot([], 1).snapshot).toBeUndefined();
  });

  it('preserves supplied time without substituting conversion time or dashboard range', () => {
    const frame = toDataFrame({ refId: 'nodes', fields: [{ name: 'id', values: ['a'] }] });
    expect(framesToSnapshot([frame], 1).snapshot).toMatchObject({ generatedAt: '', observedAt: '', timeMode: 'query' });
    frame.meta = { custom: { teia: { timeMode: 'current', snapshotAt: '2026-10-15T12:00:00Z' } } };
    expect(framesToSnapshot([frame], 99).snapshot).toMatchObject({ generatedAt: '2026-10-15T12:00:00.000Z', timeMode: 'current' });
    const emptyEdges = toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: [] }, { name: 'target', values: [] }] });
    expect(framesToSnapshot([frame, emptyEdges], 99).snapshot).toMatchObject({ generatedAt: '2026-10-15T12:00:00.000Z', timeMode: 'current' });
    frame.meta = { custom: { teia: { snapshotAt: '2026-10-15T12:00:00Z', observedAt: '2026-10-14T10:00:00Z',
      periodFrom: '2026-10-14T00:00:00Z', periodTo: '2026-10-15T00:00:00Z' } } };
    expect(framesToSnapshot([frame], 100).snapshot).toMatchObject({ timeMode: 'query', observedAt: '2026-10-14T10:00:00.000Z',
      period: { from: '2026-10-14T00:00:00.000Z', to: '2026-10-15T00:00:00.000Z' } });
  });

  it('makes mixed modes and incomplete timestamps explicit', () => {
    const current = toDataFrame({ refId: 'nodes', fields: [{ name: 'id', values: ['a'] },
      { name: 'timeMode', values: ['current'] }, { name: 'snapshotAt', values: ['2026-10-15T12:00:00Z'] }] });
    const unknown = toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['a'] }, { name: 'target', values: ['b'] }] });
    expect(framesToSnapshot([current, unknown], 1).snapshot).toMatchObject({ generatedAt: '', timeMode: 'mixed', period: undefined });
  });

  it('computes coverage when the data has none', () => {
    const c = coverageOf(
      [
        { id: 'a', kind: 'service', visibility: 'full', health: 'healthy', sources: [] },
        { id: 'x', kind: 'external', visibility: 'partial', health: 'unknown', sources: [] },
        { id: 'b', kind: 'service', visibility: 'partial', health: 'unknown', sources: [] },
      ],
      []
    );
    expect(c.visibility).toBe(0.5);
  });

  it('does not infer first-hand visibility or error health from rate alone', () => {
    const rateOnly = toDataFrame({ fields: [{ name: 'id', values: ['a'] }, { name: 'secondarystat', values: [10] }] });
    const { snapshot } = framesToSnapshot([rateOnly], 1);
    expect(snapshot!.nodes[0]).toMatchObject({ health: 'unknown', visibility: 'partial', errorsUnknown: true, errorCoverage: 0 });
    expect(snapshot!.coverage.visibility).toBe(0);
  });

  it.each([
    [undefined, 'missing', true], [null, 'null', true], ['', 'missing', true],
    [NaN, 'invalid', true], [Infinity, 'invalid', true], [-1, 'invalid', true],
    [2, 'invalid', true], ['0 errors', 'invalid', true], [false, 'invalid', true],
    [0, 'measured', false], ['0', 'measured', false],
  ])('keeps error input %p distinct from measured zero', (value, status, unknown) => {
    const frame = toDataFrame({ refId: 'edges', fields: [
      { name: 'source', values: ['a'] }, { name: 'target', values: ['b'] },
      { name: 'requestRate', values: [10] }, { name: 'errorRate', values: [value] },
    ] });
    const e = framesToSnapshot([frame], 1).snapshot!.edges[0];
    expect(e).toMatchObject({ errorsUnknown: unknown, health: unknown ? 'unknown' : 'healthy', metricStatus: { errorRate: status } });
  });

  it('does not hide an invalid canonical metric behind an alias and retains other health reasons', () => {
    const frame = toDataFrame({ refId: 'nodes', fields: [
      { name: 'id', values: ['a'] }, { name: 'requestRate', values: ['10 req/s'] },
      { name: 'secondarystat', values: [10] }, { name: 'errorRate', values: [0] },
      { name: 'errorsUnknown', values: [true] },
      { name: 'reasons', values: ['[{"cause":"backlog","health":"failing","summary":"queue stalled"}]'] },
    ] });
    expect(framesToSnapshot([frame], 1).snapshot!.nodes[0]).toMatchObject({
      requestRate: 0, errorsUnknown: true, health: 'failing', metricStatus: { requestRate: 'invalid' },
    });
  });
});
