import { DataFrame } from '@grafana/data';
import type { TeiaChange, TeiaEdge, TeiaFinding, TeiaMeasure, TeiaNode, TeiaReason, TeiaSnapshot, TeiaSource } from './teia-ui';

type Row = Record<string, unknown>;
type Role = 'nodes' | 'edges' | 'findings' | 'sources' | 'changes' | 'coverage';

const ROLES: Role[] = ['nodes', 'edges', 'findings', 'sources', 'changes', 'coverage'];
const HEALTH: Array<TeiaNode['health']> = ['healthy', 'degraded', 'failing', 'unknown'];
const VISIBILITY: Array<TeiaNode['visibility']> = ['full', 'partial', 'none'];
const STATES: Array<TeiaEdge['state']> = ['ok', 'ghost', 'stale'];
const SEVERITY: Array<TeiaFinding['severity']> = ['critical', 'warning', 'info'];

/** Rows of a frame as plain objects. Rows have no prototype, so a field
 * named __proto__ or constructor is just a field. */
export function rows(frame: DataFrame): Row[] {
  const length = frame.length ?? frame.fields[0]?.values.length ?? 0;
  const out: Row[] = [];
  for (let i = 0; i < length; i++) {
    const row: Row = Object.create(null);
    for (const f of frame.fields) {
      row[f.name] = f.values[i];
    }
    out.push(row);
  }
  return out;
}

/** What a frame holds: by refId or name first (the teia dashboards name
 * their queries), then by its fields, so plain Node Graph data (Tempo's
 * service graph, for one) works too. */
export function roleOf(frame: DataFrame): Role | undefined {
  for (const key of [frame.refId, frame.name]) {
    const k = (key ?? '').toLowerCase() as Role;
    if (ROLES.includes(k)) {
      return k;
    }
  }
  const names = new Set(frame.fields.map((f) => f.name));
  if (names.has('source') && names.has('target')) {
    return 'edges';
  }
  if (names.has('severity') && names.has('title')) {
    return 'findings';
  }
  if (names.has('visibility') && names.has('declared') && names.has('observed') && !names.has('id')) {
    return 'coverage';
  }
  if (names.has('id')) {
    return 'nodes';
  }
  return undefined;
}

const has = (r: Row, key: string): boolean => Object.hasOwn(r, key) && r[key] != null && r[key] !== '';
const str = (v: unknown): string => (v == null ? '' : String(v));
const num = (v: unknown): number => {
  const n = typeof v === 'number' ? v : parseFloat(str(v));
  return Number.isFinite(n) ? n : 0;
};
const bool = (v: unknown): boolean => v === true || v === 'true';
const list = (v: unknown): string[] =>
  str(v)
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
/** v when it is one of the allowed values, fallback otherwise. */
function oneOf<T extends string>(v: unknown, allowed: readonly T[], fallback: T): T {
  const s = str(v).toLowerCase() as T;
  return allowed.includes(s) ? s : fallback;
}

function health(errorRate: number, rate: number): TeiaNode['health'] {
  if (rate <= 0) {
    return 'unknown';
  }
  return errorRate >= 0.05 ? 'failing' : errorRate >= 0.01 ? 'degraded' : 'healthy';
}

function labelsOf(v: unknown): Record<string, string> {
  try {
    const parsed: unknown = typeof v === 'string' && v ? JSON.parse(v) : v;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {};
    }
    const out: Record<string, string> = {};
    for (const [k, val] of Object.entries(parsed)) {
      if (val != null && typeof val !== 'object') {
        out[k] = String(val);
      }
    }
    return out;
  } catch {
    return {};
  }
}

/** A list from one of teia's JSON columns (reasons, measures); the map
 * drops any item it cannot draw. */
function jsonList<T>(v: unknown): T[] {
  try {
    const parsed: unknown = typeof v === 'string' && v ? JSON.parse(v) : v;
    return Array.isArray(parsed) ? (parsed as T[]) : [];
  } catch {
    return [];
  }
}

function toNode(r: Row): TeiaNode {
  // Plain Node Graph frames (e.g. Tempo) carry a rate in secondarystat and
  // the failed share in arc__failed.
  const rate = has(r, 'requestRate') ? num(r.requestRate) : num(r.secondarystat);
  const errorRate = has(r, 'errorRate') ? num(r.errorRate) : num(r.arc__failed);
  return {
    id: str(r.id),
    kind: str(r.kind) || 'service',
    tech: str(r.tech) || undefined,
    visibility: oneOf(r.visibility, VISIBILITY, 'full'),
    health: oneOf(r.health, HEALTH, health(errorRate, rate)),
    requestRate: rate,
    errorRate,
    errorsUnknown: bool(r.errorsUnknown),
    latencyP50Ms: num(r.latencyP50Ms),
    latencyP95Ms: num(r.latencyP95Ms),
    reasons: jsonList<TeiaReason>(r.reasons),
    measures: jsonList<TeiaMeasure>(r.measures),
    sources: list(r.sources),
    addresses: list(r.addresses),
    labels: labelsOf(r.labels),
  };
}

/** Whether edge rows carry intent (declared flows). Plain Node Graph data
 * does not, and then there is no drift to show. */
const hasIntent = (r: Row): boolean => has(r, 'state') || has(r, 'declared');

function toEdge(r: Row, drift: boolean): TeiaEdge {
  const rate = has(r, 'requestRate') ? num(r.requestRate) : num(r.secondarystat);
  const errorRate = num(r.errorRate);
  const declared = has(r, 'declared') ? bool(r.declared) : false;
  const observed = has(r, 'observed') ? bool(r.observed) : true;
  const derived: TeiaEdge['state'] = !drift ? 'ok' : declared ? (observed ? 'ok' : 'stale') : 'ghost';
  return {
    from: str(r.source),
    to: str(r.target),
    state: oneOf(r.state, STATES, derived),
    health: oneOf(r.health, HEALTH, health(errorRate, rate)),
    protocol: str(r.protocol) || undefined,
    port: num(r.port) || undefined,
    reason: str(r.reason) || undefined,
    declared,
    observed,
    requestRate: rate,
    errorRate,
    errorsUnknown: bool(r.errorsUnknown),
    latencyP50Ms: num(r.latencyP50Ms),
    latencyP95Ms: num(r.latencyP95Ms),
    reasons: jsonList<TeiaReason>(r.reasons),
    measures: jsonList<TeiaMeasure>(r.measures),
    signals: list(r.signals),
    sources: list(r.sources),
  };
}

function toFinding(r: Row): TeiaFinding {
  const summary = str(r.nextStep);
  return {
    id: str(r.id) || `${str(r.rule)}:${str(r.subject)}`,
    rule: str(r.rule),
    kind: str(r.kind),
    severity: oneOf(r.severity, SEVERITY, 'info'),
    subject: str(r.subject),
    title: str(r.title),
    detail: str(r.detail) || undefined,
    fix: summary ? { summary, snippet: str(r.fixSnippet) || undefined, language: str(r.fixLanguage) || undefined } : undefined,
  };
}

function toSource(r: Row): TeiaSource {
  return { name: str(r.name), type: str(r.type), state: str(r.state), message: str(r.message), nodes: num(r.nodes), edges: num(r.edges) };
}

function toChange(r: Row): TeiaChange {
  const at = typeof r.at === 'number' ? new Date(r.at).toISOString() : str(r.at);
  return {
    id: str(r.id) || undefined,
    at,
    type: str(r.type),
    subject: str(r.subject),
    from: str(r.from),
    to: str(r.to),
    message: str(r.message),
  };
}

/** Coverage the way teia computes it, for data that did not bring its own. */
export function coverageOf(nodes: TeiaNode[], edges: TeiaEdge[]): TeiaSnapshot['coverage'] {
  const internal = nodes.filter((n) => n.kind !== 'external');
  const observed = edges.filter((e) => e.observed);
  const declared = edges.filter((e) => e.declared);
  const ratio = (a: number, b: number) => (b ? a / b : 0);
  return {
    nodes: nodes.length,
    edges: edges.length,
    visibility: ratio(internal.filter((n) => n.visibility === 'full').length, internal.length),
    declared: ratio(observed.filter((e) => e.declared).length, observed.length),
    observed: ratio(declared.filter((e) => e.observed).length, declared.length),
  };
}

/** Turns the panel's data frames into the snapshot the map renders. */
export function framesToSnapshot(series: DataFrame[], round: number): { snapshot?: TeiaSnapshot; changes: TeiaChange[] } {
  const byRole: Partial<Record<Role, Row[]>> = {};
  for (const frame of series) {
    const role = roleOf(frame);
    if (role) {
      byRole[role] = [...(byRole[role] ?? []), ...rows(frame)];
    }
  }
  if (!byRole.nodes && !byRole.edges) {
    return { changes: [] };
  }
  const edgeRows = byRole.edges ?? [];
  const drift = edgeRows.some(hasIntent);
  const edges = edgeRows.map((r) => toEdge(r, drift)).filter((e) => e.from && e.to && e.from !== e.to);
  const nodes: TeiaNode[] = [];
  const known = new Set<string>();
  for (const n of (byRole.nodes ?? []).map(toNode)) {
    if (n.id && !known.has(n.id)) {
      known.add(n.id);
      nodes.push(n);
    }
  }
  // Edges may name nodes the nodes query did not return.
  for (const e of edges) {
    for (const id of [e.from, e.to]) {
      if (!known.has(id)) {
        known.add(id);
        nodes.push({ id, kind: 'unknown', visibility: 'partial', health: 'unknown', sources: [] });
      }
    }
  }
  const cov = byRole.coverage?.[0];
  const coverage = cov
    ? { nodes: nodes.length, edges: edges.length, visibility: num(cov.visibility), declared: num(cov.declared), observed: num(cov.observed) }
    : coverageOf(nodes, edges);
  return {
    snapshot: {
      round,
      generatedAt: new Date().toISOString(),
      nodes,
      edges,
      findings: (byRole.findings ?? []).map(toFinding).filter((f) => f.title),
      sources: (byRole.sources ?? []).map(toSource).filter((s) => s.name),
      coverage,
      drift,
    },
    changes: (byRole.changes ?? []).map(toChange).filter((c) => c.message),
  };
}
