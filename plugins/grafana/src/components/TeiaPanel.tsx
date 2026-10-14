import React, { useEffect, useRef } from 'react';
import { LoadingState, PanelProps } from '@grafana/data';
import { useTheme2 } from '@grafana/ui';
import { TeiaOptions } from '../types';
import { framesToSnapshot } from '../teia/frames';
import TeiaUI, { TeiaController } from '../teia/teia-ui';

/**
 * The teia map inside a Grafana panel. The map is the same component teia
 * serves on its own; here it is fed by the panel queries (refreshing with the
 * dashboard), or streams from teia directly when a teia URL is set.
 */
export const TeiaPanel: React.FC<PanelProps<TeiaOptions>> = ({ data, options, width, height, id, replaceVariables }) => {
  const host = useRef<HTMLDivElement>(null);
  const ui = useRef<TeiaController | null>(null);
  const round = useRef(0);
  const theme = useTheme2();
  const mode = theme.isDark ? 'dark' : 'light';
  // Layout and filters are remembered per dashboard and panel: panel ids
  // repeat across dashboards, dashboard uids do not.
  const storageKey = `panel-${replaceVariables('${__dashboard.uid}') || 'local'}-${id}`;

  useEffect(() => {
    if (!host.current) {
      return;
    }
    ui.current = TeiaUI.mount(host.current, {
      mode: 'panel',
      api: options.teiaUrl || undefined,
      storageKey,
      theme: mode,
      groupBy: options.groupBy,
      flow: options.flow,
    });
    return () => {
      ui.current?.destroy();
      ui.current = null;
    };
    // Remount only when the data path changes; theme, options and data have their own effects.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [options.teiaUrl, storageKey]);

  useEffect(() => {
    ui.current?.setTheme(mode);
  }, [mode]);

  useEffect(() => {
    ui.current?.setOptions({ groupBy: options.groupBy, flow: options.flow });
  }, [options.groupBy, options.flow]);

  useEffect(() => {
    if (options.teiaUrl || !ui.current) {
      return;
    }
    if (data.state === LoadingState.Error) {
      ui.current.setStatus(data.errors?.[0]?.message || 'the panel query failed');
      return;
    }
    ui.current.setStatus('');
    const { snapshot, changes } = framesToSnapshot(data.series, ++round.current);
    if (snapshot) {
      ui.current.update(snapshot, changes);
    }
  }, [data, options.teiaUrl]);

  return <div ref={host} style={{ width, height }} data-testid="teia-panel" />;
};
