#!/usr/bin/env bash
# One capacity run: N clusters of synthetic telemetry read by one teia.
#   lab/stress/run.sh CLUSTERS SERVICES FANOUT JOURNEYS TENANTS [OUT_DIR]
# Prints one JSON line: round time per cluster, memory, CPU, snapshot size and
# the k6 latencies of the API, slices and MCP under load.
# Needs: go, curl, k6. Ports 19090 (synth), 18480 (teia) and 16060 (pprof).
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.."
clusters=$1 services=$2 fanout=$3 journeys=$4 tenants=$5
out=${6:-$(mktemp -d)}
mkdir -p "$out/bin"
go build -o "$out/bin/synth" ./lab/stress/synth
go build -o "$out/bin/teiaprof" ./lab/stress/teiaprof
interval=${INTERVAL:-30s}

dims=""
if (( journeys > 1 || tenants > 1 )); then
  dims=$'\n          dimensions: {journey: journey, tenant: tenant}'
fi
config="$out/teia.yaml"
{
  echo "listen: 127.0.0.1:18480"
  echo "interval: $interval"
  echo "timeout: ${TIMEOUT:-25s}"
  echo "clusters:"
  for ((i = 0; i < clusters; i++)); do
    cat <<YAML
  - name: c$i
    sources:
      queries:
        - name: calls
          url: http://127.0.0.1:19090/c/$i
          query: sum by (source, target, journey, tenant) (rate(calls_total[\$window]))
          errors: sum by (source, target, journey, tenant) (rate(calls_total{status="error"}[\$window]))
          latency: histogram_quantile(0.95, sum by (source, target, journey, tenant, le) (rate(call_seconds_bucket[\$window])))
          from: source
          to: target$dims
      tempo:
        - url: http://127.0.0.1:19090/c/$i/tempo
          maxTraces: ${TRACES:-100}
          dimensions: {journey: business.journey}
YAML
  done
} > "$config"

"$out/bin/synth" -clusters "$clusters" -services "$services" -fanout "$fanout" -journeys "$journeys" -tenants "$tenants" 2> "$out/synth.log" &
synth=$!
"$out/bin/teiaprof" -config "$config" -pprof 127.0.0.1:16060 2> "$out/teia.log" &
teia=$!
trap 'kill $synth $teia 2>/dev/null || true' EXIT

# Wait until every cluster finished two rounds.
started=$(date +%s)
until rounds=$(curl -sf http://127.0.0.1:18480/api/v1/clusters | jq '[.clusters[].round] | min') && (( rounds >= 2 )); do
  sleep 2
  if (( $(date +%s) - started > 900 )); then echo '{"error":"no second round in 15 minutes"}'; exit 1; fi
done
warm=$(( $(date +%s) - started ))

# Steady state over one minute (rounds keep running): peak memory and CPU.
cpu() { ps -o time= -p "$teia" | awk -F'[:.]' '{ if (NF==4) print $1*3600+$2*60+$3; else print $1*60+$2 }'; }
rss=0; c0=$(cpu); t0=$(date +%s)
for _ in $(seq 1 30); do
  r=$(ps -o rss= -p "$teia" | tr -d ' '); (( r > rss )) && rss=$r
  sleep 2
done
cores=$(echo "($(cpu) - $c0) / ($(date +%s) - $t0)" | bc -l)

# Discovery time per cluster: its slowest source.
curl -sf "http://127.0.0.1:18480/api/v1/snapshot?cluster=c0" > "$out/snapshot-c0.json"
nodes=$(jq '.nodes | length' "$out/snapshot-c0.json"); edges=$(jq '.edges | length' "$out/snapshot-c0.json")
snapshot_bytes=$(wc -c < "$out/snapshot-c0.json" | tr -d ' ')
metrics_bytes=$(curl -sf http://127.0.0.1:18480/metrics | wc -c | tr -d ' ')
round_ms=$(for ((i = 0; i < clusters; i++)); do curl -sf "http://127.0.0.1:18480/api/v1/sources?cluster=c$i" | jq '[.[].duration] | max / 1e6'; done | sort -n | awk '{a[NR]=$1} END {printf "{\"p50\":%.0f,\"max\":%.0f}", a[int((NR+1)/2)], a[NR]}')

# The API, slices and MCP under load while rounds keep running.
k6 run --quiet --summary-export "$out/k6.json" -e TEIA=http://127.0.0.1:18480 -e CLUSTERS="$clusters" \
  -e SLICE="$([[ -n $dims ]] && echo journey:j1)" -e VUS="${VUS:-20}" -e DURATION="${DURATION:-30s}" lab/stress/api.js > "$out/k6.log" 2>&1 || true
curl -sf "http://127.0.0.1:16060/debug/pprof/heap?gc=1" -o "$out/heap.pprof" || true

jq -nc --argjson clusters "$clusters" --argjson services "$services" --argjson fanout "$fanout" \
  --argjson journeys "$journeys" --argjson tenants "$tenants" --argjson nodes "$nodes" --argjson edges "$edges" \
  --argjson warm "$warm" --argjson rss "$rss" --argjson cores "$cores" --argjson round "$round_ms" \
  --argjson snap "$snapshot_bytes" --argjson metrics "$metrics_bytes" --slurpfile k6 "$out/k6.json" '{
    clusters: $clusters, servicesPerCluster: $services, edgesPerCluster: $edges, nodesPerCluster: $nodes,
    seriesPerQuery: ($edges * $journeys * $tenants), totalSeriesPerRound: ($clusters * $edges * $journeys * $tenants * 3),
    secondsToSecondRound: $warm, roundMsPerCluster: $round, rssMiB: ($rss / 1024 | floor), cpuCores: ($cores * 100 | round / 100),
    snapshotKiB: ($snap / 1024 | floor), metricsKiB: ($metrics / 1024 | floor),
    k6: {
      rps: ($k6[0].metrics.http_reqs.rate | floor),
      failed: $k6[0].metrics.http_req_failed.value,
      p95ms: { all: ($k6[0].metrics.http_req_duration["p(95)"] | floor),
               snapshot: ($k6[0].metrics["http_req_duration{kind:snapshot}"]["p(95)"] // null | if . then floor else . end),
               mcp: ($k6[0].metrics["http_req_duration{kind:mcp}"]["p(95)"] // null | if . then floor else . end) }
    }
  }'
