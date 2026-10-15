import { toDataFrame } from '@grafana/data';
import { framesToSnapshot } from './frames';
import type { TeiaIdentityResolution } from './teia-ui';

const decision: TeiaIdentityResolution = {
  input: 'ip:10.0.0.1', resolvedId: 'orders', method: 'ip-temporal', source: 'network', sourceType: 'prometheus',
  detail: 'One owner covers the original window', timeBasis: 'evaluation-window', collectedAt: '2026-10-15T12:00:00Z',
  evaluatedAt: '2026-10-15T11:59:59Z', windowSeconds: 60, retained: true,
  candidates: [{ id: 'orders', source: 'inventory', sourceType: 'kubernetes', since: '2026-10-15T10:00:00Z', collectedAt: '2026-10-15T11:59:00Z', retained: true }],
};

describe('identity columns', () => {
  it.each([false, true])('preserves node and endpoint metadata from JSON-string or object cells (%s)', objects => {
    const target = { ...decision, input: 'old-db', resolvedId: 'db', method: 'alias' };
    const connection = { from: [decision], to: [target] };
    const value = (input: unknown) => objects ? input : JSON.stringify(input);
    const node = toDataFrame({ refId: 'nodes', fields: [{ name: 'id', values: ['orders'] }, { name: 'identity', values: [value([decision])] }] });
    const edge = toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['orders'] }, { name: 'target', values: ['db'] }, { name: 'identity', values: [value(connection)] }] });
    const { snapshot } = framesToSnapshot([node, edge], 1);
    expect(snapshot?.nodes[0].identity).toEqual([decision]);
    expect(snapshot?.edges[0].identity).toEqual(connection);
    expect(snapshot?.nodes[1].identity).toBeUndefined();
  });

  it.each([undefined, 'broken JSON', 'null', '42'])('does not invent provenance for a missing or malformed cell (%s)', value => {
    const node = toDataFrame({ refId: 'nodes', fields: [{ name: 'id', values: ['orders'] }, { name: 'identity', values: [value] }] });
    const edge = toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['orders'] }, { name: 'target', values: ['db'] }, { name: 'identity', values: [value] }] });
    const { snapshot } = framesToSnapshot([node, edge], 1);
    expect(snapshot?.nodes[0].identity).toEqual([]);
    expect(snapshot?.edges[0].identity).toBeUndefined();
  });
});
