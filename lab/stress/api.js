// k6 load on teia's API while it discovers: what dashboards, the standalone
// page and agents ask for. TEIA is the base URL, CLUSTERS the number of
// clusters (c0..cN-1), SLICE a selector to read alongside the whole map.
import http from 'k6/http';
import { check } from 'k6';

const base = __ENV.TEIA || 'http://127.0.0.1:18480';
const clusters = Number(__ENV.CLUSTERS || 1);
const slice = __ENV.SLICE || '';

export const options = {
  scenarios: {
    api: { executor: 'constant-vus', vus: Number(__ENV.VUS || 20), duration: __ENV.DURATION || '30s' },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{kind:snapshot}': ['p(95)<2000'],
    'http_req_duration{kind:mcp}': ['p(95)<2000'],
  },
};

const mcp = (name, args) => http.post(`${base}/mcp`, JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } }), {
  headers: { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' },
  tags: { kind: 'mcp' },
});

export default function () {
  const cluster = `c${Math.floor(Math.random() * clusters)}`;
  const r = Math.random();
  let res;
  if (r < 0.4) {
    res = http.get(`${base}/api/v1/snapshot?cluster=${cluster}`, { tags: { kind: 'snapshot' } });
  } else if (r < 0.55 && slice) {
    res = http.get(`${base}/api/v1/snapshot?cluster=${cluster}&slice=${encodeURIComponent(slice)}`, { tags: { kind: 'slice' } });
  } else if (r < 0.65) {
    res = http.get(`${base}/api/v1/clusters`, { tags: { kind: 'clusters' } });
  } else if (r < 0.75) {
    res = http.get(`${base}/api/v1/grafana/edges?cluster=${cluster}`, { tags: { kind: 'grafana' } });
  } else if (r < 0.85) {
    res = mcp('teia_overview', { cluster });
  } else if (r < 0.9) {
    res = mcp('teia_map', { cluster, node: 'svc-0001' });
  } else if (r < 0.95) {
    res = mcp('teia_impact', { cluster, id: 'svc-0002', direction: 'upstream', depth: 3 });
  } else {
    res = mcp('teia_path', { cluster, from: 'svc-0001', to: 'svc-0042' });
  }
  if (!check(res, { ok: x => x.status === 200 })) console.error(`failed ${res.request.method} ${res.request.url}: status ${res.status} ${res.error}`);
}
