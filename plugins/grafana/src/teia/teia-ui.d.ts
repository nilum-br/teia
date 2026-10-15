// Types for teia-ui.js, which is synced from internal/api/ui before builds.

/** Why a node or edge is amber or red: a golden signal past its limit. */
export interface TeiaReason {
  cause: 'errors' | 'latency' | 'saturation' | 'backlog' | 'locks' | 'lag' | 'restarts' | 'evictions' | 'rejections';
  health: 'degraded' | 'failing';
  value: number;
  limit: number;
  summary: string;
  coverage?: number;
  approximate?: boolean;
  rule?: string;
  unit?: string;
  evidence?: TeiaEvidence[];
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

export interface TeiaEvidence {
  source: string;
  signal: string;
  collectedAt?: string;
  /** Query evaluation instant; never the raw sample's timestamp. */
  evaluatedAt?: string;
  /** Oldest input sample reported for this metric evaluation, including capacity. */
  oldestSampleAt?: string;
  origin?: string;
  observedAt?: string;
  /** Earliest start / latest end of sampled calls, not continuous coverage. */
  observedFrom?: string;
  /** Calls in the sample, not total traffic or a request rate. */
  sampleCount?: number;
  windowSeconds: number;
  retained?: boolean;
  metrics: string[];
}

export interface TeiaTimeRange { from: string; to: string; }

export interface TeiaEvidenceLink {
  label: string;
  url: string;
  kind: 'traces' | 'logs' | 'dashboard';
  description: string;
  periodFrom?: string;
  periodTo?: string;
}

export interface TeiaHealthRule {
  cause: TeiaReason['cause'];
  name: string;
  match: 'global' | 'identity' | 'selector';
  priority?: number;
  degraded: number;
  failing: number;
  unit: string;
}

export interface TeiaNode {
  links?: TeiaEvidenceLink[];
  healthRules?: TeiaHealthRule[];
  journeys?: string[];
  evidence?: TeiaEvidence[];
  id: string;
  kind: string;
  tech?: string;
  visibility: 'full' | 'partial' | 'none';
  health: 'healthy' | 'degraded' | 'failing' | 'unknown';
  requestRate?: number;
  errorRate?: number;
  /** Traffic was seen but no signal measured its failures. */
  errorsUnknown?: boolean;
  /** Fraction of this traffic with measured errors; errorRate describes that subset. */
  errorCoverage?: number;
  metricStatus?: Record<string, 'measured' | 'missing' | 'null' | 'invalid'>;
  /** Call durations in milliseconds, when a signal measures them. */
  latencyP50Ms?: number;
  latencyP95Ms?: number;
  latencyApproximate?: boolean;
  latencyCoverage?: number;
  trafficDirection?: string;
  reasons?: TeiaReason[];
  measures?: TeiaMeasure[];
  sources: string[];
  addresses?: string[];
  labels?: Record<string, string>;
}

export interface TeiaEdge {
  expectedFrequencySeconds?: number;
  frequency?: { status: 'unknown' | 'not-due' | 'overdue'; lastObservedAt?: string; nextExpectedAt?: string; detail: string };
  links?: TeiaEvidenceLink[];
  healthRules?: TeiaHealthRule[];
  evidence?: TeiaEvidence[];
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
  errorsUnknown?: boolean;
  errorCoverage?: number;
  metricStatus?: Record<string, 'measured' | 'missing' | 'null' | 'invalid'>;
  latencyP50Ms?: number;
  latencyP95Ms?: number;
  latencyApproximate?: boolean;
  latencyCoverage?: number;
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
  lastSuccessAt?: string;
  retained?: boolean;
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
  journeys?: TeiaJourneyContract;
  timeMode?: 'current' | 'query' | 'mixed' | 'historical';
  history?: TeiaHistory;
  period?: TeiaTimeRange;
  observedAt?: string;
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

export interface TeiaHistory {
  id: string;
  recordedAt: string;
  availability: 'stored-snapshot';
  nodes: Record<string, TeiaPresence>;
  edges: Record<string, TeiaPresence>;
}
export interface TeiaPresence {
  episode: string;
  identity: string;
  identityConfidence: string;
  firstSeen: string;
  lastSeen: string;
  firstObservedAt?: string;
  lastObservedAt?: string;
}

/** Ownership topology only; never request or transaction correlation. */
export interface TeiaJourneyContract {
  version: 1;
  mode: 'ownership';
  key: string;
  maxDepth: number;
  maxNodes: number;
  scopes: TeiaJourneyScope[];
}

export interface TeiaJourneyScope {
  name: string;
  key: string;
  roles: Record<string, 'member' | 'dependency'>;
  entryPoints: string[];
  entryEdges: string[];
  boundaryPorts: Array<{ kind: 'caller' | 'tenant' | 'exit'; edgeId: string; from: string; to: string }>;
  health: TeiaNode['health'];
  truncated: boolean;
}

export interface TeiaMountOptions {
  requestedRange?: TeiaTimeRange;
  mode?: 'page' | 'panel';
  api?: string;
  stream?: boolean;
  storageKey?: string;
  theme?: 'dark' | 'light';
  groupBy?: string;
  flow?: boolean;
  /** The journey shown: a journey label value, key=value, or from:node. */
  journey?: string;
  /** The label naming journeys; empty detects journey, app.kubernetes.io/part-of or system. */
  journeyKey?: string;
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
  setOptions(options: { groupBy?: string; flow?: boolean; journey?: string; journeyKey?: string; values?: boolean; requestedRange?: TeiaTimeRange }): void;
  setTheme(theme: 'dark' | 'light'): void;
  destroy(): void;
}

declare const TeiaUI: { mount(host: HTMLElement, options?: TeiaMountOptions): TeiaController; core: Record<string, unknown> };
export default TeiaUI;
