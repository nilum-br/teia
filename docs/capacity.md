# Capacity

How large a map one teia holds, and what to change when it grows. Measured with
[lab/stress](../lab/stress/README.md): synthetic Prometheus and Tempo APIs for
many clusters, one teia reading all of them through [`clusters`](configuration.md#clusters),
and k6 reading the API and MCP while rounds run. Each cluster has a query source
(three PromQL queries per round) and a Tempo source (100 sampled traces per round);
a "connection" is one caller→callee pair, each with one series per journey and tenant
combination when the queries carry those dimensions. One process, 30 s interval,
Apple M-series laptop.

## Run it yourself

```sh
make stress        # two scenarios, about 6 minutes
make stress-full   # every row below, about 25 minutes
```

Needs Go, curl, jq, bc and k6; no Docker or cluster. Both print a table in the format below.
[lab/stress](../lab/stress/README.md) explains the options and how to read a run.

## Results

| Clusters × connections | Series per round | Memory (RSS) | Round per cluster (max) | Snapshot | API under load (20 VUs) |
|---|---|---|---|---|---|
| 1 × 300 | 900 | 48 MiB | 18 ms | 1.5 MB | 6210 req/s, snapshot p95 4 ms |
| 10 × 300 | 9 000 | 126 MiB | 29 ms | 1.5 MB | 6448 req/s, snapshot p95 4 ms |
| 10 × 2 000 (2 journeys × 2 tenants) | 240 000 | 583 MiB | 76 ms | 8.8 MB | 652 req/s, snapshot p95 38 ms |
| 30 × 2 000 (2 journeys × 2 tenants) | 720 000 | 1.5–1.6 GiB | 76–91 ms | 8.8 MB | 576–606 req/s, snapshot p95 44 ms, MCP p95 53 ms |
| 100 × 900 | 270 000 | 2.1 GiB | 29 ms | 4.2 MB | 2105 req/s, snapshot p95 19 ms |
| 50 × 4 000 | 600 000 | 3.0 GiB | 47 ms | 16.5 MB | 180 req/s, snapshot p95 210 ms |

Two runs had requests end with `unexpected EOF` in the first second of the load: 4 of 17 198 at
30 × 2 000, and 1 of 18 197 at 10 × 2 000, a whole `/api/v1/snapshot` (the cached, gzip-capable
response). Other runs with the same load (about 41 000 requests), and 15 runs reading only snapshots
(about 53 000), had none; teia logged nothing and no handler panicked. The cause was not found; `api.js`
prints the method and URL of any failed request so the next occurrence can be traced. Every other run had no failed request. The MCP load includes `teia_impact` and
`teia_path`, which walk the graph. CPU averaged under 0.3 core between requests:
rounds spend their time waiting for the backends, which the synthetic APIs answer at
once, so real rounds take as long as your Prometheus and Tempo do.

## Sizing

- **Memory grows with connections, not clusters:** about 15–25 KiB per connection
  (more with business dimensions, which keep one slice per combination), plus about
  9 KiB per connection of encoded snapshot kept for the API and streams. Plan
  `resources.limits.memory` at twice the steady state: a round builds the next map
  while the last one is served. For the chart's 512 MiB default, stay under about
  15 000 connections in all.
- **Rounds:** each cluster runs its own, offset across the interval, so the backends
  and teia see `clusters ÷ interval` rounds per second rather than every cluster at
  once. Keep the interval above your slowest cluster's round (`/api/v1/sources`
  reports each source's `duration`). A source that does not answer within `timeout`
  is reported, and its last good observation is kept for `retention`.
- **Backends:** per cluster and round, each query source runs its query, errors
  and latency queries; each Tempo source one tag-values call, one search per sampled
  service (four at a time) and one fetch per trace (`maxTraces`), within the source's
  `timeout`. With 50 clusters every 30 s that is about 5 PromQL queries and 170 Tempo
  requests per second in all. On a backend that keeps every cluster, see
  [A central backend](#a-central-backend).
- **The API:** the whole snapshot is served as the round encoded it, gzipped once
  per round. Grafana frames, slices, the graph, operations, journeys, findings and
  coverage are encoded and gzipped once per round for clients that take gzip
  (Grafana's backend does), however many dashboards ask; MCP answers are computed
  per request. Dashboards should read one cluster (`?cluster=`) and a slice only
  when they need one. Under load, memory goes up to about 2.5 times the steady
  state (sliced snapshots and per-request answers): size the limit for that.
- **Go's memory limit** is set to 90% of the container's (unless `GOMEMLIMIT` is
  set), so near the limit the collector works harder instead of the pod being killed.
- **`/metrics`** has one series per component and connection: about 8 MB with
  30 × 2 000. Scrape `/metrics?cluster=name` per cluster, or drop
  `teia_edge_info`/`teia_node_info` at scrape time if you only alert on health and
  sources.

## A central stack

The runs above answer from synthetic APIs at once and leave history off. A central Grafana stack adds slow
backends, a backend shared by every cluster, inventories, history and many viewers. These runs measure each, with
the extensions of [lab/stress](../lab/stress/README.md) (`CENTRAL`, `PROM_LATENCY`, `SEARCH_LATENCY`,
`TRACE_LATENCY`) and others kept out of the repository: a real Prometheus, a Kubernetes API answering realistic pod
lists, simulated viewers (streams and Grafana dashboards), a real Grafana 13.2 with Infinity 4.1, and Chromium.
Machine: an x86-64 Linux VM with 16 vCPUs (Docker Desktop), slower than the laptop above: compare rows with each
other, not with the table above.

### A central backend

One Mimir (or Thanos, VictoriaMetrics) keeping every cluster: without a [`selector`](configuration.md#shared-backends),
each cluster's source reads every cluster's series as its own. With 10 clusters of the same services, each map showed
9 times its real traffic. With `selector: {cluster: …}`, each query reads one cluster: one tenth of the series, and
rounds of 18–30 ms instead of 155–343 ms.

On a real Prometheus 3.15 holding 10 clusters × 2 000 connections (300 000 series), one query per cluster cost the
same in all as one query grouped by cluster, and each was a tenth of it:

| Query | 10 scoped queries, in all (slowest) | 1 query grouped by cluster |
|---|---|---|
| requests: `sum by (source, target) (rate(…[5m]))` | 0.17 s (0.02 s) | 0.15 s |
| errors | 0.12 s (0.02 s) | 0.09 s |
| p95: `histogram_quantile(0.95, …)` | 0.59 s (0.07 s) | 0.65 s |

Small queries stay under per-query limits (Mimir's fetched-chunk and sample limits, query timeouts) that a grouped
one meets first. Mimir and Thanos do not cache instant queries, so every round is evaluated again; for large meshes,
[recording rules](https://prometheus.io/docs/practices/rules/) that keep caller, callee and cluster make each round
cheaper. Rounds start at different points of the interval, so the backend sees about `clusters ÷ interval` rounds per
second.

### Slow backends

Backends that answer like real ones (PromQL 300 ms plus 50 ms per thousand series, Tempo searches 400 ms, trace
fetches 100 ms), 10 clusters × 500 services, `maxTraces` 100 and 400:

| | Tempo source | Round per cluster |
|---|---|---|
| before | failed every round; 0 traces fetched in 3 400 searches; restarted from the same service | the whole `timeout` (25 s) |
| after | sampled every round: 5 100 traces with `maxTraces` 100; with 400, 124–132 services per round before the round's time ran out, said so (degraded), and the next round carried on | 13–17 s |

Before, searches ran one at a time, one failure failed the source, and the next round started over from the same
service, so the services past it were never sampled. Now four searches run at once, a round stops searching with 40%
of `timeout` left to fetch what it found, says when it ran out of time, and the next round carries on.

### Kubernetes inventory

10 clusters × 1 500 pods (pod lists of about 8 MB of JSON per cluster and round, 200 ms each): rounds of 0.6–0.9 s
instead of about 80 ms, 0.12 core instead of 0.08. The inventory lists every pod and service of each cluster every
round: for thousands of pods per cluster, set `namespaces`, or a longer `interval`.

### History

30 × 2 000 with `history.directory` set: 0.46 core between requests instead of 0.25. Each record is about 10 MB, so
the default `history.maxBytes` (256 MiB, per cluster) keeps about 25 captures, 13 minutes at a 30 s interval, and 30
clusters need 7.5 GB of disk: give the volume `clusters × maxBytes`, more than the chart's 1 Gi default.

### Viewers

30 × 2 000, with 100 browsers on the live stream (half of them on a slice) and 50 Grafana dashboards refreshing every
5 s (the six queries of the lab's dashboard):

| | teia CPU | Memory (peak) | Sent | `grafana/edges` p95 | Dashboard refresh p95 |
|---|---|---|---|---|---|
| before | 2.17 cores | 3.9 GiB | 43 MiB/s on streams + about 110 MB/s to Grafana | 137 ms (6.8 MB each) | 137 ms |
| after | 0.87 core | 3.1 GiB | 43 MiB/s on streams + about 4 MB/s to Grafana (gzipped) | 55 ms (encoded once a round) | 55 ms |

Each stream still gets every round's whole snapshot uncompressed (8.9 MB at 2 000 connections): for many browsers,
prefer the Grafana panel's queries.

### In the browser and in Grafana

Chromium, one map, no GPU (the slowest case):

| Map | Grouped | First drawn | Next round | Frames per second |
|---|---|---|---|---|
| 500 components, 2 000 connections | no | 1.2 s | 1.3 s | 45 |
| 2 000 components, 8 000 connections | no | 4.3 s | 16.7 s | 0.4 |
| 2 000 components, 8 000 connections | by namespace (20) | 1.3 s | 1.2 s | 15 |

A map of more than 1 000 components that nobody chose a grouping for now opens grouped (namespace, team, system or
part-of), and says so; **No grouping** still draws every component.

In Grafana 13.2 with the lab's dashboard (5 s refresh), frames of 8 000 connections are 41 MB of JSON per refresh
and viewer (1.4 MB gzipped now), and Grafana's backend took about 4 s per refresh to decode them. The panel no longer
redraws a refresh that brings the same round: in 30 s, the browser was blocked 0.4 s instead of 1.5 s for 2 000
connections, and 2.3 s instead of 4.1–4.9 s for 8 000; its memory went from 355–950 MB to 199–377 MB. For large maps,
refresh at teia's interval or slower: Grafana still fetches and decodes every refresh.

## What changed to get here

The same runs before these changes, at 30 × 2 000: 2.4 GiB, rounds up to 1 060 ms
when every cluster ran at once, 82 req/s with a snapshot p95 of 645 ms.

- The snapshot endpoint served a fresh, indented encoding per request (14 MB each);
  it now writes the round's own encoding, and a gzip copy made once per round.
  API responses are no longer indented.
- Every component and connection carried its own copy of the nine global health
  rules; they now share one read-only copy unless an override applies.
- Query sources allocated a dimensions map per series (three times per series per
  round); combinations are now keyed from the labels and share one map.
- Clusters started their rounds at the same moment; each now starts at its own
  point of the interval.
- Grafana frames and other per-round answers were encoded per request and sent
  uncompressed (41 MB per refresh at 8 000 connections); they are now encoded and
  gzipped once per round.
- The Tempo source searched one service at a time and failed whole when a search
  did; it now searches four at a time within the round's time and moves on.
- Every request of a source shared two idle connections per host and stopped at
  15 s; sources now keep up to 32 per host, and only the source's `timeout` bounds
  a round.
