import React, { use, useEffect, useRef } from 'react';
import { LoadingState, PanelProps, ThemeContext } from '@grafana/data';
import { getTemplateSrv, locationService } from '@grafana/runtime';
import { TeiaOptions } from '../types';
import { framesToSnapshot } from '../teia/frames';
import { applyMapping, describeFrames, parseMapping, validateMapping, type FrameMapping } from '../teia/mapping';
import TeiaUI, { TeiaController } from '../teia/teia-ui';

/**
 * The teia map inside a Grafana panel. The map is the same component teia
 * serves on its own; here it is fed by the panel queries (refreshing with the
 * dashboard), or streams from teia directly when a teia URL is set.
 */
export const TeiaPanel: React.FC<PanelProps<TeiaOptions>> = ({ data, options, onOptionsChange, width, height, id, replaceVariables, timeRange }) => {
  const hostRef = useRef<HTMLDivElement>(null);
  const uiRef = useRef<TeiaController | null>(null);
  const roundRef = useRef(0);
  const optionsRef = useRef({ options, onOptionsChange });
  const theme = use(ThemeContext);
  const mode = theme.isDark ? 'dark' : 'light';
  // Layout and filters are remembered per dashboard and panel: panel ids
  // repeat across dashboards, dashboard uids do not.
  const storageKey = `panel-${replaceVariables('${__dashboard.uid}') || 'local'}-${id}`;
  const journey = journeyOf(replaceVariables(options.journey || ''));
  const from = timeRange?.from.toISOString(), to = timeRange?.to.toISOString();
  // Queries reach teia through the data source, so a slice travels as a
  // dashboard variable they pass as ?slice=; a teia URL slices directly.
  const sliceVariable = options.teiaUrl ? '' : (options.sliceVariable ?? 'slice').trim();
  const canSlice = sliceVariable !== '' && hasVariable(sliceVariable);

  useEffect(() => { optionsRef.current = { options, onOptionsChange }; }, [options, onOptionsChange]);

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
      onMappingChange: (mapping: FrameMapping) => {
        try {
          const valid = validateMapping(mapping);
          const current = optionsRef.current;
          current.onOptionsChange?.({ ...current.options, frameMapping: Object.keys(valid).length ? JSON.stringify(valid) : '' });
        } catch (error) {
          uiRef.current?.setStatus(error instanceof Error ? error.message : 'Column mapping could not be saved.');
        }
      },
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
    uiRef.current?.setOptions({
      onSliceChange: canSlice ? (slice: string) => locationService.partial({ [`var-${sliceVariable}`]: slice }, true) : undefined,
    });
  }, [canSlice, sliceVariable, options.teiaUrl, storageKey]);

  useEffect(() => {
    if (options.teiaUrl || !uiRef.current) {
      return;
    }
    if (data.state === LoadingState.Error) {
      uiRef.current.setStatus(data.errors?.[0]?.message || 'the panel query failed');
      return;
    }
    let mapped;
    try {
      const mapping = parseMapping(options.frameMapping);
      uiRef.current.setOptions({ inputFrames: describeFrames(data.series), fieldMapping: mapping });
      mapped = applyMapping(data.series, mapping);
    } catch (error) {
      uiRef.current.setOptions({ inputFrames: describeFrames(data.series) });
      uiRef.current.setStatus(error instanceof Error ? error.message : 'Column mapping could not be applied.');
      return;
    }
    uiRef.current.setStatus('');
    const { snapshot, changes } = framesToSnapshot(mapped, ++roundRef.current);
    if (snapshot) {
      uiRef.current.update(snapshot, changes);
    } else {
      uiRef.current.setStatus(data.series.length ? 'No supported node/edge fields. Check the query mapping.' : 'No query data. Configure node and edge queries or a Teia URL.');
    }
  }, [data, options.teiaUrl, options.frameMapping, storageKey]);

  return <div ref={hostRef} style={{ width, height }} data-testid="teia-panel" />;
};

/** hasVariable says whether the dashboard defines the variable a slice is
 * written to; without it the panel only labels the slice its data holds. */
export function hasVariable(name: string): boolean {
  try {
    return getTemplateSrv().getVariables().some(v => v.name === name);
  } catch {
    return false;
  }
}

/** journeyOf reads the panel's journey after variables: an "All" choice, or
 * a variable left unresolved, means the whole map. */
export function journeyOf(value: string): string {
  const v = value.trim();
  return v === '*' || v === 'All' || v === '$__all' || v.startsWith('$') || v.startsWith('{') ? '' : v;
}
