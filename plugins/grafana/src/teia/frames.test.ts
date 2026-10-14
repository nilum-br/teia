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
  ],
});

describe('framesToSnapshot', () => {
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
    expect(snapshot!.edges[1]).toMatchObject({ latencyP50Ms: 0, latencyP95Ms: 0 });
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
    expect(snapshot!.edges[0].health).toBe('healthy');
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
    expect(snapshot!.nodes[0]).toMatchObject({ health: 'unknown', visibility: 'full', labels: { team: 'x', n: '2' } });
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
});
