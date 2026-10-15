import React from 'react';
import { render } from '@testing-library/react';
import { createTheme, dateTime, LoadingState, PanelProps, ThemeContext, toDataFrame } from '@grafana/data';
import { journeyOf, TeiaPanel } from './TeiaPanel';
import { TeiaOptions } from '../types';

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

  it('draws the nodes from the panel queries', () => {
    const { container } = render(<TeiaPanel {...props()} />);
    const ids = [...shadow(container).querySelectorAll('.node')].map((n) => (n as HTMLElement).dataset.id);
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
    expect(root.querySelector('#rings')!.textContent).toContain('0%visibility');
    (root.querySelector('.edge path.hit') as SVGElement).dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(root.querySelector('#pane-details')!.textContent).toContain('50% errors · measured on 10% of traffic');
    expect(root.querySelector('#pane-details')!.textContent).toContain('p95 approximation');
    expect(root.querySelector('#pane-details')!.textContent).not.toContain('5.0%');
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
    expect(banner.textContent).toContain('original observation time unknown');
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

  it('shows a failed query instead of stale silence', () => {
    const { container, rerender } = render(<TeiaPanel {...props()} />);
    rerender(<TeiaPanel {...props({ data: { series: [], state: LoadingState.Error, errors: [{ message: 'datasource down' }] } as never })} />);
    expect(shadow(container).querySelector('#live')!.textContent).toContain('datasource down');
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
    expect(details).toContain('Why it is amber');
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
