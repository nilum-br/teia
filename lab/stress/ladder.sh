#!/usr/bin/env bash
# Runs a set of capacity scenarios and prints a table to compare with
# docs/capacity.md.
#   lab/stress/ladder.sh            quick: two scenarios, about 6 minutes
#   lab/stress/ladder.sh full       the table in docs/capacity.md, about 25 minutes
#   lab/stress/ladder.sh "C S F J T" ...   your own scenarios: clusters, services,
#                                   calls per service, journeys, tenants
# Results stay in OUT (default: a new directory under $TMPDIR), one folder per
# scenario with the config, logs, k6 summary and heap profile.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.."

case "${1:-quick}" in
  quick) scenarios=("1 100 3 1 1" "10 500 4 2 2") ;;
  full) scenarios=("1 100 3 1 1" "10 100 3 1 1" "10 500 4 2 2" "30 500 4 2 2" "100 300 3 1 1" "50 1000 4 1 1") ;;
  *) scenarios=("$@") ;;
esac
out=${OUT:-$(mktemp -d "${TMPDIR:-/tmp}/teia-stress-XXXXXX")}
mkdir -p "$out"
echo "Results in $out" >&2

for scenario in "${scenarios[@]}"; do
  read -r c s f j t <<< "$scenario"
  echo "Running $c clusters × $s services × $f calls × $j journeys × $t tenants…" >&2
  if ! lab/stress/run.sh "$c" "$s" "$f" "$j" "$t" "$out/$c-$s-$f-$j-$t" 2> "$out/$c-$s-$f-$j-$t.log" | tail -1 >> "$out/results.jsonl"; then
    echo "  failed: see $out/$c-$s-$f-$j-$t.log" >&2
  fi
done

echo
echo "| Clusters × connections | Series per round | Memory (RSS) | Round per cluster (max) | Snapshot | API under load | Failed |"
echo "|---|---|---|---|---|---|---|"
jq -r 'select(.clusters) | "| \(.clusters) × \(.edgesPerCluster) | \(.totalSeriesPerRound) | \(.rssMiB) MiB | \(.roundMsPerCluster.max) ms | \(.snapshotKiB / 1024 * 10 | floor / 10) MB | \(.k6.rps) req/s, snapshot p95 \(.k6.p95ms.snapshot) ms, MCP p95 \(.k6.p95ms.mcp) ms | \(.k6.failed * 100 | . * 1000 | floor / 1000)% |"' "$out/results.jsonl"
