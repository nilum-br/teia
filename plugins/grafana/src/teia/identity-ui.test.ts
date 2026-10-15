import TeiaUI, { TeiaIdentityResolution, TeiaSnapshot } from './teia-ui';

beforeAll(() => {
  const svg = window.SVGElement.prototype as unknown as Record<string, unknown>;
  svg.getTotalLength = () => 100;
  svg.getPointAtLength = () => ({ x: 0, y: 0 });
  svg.getBBox = () => ({ x: 0, y: 0, width: 40, height: 12 });
  (window as unknown as Record<string, unknown>).ResizeObserver = class { observe() {} disconnect() {} };
});

function snapshot(): TeiaSnapshot {
  return { round: 1, generatedAt: '2040-01-01T00:00:00Z', nodes: [], edges: [], findings: [], sources: [], coverage: { nodes: 0, edges: 0, visibility: 0, declared: 0, observed: 0 } };
}

function decision(input: string, resolvedId: string): TeiaIdentityResolution {
  return { input, resolvedId, method: 'ambiguous', source: '<img src=x onerror=alert(1)>', sourceType: 'prometheus', timeBasis: 'evaluation-window',
    collectedAt: '2026-10-15T12:00:00Z', evaluatedAt: '2026-10-15T11:59:59Z', windowSeconds: 60, retained: true,
    detail: 'No workload owner selected; the connection was withheld.',
    candidates: [{ id: '<script>alert(1)</script>', source: 'inventory', sourceType: 'docker', since: '2026-10-15T10:00:00Z', retained: true }] };
}

function mounted(snap: TeiaSnapshot, groupBy = '') {
  const host = document.createElement('div');
  document.body.append(host);
  const controller = TeiaUI.mount(host, { mode: 'panel', flow: false, groupBy, storageKey: 'identity-test' });
  controller.update(snap, []);
  const root = host.shadowRoot!;
  const select = (selector: string) => root.querySelector(selector)!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  return { root, select, controller, close: () => { controller.destroy(); host.remove(); } };
}

describe('identity details', () => {
  beforeEach(() => localStorage.clear());

  it('renders safe text, ambiguity, original times and retained candidates', () => {
    const snap = snapshot();
    snap.nodes = [{ id: 'orders', kind: 'service', health: 'unknown', visibility: 'partial', sources: [], identity: [decision('ip:10.0.0.1', 'ip:10.0.0.1')] }];
    const map = mounted(snap);
    try {
      map.select('.node');
      const details = map.root.querySelector('[data-disclosure-key="identity-provenance"]')!;
      expect(details.textContent).toContain('1 ambiguous');
      expect(details.textContent).toContain('Ambiguous identity');
      expect(details.textContent).toContain('No workload owner was selected');
      expect(details.textContent).toContain('retained inventory; current ownership not refreshed');
      expect(details.textContent).toContain('2026-10-15T10:00:00.000Z');
      expect(details.textContent).toContain('Original observation: unknown → unknown');
      expect(details.textContent).not.toContain('2040-01-01');
      expect(details.textContent).toContain('<img src=x onerror=alert(1)>');
      expect(details.textContent).toContain('<script>alert(1)</script>');
      expect(details.querySelector('img,script,a')).toBeNull();
      expect(details.textContent).not.toContain('%');
    } finally { map.close(); }
  });

  it('keeps caller and callee decisions distinct for grouped connections', () => {
    const snap = snapshot();
    snap.nodes = ['a', 'b', 'db'].map(id => ({ id, kind: 'service', health: 'unknown', visibility: 'partial', sources: [], labels: id === 'db' ? undefined : { team: 'shop' }, identity: [decision(id, id)] }));
    snap.edges = ['a', 'b'].map(from => ({ from, to: 'db', state: 'ghost', health: 'unknown', declared: false, observed: true, sources: [], signals: [], identity: {
      from: [{ ...decision(from, from), method: 'name' }], to: [{ ...decision('old-db', 'db'), method: 'alias' }],
    } }));
    const map = mounted(snap, 'team');
    try {
      map.select('.edge');
      let details = map.root.querySelector('[data-disclosure-key="identity-provenance"]')!;
      expect(details.textContent).toContain('a → db · caller');
      expect(details.textContent).toContain('b → db · caller');
      expect(details.textContent).toContain('a → db · callee');
      expect(details.textContent).toContain('b → db · callee');
      expect(details.textContent).toContain('Reported name');
      expect(details.textContent).toContain('Configured alias');
      map.select('.node[data-id="group:shop"]');
      details = map.root.querySelector('[data-disclosure-key="identity-provenance"]')!;
      expect(details.textContent).toContain('Identity and provenance (2');
      expect(details.querySelectorAll('article')).toHaveLength(2);
    } finally { map.close(); }
  });

  it('groups one decision reported by several sources and lists consequential methods first', () => {
    const snap = snapshot();
    const named = (source: string, collectedAt: string): TeiaIdentityResolution => ({ input: 'orders', resolvedId: 'orders', method: 'name', source,
      sourceType: source, timeBasis: 'not-applicable', detail: 'The source supplied this name.', collectedAt });
    snap.nodes = [{ id: 'orders', kind: 'service', health: 'unknown', visibility: 'partial', sources: [], identity: [
      named('prometheus', '2026-10-15T12:00:00Z'), named('tempo', '2026-10-15T12:01:00Z'),
      { ...decision('ip:10.0.0.7', 'orders'), method: 'ip-temporal', source: 'loki', retained: false, candidates: [] },
    ] }];
    const map = mounted(snap);
    try {
      map.select('.node');
      const cards = [...map.root.querySelector('[data-disclosure-key="identity-provenance"]')!.querySelectorAll('article')];
      expect(cards).toHaveLength(2);
      expect(cards[0].textContent).toContain('IP temporal match');
      expect(cards[0].textContent).toContain('Time basis: query evaluation window');
      expect(cards[1].textContent).toContain('Sources: prometheus, tempo');
      expect(cards[1].textContent).toContain('No time anchor is required for this match. Last collected: 2026-10-15T12:01:00.000Z');
      expect(cards[1].textContent).not.toContain('Time basis');
    } finally { map.close(); }
  });

  it('states that metadata is unavailable on an older snapshot', () => {
    const snap = snapshot();
    snap.nodes = [{ id: 'old', kind: 'service', health: 'unknown', visibility: 'partial', sources: [] }];
    const map = mounted(snap);
    try {
      map.select('.node');
      expect(map.root.querySelector('[data-disclosure-key="identity-provenance"]')?.textContent).toContain('Identity provenance was not supplied');
    } finally { map.close(); }
  });
});
