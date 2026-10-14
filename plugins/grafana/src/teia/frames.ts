import { DataFrame } from '@grafana/data';
import type { TeiaChange, TeiaEdge, TeiaFinding, TeiaNode, TeiaSnapshot, TeiaSource } from './teia-ui';

type Row = Record<string, unknown>;
type Role = 'nodes' | 'edges' | 'findings' | 'sources' | 'changes' | 'coverage';

const ROLES: Role[] = ['nodes', 'edges', 'findings', 'sources', 'changes', 'coverage'];

/** Rows of a frame as plain objects. */
export function rows(frame: DataFrame): Row[] {
  const length = frame.length ?? frame.fields[0]?.values.length ?? 0;
  const out: Row[] = [];
  for (let i = 0; i < length; i++) {
    const row: Row = {};
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

function health(errorRate: number, rate: number): TeiaNode['health'] {
  if (rate <= 0) {
    return 'unknown';
  }
  return errorRate >= 0.05 ? 'failing' : errorRate >= 0.01 ? 'degraded' : 'healthy';
}

function toNode(r: Row): TeiaNode {
  let labels: Record<string, string> = {};
  try {
    labels = r.labels ? JSON.parse(str(r.labels)) : {};
  } catch {
    labels = {};
  }
  // Plain Node Graph frames (e.g. Tempo) carry arc__failed and a rate.
  const rate = r.requestRate != null ? num(r.requestRate) : num(r.secondarystat);
  const errorRate = r.errorRate != null ? num(r.errorRate) : num(r.arc__failed);
  return {
    id: str(r.id),
    kind: str(r.kind) || 'service',
    tech: str(r.tech) || undefined,
    visibility: (str(r.visibility) || 'full') as TeiaNode['visibility'],
    health: (str(r.health) || health(errorRate, rate)) as TeiaNode['health'],
    requestRate: rate,
    errorRate,
    sources: list(r.sources),
    addresses: list(r.addresses),
    labels,
  };
}

function toEdge(r: Row): TeiaEdge {
  const rate = num(r.requestRate);
  const errorRate = num(r.errorRate);
  const declared = r.declared != null ? bool(r.declared) : false;
  const observed = r.observed != null ? bool(r.observed) : true;
  return {
    from: str(r.source),
    to: str(r.target),
    state: (str(r.state) || (declared ? (observed ? 'ok' : 'stale') : 'ghost')) as TeiaEdge['state'],
    health: (str(r.health) || health(errorRate, rate)) as TeiaEdge['health'],
    protocol: str(r.protocol) || undefined,
    port: num(r.port) || undefined,
    reason: str(r.reason) || undefined,
    declared,
    observed,
    requestRate: rate,
    errorRate,
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
    severity: (str(r.severity) || 'info') as TeiaFinding['severity'],
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
  return { at, type: str(r.type), subject: str(r.subject), from: str(r.from), to: str(r.to), message: str(r.message) };
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
  const edges = (byRole.edges ?? []).map(toEdge).filter((e) => e.from && e.to);
  const nodes = (byRole.nodes ?? []).map(toNode).filter((n) => n.id);
  // Edges may name nodes the nodes query did not return.
  const known = new Set(nodes.map((n) => n.id));
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
      findings: (byRole.findings ?? []).map(toFinding),
      sources: (byRole.sources ?? []).map(toSource),
      coverage,
    },
    changes: (byRole.changes ?? []).map(toChange),
  };
}
