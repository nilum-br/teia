import React, { use, useEffect, useRef } from 'react';
import { LoadingState, PanelProps, ThemeContext } from '@grafana/data';
import { TeiaOptions } from '../types';
import { framesToSnapshot } from '../teia/frames';
import TeiaUI, { TeiaController } from '../teia/teia-ui';

/**
 * The teia map inside a Grafana panel. The map is the same component teia
 * serves on its own; here it is fed by the panel queries (refreshing with the
 * dashboard), or streams from teia directly when a teia URL is set.
 */
export const TeiaPanel: React.FC<PanelProps<TeiaOptions>> = ({ data, options, width, height, id, replaceVariables, timeRange }) => {
  const hostRef = useRef<HTMLDivElement>(null);
  const uiRef = useRef<TeiaController | null>(null);
  const roundRef = useRef(0);
  const theme = use(ThemeContext);
  const mode = theme.isDark ? 'dark' : 'light';
  // Layout and filters are remembered per dashboard and panel: panel ids
  // repeat across dashboards, dashboard uids do not.
  const storageKey = `panel-${replaceVariables('${__dashboard.uid}') || 'local'}-${id}`;
  const journey = journeyOf(replaceVariables(options.journey || ''));
  const from = timeRange?.from.toISOString(), to = timeRange?.to.toISOString();

  useEffect(() => {
    if (!hostRef.current) {
      return;
    }
    uiRef.current = TeiaUI.mount(hostRef.current, {
      mode: 'panel',
      api: options.teiaUrl || undefined,
      storageKey,
      theme: mode,
      groupBy: options.groupBy,
      flow: options.flow,
      journey,
      journeyKey: options.journeyKey,
      values: options.values,
    });
    return () => {
      uiRef.current?.destroy();
      uiRef.current = null;
    };
    // Remount only when the data path changes; theme, options and data have their own effects.
    // eslint-disable-next-line react-hooks/exhaustive-deps, @eslint-react/exhaustive-deps
  }, [options.teiaUrl, storageKey]);

  useEffect(() => {
    uiRef.current?.setTheme(mode);
  }, [mode]);

  useEffect(() => {
    uiRef.current?.setOptions({ requestedRange: from && to ? { from, to } : undefined });
  }, [from, to, options.teiaUrl, storageKey]);

  useEffect(() => {
    uiRef.current?.setOptions({ groupBy: options.groupBy, flow: options.flow, journey, journeyKey: options.journeyKey ?? '', values: options.values });
  }, [options.groupBy, options.flow, journey, options.journeyKey, options.values]);

  useEffect(() => {
    if (options.teiaUrl || !uiRef.current) {
      return;
    }
    if (data.state === LoadingState.Error) {
      uiRef.current.setStatus(data.errors?.[0]?.message || 'the panel query failed');
      return;
    }
    uiRef.current.setStatus('');
    const { snapshot, changes } = framesToSnapshot(data.series, ++roundRef.current);
    if (snapshot) {
      uiRef.current.update(snapshot, changes);
    }
  }, [data, options.teiaUrl]);

  return <div ref={hostRef} style={{ width, height }} data-testid="teia-panel" />;
};

/** journeyOf reads the panel's journey after variables: an "All" choice, or
 * a variable left unresolved, means the whole map. */
export function journeyOf(value: string): string {
  const v = value.trim();
  return v === '*' || v === 'All' || v === '$__all' || v.startsWith('$') || v.startsWith('{') ? '' : v;
}
