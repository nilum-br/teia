// Types for teia-ui.js, which is synced from internal/api/ui before builds.

/** Why a node or edge is amber or red: a golden signal past its limit. */
export interface TeiaReason {
  cause: 'errors' | 'latency' | 'saturation' | 'backlog' | 'locks';
  health: 'degraded' | 'failing';
  value: number;
  limit: number;
  summary: string;
  /** The measure behind a saturation, backlog or locks reason. */
  what?: string;
  /** The dependency it most likely comes from, or who holds the lock. */
  via?: string;
}

/** A reading beyond traffic: connections in use, sessions waiting on locks. */
export interface TeiaMeasure {
  cause: TeiaReason['cause'];
  what: string;
  value: number;
  max?: number;
  via?: string;
}

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
  reasons?: TeiaReason[];
  measures?: TeiaMeasure[];
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
  reasons?: TeiaReason[];
  measures?: TeiaMeasure[];
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
  /** The journey shown: a journey label value, key=value, or from:node. */
  journey?: string;
  /** Traffic and latency on every line (default true). */
  values?: boolean;
  /** Node to select once the first snapshot is in. */
  select?: string;
}

export interface TeiaController {
  update(snapshot: TeiaSnapshot, changes?: TeiaChange[]): void;
  /** Reports the host's own query state; an empty message clears it. */
  setStatus(error?: string): void;
  /** Applies panel options, unless the viewer already chose otherwise on the map. */
  setOptions(options: { groupBy?: string; flow?: boolean; journey?: string; values?: boolean }): void;
  setTheme(theme: 'dark' | 'light'): void;
  destroy(): void;
}

declare const TeiaUI: { mount(host: HTMLElement, options?: TeiaMountOptions): TeiaController; core: Record<string, unknown> };
export default TeiaUI;
