import type { DataFrame, Field } from '@grafana/data';

export type FrameMapping = { nodes?: Record<string, string>; edges?: Record<string, string> };
export interface InputFrame { key: string; fields: string[]; }

const common = ['health', 'requestRate', 'errorRate', 'errorsUnknown', 'errorCoverage', 'latencyP50Ms', 'latencyP95Ms',
  'latencyCoverage', 'latencyApproximate', 'reasons', 'measures', 'sources', 'evidence', 'links', 'healthRules',
  'timeMode', 'snapshotAt', 'observedAt', 'periodFrom', 'periodTo', 'history', 'historyId'];
const allowed = {
  nodes: new Set(['id', 'kind', 'tech', 'visibility', 'labels', 'addresses', 'journeys', 'journeyContract', 'trafficDirection', ...common]),
  edges: new Set(['id', 'source', 'target', 'state', 'declared', 'observed', 'protocol', 'port', 'reason', 'signals',
    'expectedFrequencySeconds', 'frequency', ...common]),
};

function object(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

/** Configuration contains column names only: no expressions, units or row values. */
export function parseMapping(raw: string | undefined): FrameMapping {
  if (!raw?.trim()) { return {}; }
  let parsed: unknown;
  try { parsed = JSON.parse(raw); } catch { throw new Error('Column mapping is not valid JSON. Use the setup form or correct the panel mapping option.'); }
  return validateMapping(parsed);
}

export function validateMapping(value: unknown): FrameMapping {
  if (!object(value) || Object.keys(value).some(key => key !== 'nodes' && key !== 'edges')) {
    throw new Error('Column mapping must be an object with optional nodes and edges sections.');
  }
  const result: FrameMapping = {};
  for (const role of ['nodes', 'edges'] as const) {
    if (!Object.hasOwn(value, role)) { continue; }
    const input = value[role];
    if (!object(input)) { throw new Error(`The ${role} mapping must contain a frame and its column names.`); }
    const mapping: Record<string, string> = {};
    for (const [key, column] of Object.entries(input)) {
      if (key !== 'frame' && !allowed[role].has(key)) { throw new Error(`Unsupported ${role} mapping field: ${key}.`); }
      if (typeof column !== 'string') { throw new Error(`The ${role}.${key} mapping must be a column name.`); }
      if (column) { mapping[key] = column; }
    }
    for (const required of role === 'nodes' ? ['frame', 'id'] : ['frame', 'source', 'target']) {
      if (!mapping[required]?.trim()) { throw new Error(`Choose ${role}.${required} in the column mapping setup.`); }
    }
    result[role] = mapping;
  }
  return result;
}

/** Frame order never forms part of its identity. Ambiguous keys are rejected
 * when selected instead of silently switching to another series on refresh. */
export function frameKey(frame: DataFrame): string {
  const parts: string[] = [];
  if (frame.refId) { parts.push('ref:' + encodeURIComponent(frame.refId)); }
  if (frame.name) { parts.push('name:' + encodeURIComponent(frame.name)); }
  return parts.join('/') || 'unnamed';
}

export function describeFrames(frames: DataFrame[]): InputFrame[] {
  return frames.map(frame => ({ key: frameKey(frame), fields: frame.fields.map(field => field.name) }));
}

function checkUnit(field: Field, canonical: string) {
  const unit = field.config?.unit;
  if (!unit || unit === 'none' || unit === 'short') { return; }
  const required = canonical === 'latencyP50Ms' || canonical === 'latencyP95Ms' ? 'ms'
    : ['errorRate', 'errorCoverage', 'latencyCoverage'].includes(canonical) ? 'percentunit'
    : canonical === 'requestRate' ? 'reqps' : undefined;
  if (required && unit !== required) {
    const meaning = required === 'ms' ? 'milliseconds' : required === 'percentunit' ? 'a fraction from 0 to 1' : 'requests per second';
    throw new Error(`Column ${field.name} has unit ${unit}, but ${canonical} requires ${meaning}. Convert it in the query before mapping; no unit conversion is inferred.`);
  }
}

/** Add aliases without mutating original frames, values, labels or metadata.
 * Existing canonical columns cannot be overwritten by a conflicting alias. */
export function applyMapping(series: DataFrame[], supplied: FrameMapping): DataFrame[] {
  const mapping = validateMapping(supplied);
  const consumed = new Set<DataFrame>();
  const mapped: DataFrame[] = [];
  for (const role of ['nodes', 'edges'] as const) {
    const choice = mapping[role];
    if (!choice) { continue; }
    if (choice.frame === 'unnamed') { throw new Error('This frame has no refId or name. Give the query a stable identity before mapping it.'); }
    const matches = series.filter(frame => frameKey(frame) === choice.frame);
    if (matches.length !== 1) {
      throw new Error(matches.length ? `Frame ${choice.frame} is ambiguous. Use unique query refIds or frame names.`
        : `Frame ${choice.frame} is unavailable. Refresh the query and choose its frame again.`);
    }
    const frame = matches[0];
    const names = new Set<string>();
    for (const field of frame.fields) {
      if (names.has(field.name)) { throw new Error(`Frame ${choice.frame} has duplicate column ${field.name}. Rename duplicate columns in the query.`); }
      names.add(field.name);
    }
    const fields = [...frame.fields];
    for (const [canonical, name] of Object.entries(choice)) {
      if (canonical === 'frame') { continue; }
      const source = frame.fields.find(field => field.name === name);
      if (!source) { throw new Error(`Column ${name} is missing from ${choice.frame}. Update the ${role}.${canonical} mapping.`); }
      checkUnit(source, canonical);
      if (canonical === name) { continue; }
      if (names.has(canonical)) {
        throw new Error(`Frame ${choice.frame} already has ${canonical}. Mapping ${name} onto it would replace existing data; rename the conflicting column in the query.`);
      }
      fields.push({ ...source, name: canonical });
    }
    mapped.push({ ...frame, name: role, refId: role, fields });
    consumed.add(frame);
  }
  return [...series.filter(frame => !consumed.has(frame)), ...mapped];
}
