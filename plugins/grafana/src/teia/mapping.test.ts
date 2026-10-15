import { toDataFrame } from '@grafana/data';
import { framesToSnapshot } from './frames';
import { applyMapping, describeFrames, frameKey, parseMapping } from './mapping';

describe('explicit frame and column mapping', () => {
  it('preserves metadata, canonical evidence and unknown numeric values without mutating input', () => {
    const frame = toDataFrame({ refId: 'A', name: 'traffic', meta: { custom: { teia: { timeMode: 'historical', historyId: 'one',
      history: { id: 'one', availability: 'stored-snapshot', nodes: {}, edges: {} }, snapshotAt: '2026-10-15T12:00:00Z' } } }, fields: [
      { name: 'caller', values: ['web', 'web'] }, { name: 'callee', values: ['orders', 'db'] },
      { name: 'calls', values: [10, 20] }, { name: 'failed', values: [null, '5%'] },
      { name: 'duration', values: ['800 ms', null] }, { name: 'evidence', values: ['[]', '[]'] },
    ] });
    const original = [...frame.fields];
    const mapping = parseMapping(JSON.stringify({ edges: { frame: frameKey(frame), source: 'caller', target: 'callee', requestRate: 'calls', errorRate: 'failed', latencyP95Ms: 'duration' } }));
    const mapped = applyMapping([frame], mapping);
    expect(frame.fields).toEqual(original);
    expect(mapped[0].meta).toBe(frame.meta);
    expect(mapped[0].fields.find(f => f.name === 'evidence')).toBe(original[5]);
    expect(mapped[0].fields.find(f => f.name === 'requestRate')!.values).toBe(original[2].values);
    const snapshot = framesToSnapshot(mapped, 1).snapshot!;
    expect(snapshot.timeMode).toBe('historical');
    expect(snapshot.edges.map(e => e.requestRate)).toEqual([10, 20]);
    expect(snapshot.edges.every(e => e.errorsUnknown && e.health === 'unknown')).toBe(true);
    expect(snapshot.edges[0].metricStatus).toMatchObject({ errorRate: 'null', latencyP95Ms: 'invalid' });
    expect(snapshot.edges[1].metricStatus).toMatchObject({ errorRate: 'invalid', latencyP95Ms: 'null' });
  });

  it('uses frame identity across reordering and keeps auxiliary canonical frames', () => {
    const a = toDataFrame({ refId: 'A', name: 'services', fields: [{ name: 'service', values: ['web'] }] });
    const sources = toDataFrame({ refId: 'sources', fields: [{ name: 'name', values: ['tempo'] }] });
    const mapping = { nodes: { frame: frameKey(a), id: 'service' } };
    expect(frameKey(a)).toBe('ref:A/name:services');
    expect(describeFrames([a])[0]).toEqual({ key: frameKey(a), fields: ['service'] });
    for (const frames of [[a, sources], [sources, a]]) {
      const mapped = applyMapping(frames, mapping);
      expect(mapped).toContain(sources);
      expect(framesToSnapshot(mapped, 1).snapshot!.nodes[0].id).toBe('web');
    }
    expect(frameKey(toDataFrame({ refId: 'A/name:x', fields: [] }))).not.toBe(frameKey(toDataFrame({ refId: 'A', name: 'x', fields: [] })));
  });

  it('retains canonical identity and operations without requiring aliases', () => {
    const identity = { input: 'orders-api', resolvedId: 'orders', method: 'alias', source: 'tempo', sourceType: 'tempo',
      detail: 'Explicit alias', timeBasis: 'unknown' };
    const connectionIdentity = { from: [identity], to: [] };
    const operations = { version: 1, mode: 'operation', sampling: { policy: 'bounded-trace-search', totalRequests: null, coverage: null },
      sources: [], scopes: [], unattributed: [] };
    const nodes = toDataFrame({ refId: 'A', fields: [{ name: 'service', values: ['orders'] },
      { name: 'identity', values: [JSON.stringify([identity])] }, { name: 'operations', values: [JSON.stringify(operations)] }] });
    const edges = toDataFrame({ refId: 'B', fields: [{ name: 'caller', values: ['orders'] }, { name: 'callee', values: ['db'] },
      { name: 'identity', values: [JSON.stringify(connectionIdentity)] }] });
    const mapped = applyMapping([nodes, edges], { nodes: { frame: 'ref:A', id: 'service' }, edges: { frame: 'ref:B', source: 'caller', target: 'callee' } });
    expect(mapped[0].fields.find(f => f.name === 'identity')).toBe(nodes.fields[1]);
    expect(mapped[0].fields.find(f => f.name === 'operations')).toBe(nodes.fields[2]);
    expect(mapped[1].fields.find(f => f.name === 'identity')).toBe(edges.fields[2]);
    const snapshot = framesToSnapshot(mapped, 1).snapshot!;
    expect(snapshot.nodes[0].identity).toEqual([identity]);
    expect(snapshot.edges[0].identity).toEqual(connectionIdentity);
    expect(snapshot.operations).toEqual(operations);
  });

  it('can derive nodes and edges from one frame without losing either role', () => {
    const frame = toDataFrame({ refId: 'A', fields: [{ name: 'caller', values: ['web'] }, { name: 'callee', values: ['db'] }] });
    const mapped = applyMapping([frame], { nodes: { frame: 'ref:A', id: 'caller' }, edges: { frame: 'ref:A', source: 'caller', target: 'callee' } });
    expect(mapped.map(f => f.refId)).toEqual(['nodes', 'edges']);
    expect(framesToSnapshot(mapped, 1).snapshot!.edges[0]).toMatchObject({ from: 'web', to: 'db', errorsUnknown: true });
  });

  it('rejects ambiguous, missing and unnamed frames or duplicate columns instead of guessing', () => {
    const frame = toDataFrame({ refId: 'A', fields: [{ name: 'service', values: ['web'] }] });
    const mapping = { nodes: { frame: 'ref:A', id: 'service' } };
    expect(() => applyMapping([frame, { ...frame }], mapping)).toThrow('ambiguous');
    expect(() => applyMapping([], mapping)).toThrow('unavailable');
    expect(() => applyMapping([{ ...frame, fields: [...frame.fields, frame.fields[0]] }], mapping)).toThrow('duplicate column');
    expect(() => applyMapping([frame], { nodes: { frame: 'ref:A', id: 'missing' } })).toThrow('missing');
    expect(() => applyMapping([{ ...frame, refId: undefined }], { nodes: { frame: 'unnamed', id: 'service' } })).toThrow('stable identity');
  });

  it('rejects replacement of a canonical field and mismatched declared units', () => {
    const frame = toDataFrame({ refId: 'A', fields: [{ name: 'id', values: ['original'] }, { name: 'service', values: ['other'] }] });
    expect(() => applyMapping([frame], { nodes: { frame: 'ref:A', id: 'service' } })).toThrow('replace existing data');
    for (const [canonical, unit] of [['latencyP95Ms', 's'], ['errorRate', 'percent'], ['requestRate', 'bytes']]) {
      const other = toDataFrame({ refId: 'A', fields: [{ name: 'service', values: ['web'] }, { name: 'value', values: [10], config: { unit } }] });
      expect(() => applyMapping([other], { nodes: { frame: 'ref:A', id: 'service', [canonical]: 'value' } })).toThrow('Convert it in the query');
    }
  });

  it('accepts canonical-unit aliases without converting values, and no mapping leaves frames intact', () => {
    const frame = toDataFrame({ name: 'services', fields: [{ name: 'service', values: ['web'] },
      { name: 'failed', values: [0.2], config: { unit: 'percentunit' } }, { name: 'p95', values: [800], config: { unit: 'ms' } }] });
    const mapped = applyMapping([frame], { nodes: { frame: 'name:services', id: 'service', errorRate: 'failed', latencyP95Ms: 'p95' } });
    const node = framesToSnapshot(mapped, 1).snapshot!.nodes[0];
    expect(node.errorRate).toBe(0.2); expect(node.latencyP95Ms).toBe(800);
    expect(applyMapping([frame], {})).toEqual([frame]);
  });

  it.each(['{', 'null', '[]', '{"nodes":[]}', '{"nodes":{"frame":"ref:A"}}', '{"edges":{"frame":"ref:A","source":"s"}}',
    '{"nodes":{"frame":"ref:A","id":2}}', '{"nodes":{"frame":"ref:A","id":"service","__proto__":"x"}}', '{"query":"run"}'])('rejects malformed mapping %s', raw => {
    expect(() => parseMapping(raw)).toThrow();
  });
});
