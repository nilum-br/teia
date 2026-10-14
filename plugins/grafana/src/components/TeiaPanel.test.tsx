import React from 'react';
import { render } from '@testing-library/react';
import { LoadingState, PanelProps, toDataFrame } from '@grafana/data';
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

  it('remembers choices per dashboard and panel', () => {
    const { container } = render(<TeiaPanel {...props()} />);
    (shadow(container).querySelector('#flowBtn') as HTMLButtonElement).click();
    expect(localStorage.getItem('teia.panel-dash-1-7.flow')).toBe('true');
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

  it('shows only the journey a dashboard variable picks, with stubs where it leaves', () => {
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
    const drawn = () => [...root.querySelectorAll('.node')].map((n) => `${(n as HTMLElement).dataset.id}${n.classList.contains('stub') ? ' (stub)' : ''}`).sort();
    expect(drawn()).toEqual(['catalog (stub)', 'orders', 'web']);
    expect(root.querySelector('.journey-chip')?.textContent).toContain('checkout');
    rerender(<TeiaPanel {...props({ data: { series: shop, state: LoadingState.Done }, options: { teiaUrl: '', groupBy: '', flow: false, journey: '', values: true } } as never)} />);
    expect(drawn()).toEqual(['catalog', 'orders', 'web']);
    expect(localStorage.getItem('teia.panel-dash-1-7.journey')).toBeNull();
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
