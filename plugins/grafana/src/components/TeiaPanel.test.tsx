import React from 'react';
import { render } from '@testing-library/react';
import { createTheme, dateTime, LoadingState, PanelProps, ThemeContext, toDataFrame } from '@grafana/data';
import { journeyOf, TeiaPanel } from './TeiaPanel';
import { TeiaOptions } from '../types';
import TeiaUI from '../teia/teia-ui';
import { locationService, variables } from '../test/grafana-runtime';

// jsdom has no layout: give SVG geometry and ResizeObserver harmless answers.
beforeAll(() => {
  const svg = window.SVGElement.prototype as unknown as Record<string, unknown>;
  svg.getTotalLength = () => 100;
  svg.getPointAtLength = () => ({ x: 0, y: 0 });
  svg.getBBox = () => ({ x: 0, y: 0, width: 40, height: 12 });
  (window as unknown as Record<string, unknown>).ResizeObserver = class {
    observe() {}
    disconnect() {}
  };
});

const series = [
  toDataFrame({
    refId: 'nodes',
    fields: [
      { name: 'id', values: ['web', 'orders'] },
      { name: 'kind', values: ['service', 'service'] },
    ],
  }),
  toDataFrame({
    refId: 'edges',
    fields: [
      { name: 'source', values: ['web'] },
      { name: 'target', values: ['orders'] },
      { name: 'requestRate', values: [3] },
      { name: 'declared', values: [true] },
    ],
  }),
];

function props(over: Partial<PanelProps<TeiaOptions>> = {}): PanelProps<TeiaOptions> {
  return {
    id: 7,
    data: { series, state: LoadingState.Done },
    options: { teiaUrl: '', groupBy: '', flow: false, journey: '', values: true },
    width: 800,
    height: 500,
    replaceVariables: (s: string) => (s === '${__dashboard.uid}' ? 'dash-1' : s === '${journey}' ? 'checkout' : s),
    ...over,
  } as unknown as PanelProps<TeiaOptions>;
}

const shadow = (container: HTMLElement) => container.querySelector('[data-testid="teia-panel"]')!.shadowRoot!;

describe('TeiaPanel', () => {
  beforeEach(() => localStorage.clear());

  describe('slices', () => {
    // Frames from the panel queries, whole or holding one journey's calls.
    const frames = (journey?: string) => [
      toDataFrame({
        refId: 'nodes',
        fields: [
          { name: 'id', values: ['web', 'orders'] },
          { name: 'kind', values: ['service', 'service'] },
          { name: 'dimensions', values: [JSON.stringify({ journey: ['browse', 'checkout'] }), ''] },
          { name: 'slice', values: [journey ? JSON.stringify({ selector: { journey }, matched: true, metrics: 'scoped', sources: ['tempo'] }) : '', ''] },
        ],
      }),
      series[1],
    ];
    const withData = (journey?: string) => props({ data: { series: frames(journey), state: LoadingState.Done } as never });
    const sliceButtons = (root: ShadowRoot) => [...root.querySelectorAll<HTMLButtonElement>('.jtab')].filter((b) => b.textContent?.startsWith('journey: '));
    afterEach(() => {
      variables.length = 0;
      locationService.partial.mockClear();
    });

    it('sets the dashboard variable the queries slice by, and follows the frames that come back', () => {
      variables.push({ name: 'slice' });
      const { container, rerender } = render(<TeiaPanel {...withData()} />);
      sliceButtons(shadow(container)).find((b) => b.textContent === 'journey: checkout')!.click();
      expect(locationService.partial).toHaveBeenCalledWith({ 'var-slice': 'journey:checkout' }, true);
      rerender(<TeiaPanel {...withData('checkout')} />);
      const chip = shadow(container).querySelector('.fchip.slice')!;
      expect(chip.textContent).toContain('Calls of journey = checkout');
      chip.querySelector('button')!.click();
      expect(locationService.partial).toHaveBeenLastCalledWith({ 'var-slice': '' }, true);
    });

    it('only labels sliced frames when the dashboard has no slice variable or a teia URL slices', () => {
      const { container } = render(<TeiaPanel {...withData('checkout')} />);
      const chip = shadow(container).querySelector('.fchip.slice')!;
      expect(chip.textContent).toContain('Calls of journey = checkout');
      expect(chip.querySelector('button')).toBeNull();
      expect(sliceButtons(shadow(container))).toHaveLength(0);
      variables.push({ name: 'other' });
      const { container: unrelated } = render(<TeiaPanel {...withData()} />);
      expect(sliceButtons(shadow(unrelated))).toHaveLength(0);
      expect(locationService.partial).not.toHaveBeenCalled();
    });
  });

  describe('refreshes', () => {
    // teia's frames of one round: every row carries the round's time.
    const round = (at: string) => [
      toDataFrame({ refId: 'nodes', fields: [{ name: 'id', values: ['web', 'orders'] }, { name: 'snapshotAt', values: [at, at] }] }),
      toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['web'] }, { name: 'target', values: ['orders'] }, { name: 'snapshotAt', values: [at] }] }),
    ];
    const withSeries = (s: ReturnType<typeof round>) => props({ data: { series: s, state: LoadingState.Done } as never });
    const updates = () => {
      const mount = jest.spyOn(TeiaUI, 'mount');
      const update = jest.fn();
      mount.mockImplementation((host, options) => {
        mount.mockRestore();
        const ui = TeiaUI.mount(host, options);
        const draw = ui.update.bind(ui);
        ui.update = (...args) => { update(); draw(...args); };
        return ui;
      });
      return update;
    };

    it('draws a refresh of the same round only once', () => {
      const update = updates();
      const { rerender } = render(<TeiaPanel {...withSeries(round('2026-10-16T12:00:00Z'))} />);
      // A dashboard refresh brings new frame objects with the same round.
      rerender(<TeiaPanel {...withSeries(round('2026-10-16T12:00:00Z'))} />);
      rerender(<TeiaPanel {...withSeries(round('2026-10-16T12:00:00Z'))} />);
      expect(update).toHaveBeenCalledTimes(1);
      rerender(<TeiaPanel {...withSeries(round('2026-10-16T12:00:30Z'))} />);
      expect(update).toHaveBeenCalledTimes(2);
    });

    it('always draws frames that do not say their round', () => {
      const update = updates();
      const { rerender } = render(<TeiaPanel {...props()} />);
      rerender(<TeiaPanel {...props({ data: { series: [...series], state: LoadingState.Done } as never })} />);
      expect(update).toHaveBeenCalledTimes(2);
    });
  });

  it('draws the nodes from the panel queries', () => {
    const { container } = render(<TeiaPanel {...props()} />);
    const ids = [...shadow(container).querySelectorAll('.node')].map((n) => (n as HTMLElement).dataset.id);
    expect(ids.sort()).toEqual(['orders', 'web']);
    expect(shadow(container).querySelector('#empty')!.hasAttribute('hidden')).toBe(true);
  });

  it('maps custom columns and reports broken aliases without replacing the mounted map', () => {
    const custom = [toDataFrame({ refId: 'A', fields: [{ name: 'service', values: ['custom-service'] }, { name: 'requests', values: [10] }] })];
    const original = props({ data: { series: custom, state: LoadingState.Done } as never });
    const { container, rerender } = render(<TeiaPanel {...original} />);
    const map = shadow(container).querySelector('.teia');
    const options = { ...original.options, frameMapping: JSON.stringify({ nodes: { frame: 'ref:A', id: 'service', requestRate: 'requests' } }) };
    rerender(<TeiaPanel {...{ ...original, options }} />);
    expect(shadow(container).querySelector('.teia')).toBe(map);
    expect(shadow(container).querySelector('.node[data-id="custom-service"]')).not.toBeNull();
    rerender(<TeiaPanel {...{ ...original, options: { ...options, frameMapping: JSON.stringify({ nodes: { frame: 'ref:A', id: 'missing' } }) } }} />);
    expect(shadow(container).querySelector('#live')!.textContent).toContain('Column missing is missing');
    expect(shadow(container).querySelector('.teia')).toBe(map);
  });

  it('saves setup using the latest options callback without a remount', () => {
    const mount = jest.spyOn(TeiaUI, 'mount');
    try {
      const first = jest.fn(), latest = jest.fn();
      const original = props({ onOptionsChange: first });
      const { container, rerender } = render(<TeiaPanel {...original} />);
      const map = shadow(container).querySelector('.teia');
      const callback = mount.mock.calls[mount.mock.calls.length - 1][1]!.onMappingChange!;
      rerender(<TeiaPanel {...{ ...original, onOptionsChange: latest, options: { ...original.options, values: false } }} />);
      callback({ nodes: { frame: 'ref:nodes', id: 'id' } });
      expect(first).not.toHaveBeenCalled();
      expect(latest).toHaveBeenCalledWith(expect.objectContaining({ values: false, frameMapping: '{"nodes":{"frame":"ref:nodes","id":"id"}}' }));
      expect(shadow(container).querySelector('.teia')).toBe(map);
    } finally { mount.mockRestore(); }
  });

  it.each(['panel id', 'dashboard UID'])('replays unchanged query data after a %s remount', (identity) => {
    const original = props();
    const { container, rerender } = render(<TeiaPanel {...original} />);
    const firstMap = shadow(container).querySelector('.teia');
    // Keep data and options referentially unchanged: only the controller's
    // storage scope changes, so waiting for another query would leave it blank.
    const next = identity === 'panel id' ? { ...original, id: 8 } : {
      ...original,
      replaceVariables: (value: string) => value === '${__dashboard.uid}' ? 'dash-2' : original.replaceVariables(value),
    };
    rerender(<TeiaPanel {...next} />);
    expect(shadow(container).querySelector('.teia')).not.toBe(firstMap);
    const ids = [...shadow(container).querySelectorAll('.node')].map(n => (n as HTMLElement).dataset.id);
    expect(ids.sort()).toEqual(['orders', 'web']);
    expect(shadow(container).querySelector('#empty')!.hasAttribute('hidden')).toBe(true);
  });

  it('follows Grafana theme changes without remounting the map', () => {
    const panel = <TeiaPanel {...props()} />;
    const { container, rerender } = render(
      <ThemeContext value={createTheme({ colors: { mode: 'dark' } })}>{panel}</ThemeContext>
    );
    const host = container.querySelector('[data-testid="teia-panel"]') as HTMLElement;
    const map = shadow(container).querySelector('.teia');
    expect(host.dataset.theme).toBe('dark');
    rerender(<ThemeContext value={createTheme({ colors: { mode: 'light' } })}>{panel}</ThemeContext>);
    expect(host.dataset.theme).toBe('light');
    expect(shadow(container).querySelector('.teia')).toBe(map);
    expect(shadow(container).querySelectorAll('.node')).toHaveLength(2);
  });

  it('qualifies mixed error coverage in the frame, line details and coverage rings', () => {
    const mixed = [
      toDataFrame({ refId: 'nodes', fields: [
        { name: 'id', values: ['web', 'a', 'b'] },
        { name: 'labels', values: ['{"journey":"shop"}', '{"journey":"shop","team":"backend"}', '{"journey":"shop","team":"backend"}'] },
        { name: 'requestRate', values: [1000, 100, 900] },
      ] }),
      toDataFrame({ refId: 'edges', fields: [
        { name: 'source', values: ['web', 'web'] }, { name: 'target', values: ['a', 'b'] },
        { name: 'requestRate', values: [100, 900] }, { name: 'errorRate', values: [0.5, null] },
        { name: 'latencyP95Ms', values: [1000, 3000] },
      ] }),
    ];
    const p = props({ data: { series: mixed, state: LoadingState.Done } as never,
      options: { teiaUrl: '', groupBy: 'team', journey: 'shop', journeyKey: '', flow: false, values: true } });
    const { container } = render(<TeiaPanel {...p} />);
    const root = shadow(container);
    expect(root.querySelector('.frame-stats')!.textContent).toContain('50% err (10% covered)');
    expect(root.querySelector('.frame-stats')!.textContent).toContain('≈ p95');
    expect(root.querySelector('#rings')!.textContent).toContain('3blind spots');
    (root.querySelector('.edge path.hit') as SVGElement).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(root.querySelector('#pane-details')!.textContent).toContain('50% errors · measured on 10% of traffic');
    expect(root.querySelector('#pane-details')!.textContent).toContain('p95 approximation');
    expect(root.querySelector('#pane-details')!.textContent).not.toContain('5.0%');
  });

  it('keeps identical evidence from different grouped members identifiable', () => {
    const timing = { collectedAt: '2026-10-15T12:00:00Z', observedFrom: '2026-10-15T11:50:00Z', observedAt: '2026-10-15T11:55:00Z' };
    const sample = JSON.stringify([{ source: 'tempo', signal: 'traces', ...timing, sampleCount: 3, metrics: ['latency'] }]);
    const inventory = JSON.stringify([{ source: 'inventory', signal: 'docker', collectedAt: timing.collectedAt, metrics: [] }]);
    const grouped = [
      toDataFrame({ refId: 'nodes', fields: [
        { name: 'id', values: ['web', 'a', 'b'] }, { name: 'labels', values: ['{}', '{"team":"backend"}', '{"team":"backend"}'] },
        { name: 'evidence', values: ['[]', inventory, inventory] },
      ] }),
      toDataFrame({ refId: 'edges', fields: [
        { name: 'source', values: ['web', 'web'] }, { name: 'target', values: ['a', 'b'] }, { name: 'evidence', values: [sample, sample] },
      ] }),
    ];
    const { container } = render(<TeiaPanel {...props({ data: { series: grouped, state: LoadingState.Done } as never,
      options: { teiaUrl: '', groupBy: 'team', flow: false, journey: '', journeyKey: '', values: true } })} />);
    const root = shadow(container);
    (root.querySelector('.edge path.hit') as SVGElement).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    const details = root.querySelector('#pane-details')!;
    const entries = [...details.querySelectorAll('p.explain')].filter(p => p.textContent?.includes('Sampled calls: 3'));
    expect(entries).toHaveLength(2);
    expect(entries[0].textContent).toContain('web → a');
    expect(entries[1].textContent).toContain('web → b');
    (root.querySelector('.node[data-id="group:backend"]') as SVGElement).dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    const members = [...details.querySelectorAll('p.explain')].filter(p => p.textContent?.includes('inventory · docker'));
    expect(members).toHaveLength(2);
    expect(members[0].textContent).toContain('a · inventory');
    expect(members[1].textContent).toContain('b · inventory');
  });

  it('remembers choices per dashboard and panel', () => {
    const { container } = render(<TeiaPanel {...props()} />);
    (shadow(container).querySelector('#flowBtn') as HTMLButtonElement).click();
    expect(localStorage.getItem('teia.panel-dash-1-7.flow')).toBe('true');
  });

  it('keeps current snapshots explicit across dashboard ranges, pause and query errors', () => {
    const current = series.map(frame => ({ ...frame, meta: { custom: { teia: {
      timeMode: 'current', snapshotAt: '2026-10-15T12:00:00Z',
    } } } }));
    const range = (days: number) => ({ from: dateTime('2026-10-15T12:00:00Z').subtract(days, 'days'),
      to: dateTime('2026-10-15T12:00:00Z'), raw: { from: `now-${days}d`, to: 'now' } });
    const p = props({ data: { series: current, state: LoadingState.Done } as never, timeRange: range(1) });
    const { container, rerender } = render(<TeiaPanel {...p} />);
    const root = shadow(container);
    const banner = root.querySelector('#timeContext')!;
    expect(banner.textContent).toContain('Current state');
    expect(banner.textContent).toContain('dashboard range does not change this snapshot');
    expect(banner.textContent).toContain('Original observation time unknown');
    const original = banner.textContent;
    rerender(<TeiaPanel {...{ ...p, timeRange: range(30) }} />);
    expect(banner.textContent).toBe(original);
    expect(banner.getAttribute('title')).toContain('2026-09-15');
    (root.querySelector('#liveBtn') as HTMLButtonElement).click();
    rerender(<TeiaPanel {...{ ...p, data: { series: [], state: LoadingState.Error, errors: [{ message: 'down' }] } as never }} />);
    expect(banner.textContent).toBe(original);
    expect(root.querySelector('#live')!.textContent).toContain('down');
  });

  it('shows an unknown effective period and preserves original evidence timestamps', () => {
    const data = [toDataFrame({ refId: 'nodes', fields: [
      { name: 'id', values: ['old'] },
      { name: 'evidence', values: [JSON.stringify([{ source: 'metrics', signal: 'metrics', collectedAt: '2026-10-15T12:00:00Z',
        observedAt: '2026-10-14T10:00:00Z', windowSeconds: 300, retained: true, metrics: ['topology'] }])] },
    ] })];
    const { container } = render(<TeiaPanel {...props({ data: { series: data, state: LoadingState.Done } as never })} />);
    const root = shadow(container);
    expect(root.querySelector('#timeContext')!.textContent).toContain('effective period unknown');
    expect(root.querySelector('#timeContext')!.textContent).toContain('collection time unknown');
    (root.querySelector('.node') as SVGElement).dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    const detail = root.querySelector('#pane-details')!.textContent;
    expect(detail).toContain('2026-10-14T10:00:00.000Z');
    expect(detail).toContain('retained topology');
    expect(detail).toContain('Query lookback: 5 min');
  });

  it.each([
    { name: 'old sample', from: '2026-10-15T10:00:00Z', to: '2026-10-15T11:00:00Z', expected: 'age at collection: 60 min ago' },
    { name: 'missing time', from: undefined, to: '2026-10-15T11:00:00Z', expected: 'Sample time range unknown' },
    { name: 'reversed time', from: '2026-10-15T11:30:00Z', to: '2026-10-15T11:00:00Z', expected: 'Sample time range unknown' },
    { name: 'future clock', from: '2026-10-15T12:01:00Z', to: '2026-10-15T12:02:00Z', expected: '2 min in the future; check source clock' },
  ])('qualifies $name as sampled evidence, including while paused', ({ from, to, expected }) => {
    const data = [series[0], toDataFrame({ refId: 'edges', fields: [
      { name: 'source', values: ['web'] }, { name: 'target', values: ['orders'] },
      { name: 'timeMode', values: ['current'] }, { name: 'snapshotAt', values: ['2026-10-15T13:00:00Z'] },
      { name: 'evidence', values: [JSON.stringify([{ source: 'tempo', signal: 'traces', collectedAt: '2026-10-15T12:00:00Z',
        observedFrom: from, observedAt: to, sampleCount: 3, windowSeconds: 900, metrics: ['topology', 'latency'] }])] },
    ] })];
    const p = props({ data: { series: data, state: LoadingState.Done } as never });
    const { container, rerender } = render(<TeiaPanel {...p} />);
    const root = shadow(container);
    (root.querySelector('.edge path.hit') as SVGElement).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    const detail = root.querySelector('#pane-details')!.textContent;
    expect(detail).toContain('Sampled calls: 3');
    expect(detail).toContain(expected);
    expect(detail).toContain('Sample bounds do not imply continuous coverage');
    expect(root.querySelector('#timeContext')!.textContent).toContain('Original observation time unknown');
    (root.querySelector('#liveBtn') as HTMLButtonElement).click();
    rerender(<TeiaPanel {...{ ...p, data: { series, state: LoadingState.Done } as never }} />);
    expect(root.querySelector('#pane-details')!.textContent).toBe(detail);
  });

  it('keeps gauge input age separate from evaluation, collection and other readings', () => {
    const data = [toDataFrame({ refId: 'nodes', fields: [
      { name: 'id', values: ['cache'] },
      { name: 'evidence', values: [JSON.stringify([
        { source: 'metrics', signal: 'prometheus', origin: 'redis (instance cache:9121)', collectedAt: '2026-10-15T12:00:00Z',
          evaluatedAt: '2026-10-15T11:59:59Z', oldestSampleAt: '2026-10-15T11:00:00Z', metrics: ['saturation: memory'] },
        { source: 'metrics', signal: 'prometheus', origin: 'redis (instance cache:9121)', collectedAt: '2026-10-15T12:00:00Z',
          evaluatedAt: '2026-10-15T11:59:59Z', metrics: ['saturation: clients'] },
      ])] },
    ] })];
    const { container } = render(<TeiaPanel {...props({ data: { series: data, state: LoadingState.Done } as never })} />);
    const root = shadow(container);
    (root.querySelector('.node') as SVGElement).dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    const entries = [...root.querySelectorAll('#pane-details p.explain')];
    const memory = entries.find(p => p.textContent?.includes('saturation: memory'))!.textContent;
    expect(memory).toContain('redis (instance cache:9121)');
    expect(memory).toContain('Query evaluated: 2026-10-15T11:59:59.000Z');
    expect(memory).toContain('Collected: 2026-10-15T12:00:00.000Z');
    expect(memory).toContain('Oldest input sample: 2026-10-15T11:00:00.000Z (age at collection: 60 min ago)');
    const clients = entries.find(p => p.textContent?.includes('saturation: clients'))!.textContent;
    expect(clients).toContain('Original observation: unknown');
    expect(clients).not.toContain('Oldest input sample:');
    expect(root.querySelector('#timeContext')!.textContent).toContain('Original observation time unknown');
  });

  it('does not disguise a future original observation as zero age', () => {
    const now = jest.spyOn(Date, 'now').mockReturnValue(Date.parse('2026-10-15T12:00:00Z'));
    try {
      const future = series.map(frame => ({ ...frame, meta: { custom: { teia: {
        timeMode: 'current', snapshotAt: '2026-10-15T12:00:00Z', observedAt: '2026-10-15T12:02:00Z',
      } } } }));
      const { container } = render(<TeiaPanel {...props({ data: { series: future, state: LoadingState.Done } as never })} />);
      expect(shadow(container).querySelector('#timeContext')!.textContent).toContain('Original observation 2 min in the future; check source clock');
    } finally {
      now.mockRestore();
    }
  });

  it('shows a failed query instead of stale silence', () => {
    const { container, rerender } = render(<TeiaPanel {...props()} />);
    rerender(<TeiaPanel {...props({ data: { series: [], state: LoadingState.Error, errors: [{ message: 'datasource down' }] } as never })} />);
    expect(shadow(container).querySelector('#live')!.textContent).toContain('datasource down');
  });

  it('accepts a new historical query while paused and replaces activity with that snapshot', () => {
    const make = (id: string) => [toDataFrame({ refId: 'nodes', fields: [
      { name: 'id', values: [id] }, { name: 'timeMode', values: ['historical'] }, { name: 'historyId', values: [id] },
      { name: 'history', values: [JSON.stringify({ id, availability: 'stored-snapshot', nodes: {}, edges: {} })] },
    ] }), toDataFrame({ refId: 'changes', fields: [
      { name: 'message', values: ['Activity for ' + id] }, { name: 'timeMode', values: ['historical'] }, { name: 'historyId', values: [id] },
      { name: 'at', values: ['2026-10-15T12:00:00Z'] },
    ] })];
    const { container, rerender } = render(<TeiaPanel {...props()} />);
    const root = shadow(container);
    (root.querySelector('#liveBtn') as HTMLButtonElement).click();
    rerender(<TeiaPanel {...props({ data: { series: make('one'), state: LoadingState.Done } as never })} />);
    expect(root.querySelector('#timeContext')!.textContent).toContain('Historical snapshot');
    expect(root.querySelector('#pane-activity')!.textContent).toContain('Activity for one');
    rerender(<TeiaPanel {...props({ data: { series: make('two'), state: LoadingState.Done } as never })} />);
    expect(root.querySelector('.node')?.getAttribute('data-id')).toBe('two');
    expect(root.querySelector('#pane-activity')!.textContent).toContain('Activity for two');
    expect(root.querySelector('#pane-activity')!.textContent).not.toContain('Activity for one');
  });

  it('opens the details of a line with its readings and why it is amber', () => {
    const queue = [
      toDataFrame({ refId: 'nodes', fields: [{ name: 'id', values: ['email-worker', 'order-events'] }] }),
      toDataFrame({
        refId: 'edges',
        fields: [
          { name: 'source', values: ['email-worker'] },
          { name: 'target', values: ['order-events'] },
          { name: 'requestRate', values: [0.5] },
          { name: 'declared', values: [false] },
          { name: 'measures', values: ['[{"cause":"backlog","what":"messages waiting","value":1500}]'] },
          { name: 'reasons', values: ['[{"cause":"backlog","health":"degraded","value":1500,"limit":1000,"summary":"1.5k messages waiting, over 1.0k","what":"messages waiting"}]'] },
        ],
      }),
    ];
    const { container } = render(<TeiaPanel {...props({ data: { series: queue, state: LoadingState.Done } as never })} />);
    const root = shadow(container);
    expect(root.querySelector('.why-tag')?.textContent).toContain('1.5k messages waiting');
    (root.querySelector('.edge path.hit') as SVGElement).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    const details = root.querySelector('#pane-details')!.textContent!;
    expect(details).toContain('Primary signal');
    expect(details).toContain('messages waiting');
    expect(details).toContain('1.5k');
  });

  it('shows only the journey a dashboard variable picks, with a port where it leaves', () => {
    const shop = [
      toDataFrame({
        refId: 'nodes',
        fields: [
          { name: 'id', values: ['web', 'orders', 'catalog'] },
          { name: 'kind', values: ['service', 'service', 'service'] },
          { name: 'labels', values: ['{"journey":"checkout,browse"}', '{"journey":"checkout"}', '{"journey":"browse"}'] },
        ],
      }),
      toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['web', 'web'] }, { name: 'target', values: ['orders', 'catalog'] }] }),
    ];
    const { container, rerender } = render(<TeiaPanel {...props({ data: { series: shop, state: LoadingState.Done }, options: { teiaUrl: '', groupBy: '', flow: false, journey: '${journey}', values: true } } as never)} />);
    const root = shadow(container);
    const drawn = () => [...root.querySelectorAll('.node')].map((n) => (n as HTMLElement).dataset.id).sort();
    expect(drawn()).toEqual(['orders', 'web']);
    expect(root.querySelector('.port')?.textContent).toContain('browse');
    expect(root.querySelector('.frame-name')?.textContent).toBe('checkout');
    expect(root.querySelector('.scope .fchip')?.textContent).toContain('checkout');
    expect(root.querySelector('.scope .headline')?.textContent).toContain('components');
    rerender(<TeiaPanel {...props({ data: { series: shop, state: LoadingState.Done }, options: { teiaUrl: '', groupBy: '', flow: false, journey: '', values: true } } as never)} />);
    expect(drawn()).toEqual(['catalog', 'orders', 'web']);
    expect(localStorage.getItem('teia.panel-dash-1-7.journey')).toBeNull();
  });

  it('finds journeys in the label the services already carry', () => {
    const partOf = [
      toDataFrame({
        refId: 'nodes',
        fields: [
          { name: 'id', values: ['web', 'orders', 'ledger'] },
          { name: 'labels', values: ['{"app.kubernetes.io/part-of":"shop"}', '{"app.kubernetes.io/part-of":"shop"}', '{"app.kubernetes.io/part-of":"finance"}'] },
        ],
      }),
      toDataFrame({ refId: 'edges', fields: [{ name: 'source', values: ['web'] }, { name: 'target', values: ['orders'] }] }),
    ];
    const { container } = render(<TeiaPanel {...props({ data: { series: partOf, state: LoadingState.Done }, options: { teiaUrl: '', groupBy: '', flow: false, journey: 'shop', journeyKey: '', values: true } } as never)} />);
    const root = shadow(container);
    expect([...root.querySelectorAll('.node')].map((n) => (n as HTMLElement).dataset.id).sort()).toEqual(['orders', 'web']);
  });

  it('treats an All choice or an unresolved variable as the whole map', () => {
    expect(['All', '$__all', '*', '${journey}', '{checkout,browse}', ''].map(journeyOf)).toEqual(['', '', '', '', '', '']);
    expect(journeyOf(' checkout ')).toBe('checkout');
  });

  it('cleans up when the panel goes away', () => {
    const { container, unmount } = render(<TeiaPanel {...props()} />);
    const root = shadow(container);
    unmount();
    expect(root.querySelector('.teia')).toBeNull();
  });
});
