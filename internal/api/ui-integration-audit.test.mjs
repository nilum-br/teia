import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { it } from 'node:test';

const { core } = createRequire(import.meta.url)('./ui/teia-ui.js');

it('invalidates the rendered view when external frames gain declared-intent semantics', () => {
  const external = core.sanitize({
    nodes: [{ id: 'web' }, { id: 'orders' }],
    edges: [{ from: 'web', to: 'orders', declared: false, observed: true, state: 'ok' }],
    drift: false,
  });
  // A query can add a state/intent mapping without changing its node/edge
  // values. Coverage rings and drift explanations must still be redrawn.
  const withIntent = core.sanitize({ ...external, drift: true });
  assert.notEqual(core.signature(external), core.signature(withIntent));
});
