import type { TeiaEdge, TeiaJourneyContract, TeiaJourneyScope, TeiaNode } from './teia-ui';

const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const strings = (v: unknown): v is string[] => Array.isArray(v) && v.every((item) => typeof item === 'string');

/** Keep the backend scope only when its complete contract fits these frames.
 * Generic Node Graph data and invalid/partial contracts use the UI fallback. */
export function journeyContract(value: unknown, nodes: TeiaNode[], edges: TeiaEdge[]): TeiaJourneyContract | undefined {
  try {
    const parsed: unknown = typeof value === 'string' ? JSON.parse(value) : value;
    if (!object(parsed) || parsed.version !== 1 || parsed.mode !== 'ownership' || typeof parsed.key !== 'string' ||
      !Number.isInteger(parsed.maxDepth) || Number(parsed.maxDepth) < 0 || !Number.isInteger(parsed.maxNodes) || Number(parsed.maxNodes) < 1 || !Array.isArray(parsed.scopes)) {
      return undefined;
    }
    const ids = new Set(nodes.map((n) => n.id));
    const edgeIDs = new Set(edges.map((e) => `${e.from}->${e.to}`));
    const scopes: TeiaJourneyScope[] = [];
    for (const scope of parsed.scopes) {
      if (!object(scope) || typeof scope.name !== 'string' || scope.key !== parsed.key || !object(scope.roles) ||
        !strings(scope.entryPoints) || !scope.entryPoints.every((id) => ids.has(id)) ||
        !strings(scope.entryEdges) || !scope.entryEdges.every((id) => edgeIDs.has(id)) ||
        typeof scope.truncated !== 'boolean' || !['healthy', 'degraded', 'failing', 'unknown'].includes(String(scope.health)) ||
        !Array.isArray(scope.boundaryPorts)) {
        return undefined;
      }
      const roles: TeiaJourneyScope['roles'] = Object.create(null);
      for (const [id, role] of Object.entries(scope.roles)) {
        if (!ids.has(id) || (role !== 'member' && role !== 'dependency')) { return undefined; }
        roles[id] = role;
      }
      const boundaryPorts: TeiaJourneyScope['boundaryPorts'] = [];
      for (const boundary of scope.boundaryPorts) {
        if (!object(boundary) || (boundary.kind !== 'caller' && boundary.kind !== 'tenant' && boundary.kind !== 'exit') ||
          typeof boundary.from !== 'string' || typeof boundary.to !== 'string' || !ids.has(boundary.from) || !ids.has(boundary.to) ||
          boundary.edgeId !== `${boundary.from}->${boundary.to}` || !edgeIDs.has(boundary.edgeId) ||
          !!roles[boundary.from] === !!roles[boundary.to]) { return undefined; }
        boundaryPorts.push({ kind: boundary.kind, edgeId: boundary.edgeId, from: boundary.from, to: boundary.to });
      }
      scopes.push({ name: scope.name, key: scope.key, roles, entryPoints: scope.entryPoints, entryEdges: scope.entryEdges,
        boundaryPorts, health: scope.health as TeiaNode['health'], truncated: scope.truncated });
    }
    return { version: 1, mode: 'ownership', key: parsed.key, maxDepth: Number(parsed.maxDepth), maxNodes: Number(parsed.maxNodes), scopes };
  } catch {
    return undefined;
  }
}
