// Types for teia-ui.js, which is synced from internal/api/ui before builds.

export interface TeiaNode {
  id: string;
  kind: string;
  tech?: string;
  visibility: 'full' | 'partial' | 'none';
  health: 'healthy' | 'degraded' | 'failing' | 'unknown';
  requestRate?: number;
  errorRate?: number;
  /** Call durations in milliseconds, when a signal measures them. */
  latencyP50Ms?: number;
  latencyP95Ms?: number;
  sources: string[];
  addresses?: string[];
  labels?: Record<string, string>;
}

export interface TeiaEdge {
  from: string;
  to: string;
  state: 'ok' | 'ghost' | 'stale';
  health: 'healthy' | 'degraded' | 'failing' | 'unknown';
  protocol?: string;
  port?: number;
  reason?: string;
  declared: boolean;
  observed: boolean;
  requestRate?: number;
  errorRate?: number;
  latencyP50Ms?: number;
  latencyP95Ms?: number;
  signals: string[];
  sources: string[];
}

export interface TeiaFinding {
  id: string;
  rule: string;
  kind: string;
  severity: 'critical' | 'warning' | 'info';
  subject: string;
  title: string;
  detail?: string;
  fix?: { summary: string; snippet?: string; language?: string };
}

export interface TeiaSource {
  name: string;
  type: string;
  state: string;
  message?: string;
  nodes: number;
  edges: number;
}

export interface TeiaChange {
  id?: string;
  at: string;
  type: string;
  subject: string;
  from?: string;
  to?: string;
  message: string;
}

export interface TeiaSnapshot {
  round: number;
  generatedAt: string;
  nodes: TeiaNode[];
  edges: TeiaEdge[];
  findings: TeiaFinding[];
  sources: TeiaSource[];
  coverage: { nodes: number; edges: number; visibility: number; declared: number; observed: number };
  /** False for data without intent (plain Node Graph frames): no drift to show. */
  drift?: boolean;
  changes?: TeiaChange[];
}

export interface TeiaMountOptions {
  mode?: 'page' | 'panel';
  api?: string;
  stream?: boolean;
  storageKey?: string;
  theme?: 'dark' | 'light';
  groupBy?: string;
  flow?: boolean;
  /** Node to select once the first snapshot is in. */
  select?: string;
}

export interface TeiaController {
  update(snapshot: TeiaSnapshot, changes?: TeiaChange[]): void;
  /** Reports the host's own query state; an empty message clears it. */
  setStatus(error?: string): void;
  /** Applies panel options, unless the viewer already chose otherwise on the map. */
  setOptions(options: { groupBy?: string; flow?: boolean }): void;
  setTheme(theme: 'dark' | 'light'): void;
  destroy(): void;
}

declare const TeiaUI: { mount(host: HTMLElement, options?: TeiaMountOptions): TeiaController; core: Record<string, unknown> };
export default TeiaUI;
