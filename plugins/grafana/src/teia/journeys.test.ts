import { toDataFrame } from '@grafana/data';
import fixture from '../../../../internal/journey/testdata/shared.json';
import { framesToSnapshot } from './frames';

const contract = {
  version: 1, mode: 'ownership', key: 'journey', maxDepth: 2, maxNodes: 200,
  scopes: Object.entries(fixture.expected).map(([name, scope]) => ({ ...scope, name, key: 'journey' })),
};

function frames(value: unknown = contract) {
  const nodes = fixture.snapshot.nodes;
  const edges = fixture.snapshot.edges;
  return [
    toDataFrame({ refId: 'nodes', fields: [
      { name: 'id', values: nodes.map((n) => n.id) },
      { name: 'kind', values: nodes.map((n) => n.kind) },
      { name: 'journeys', values: nodes.map((n) => JSON.stringify(n.journeys ?? [])) },
      { name: 'journeyContract', values: nodes.map((_, i) => i === 0 ? JSON.stringify(value) : '') },
    ] }),
    toDataFrame({ refId: 'edges', fields: [
      { name: 'source', values: edges.map((e) => e.from) },
      { name: 'target', values: edges.map((e) => e.to) },
    ] }),
  ];
}

describe('ownership journey frames', () => {
  it('preserves the engine fixture scope, boundaries and explicit memberships', () => {
    const snapshot = framesToSnapshot(frames(), 1).snapshot!;
    expect(snapshot.journeys).toEqual(contract);
    expect(snapshot.nodes.find((n) => n.id === 'web')!.journeys).toEqual(['checkout', 'browse']);
    const checkout = snapshot.journeys!.scopes.find((s) => s.name === 'checkout')!;
    expect(checkout.roles.batch).toBeUndefined();
    expect(checkout.entryEdges).not.toContain('batch->db');
  });

  it.each([null, { ...contract, version: 2 }, { ...contract, mode: 'transaction' }, { ...contract, scopes: [{}] }])(
    'ignores unsupported or malformed contracts without inventing correlation: %p', (value) => {
      expect(framesToSnapshot(frames(value), 1).snapshot!.journeys).toBeUndefined();
    });

  it('rejects a scope referring to data missing from the current frame', () => {
    const value = { ...contract, scopes: contract.scopes.map((scope) => ({ ...scope, entryEdges: [...scope.entryEdges] })) };
    value.scopes[0].entryEdges.push('absent->orders');
    expect(framesToSnapshot(frames(value), 1).snapshot!.journeys).toBeUndefined();
  });
});
