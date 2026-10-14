// Types for teia-ui.js, which is synced from internal/api/ui before builds.

export interface TeiaNode {
  id: string;
  kind: string;
  tech?: string;
  visibility: 'full' | 'partial' | 'none';
  health: 'healthy' | 'degraded' | 'failing' | 'unknown';
  requestRate?: number;
  errorRate?: number;
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
}

export interface TeiaController {
  update(snapshot: TeiaSnapshot, changes?: TeiaChange[]): void;
  setTheme(theme: 'dark' | 'light'): void;
  destroy(): void;
}

declare const TeiaUI: { mount(host: HTMLElement, options?: TeiaMountOptions): TeiaController };
export default TeiaUI;
