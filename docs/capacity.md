# Capacity

How large a map one teia holds, and what to change when it grows. Measured with
[lab/stress](../lab/stress/README.md): synthetic Prometheus and Tempo APIs for
many clusters, one teia reading all of them through [`clusters`](configuration.md#clusters),
and k6 reading the API and MCP while rounds run. Each cluster has a query source
(three PromQL queries per round) and a Tempo source (100 sampled traces per round);
a "connection" is one caller→callee pair, each with one series per journey and tenant
combination when the queries carry those dimensions. One process, 30 s interval,
Apple M-series laptop.

## Results

| Clusters × connections | Series per round | Memory (RSS) | Round per cluster (max) | Snapshot | API under load (20 VUs) |
|---|---|---|---|---|---|
| 1 × 300 | 900 | 48 MiB | 18 ms | 1.5 MB | 6210 req/s, snapshot p95 4 ms |
| 10 × 300 | 9 000 | 126 MiB | 29 ms | 1.5 MB | 6448 req/s, snapshot p95 4 ms |
| 10 × 2 000 (2 journeys × 2 tenants) | 240 000 | 583 MiB | 76 ms | 8.8 MB | 652 req/s, snapshot p95 38 ms |
| 30 × 2 000 (2 journeys × 2 tenants) | 720 000 | 1.5–1.6 GiB | 76–91 ms | 8.8 MB | 576–606 req/s, snapshot p95 44 ms, MCP p95 53 ms |
| 100 × 900 | 270 000 | 2.1 GiB | 29 ms | 4.2 MB | 2105 req/s, snapshot p95 19 ms |
| 50 × 4 000 | 600 000 | 3.0 GiB | 47 ms | 16.5 MB | 180 req/s, snapshot p95 210 ms |

One run (30 × 2 000) had 4 of 17 198 requests end with `unexpected EOF` in the first second of the
load; four later runs with the same load (about 41 000 requests) had none, and teia logged nothing. The
cause was not found. Every other run had no failed request. The MCP load includes `teia_impact` and
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
  service and one fetch per trace (`maxTraces`). With 50 clusters every 30 s that is
  about 5 PromQL queries and 170 Tempo requests per second in all.
- **The API:** the whole snapshot is served as the round encoded it, gzipped once
  per round for browsers. Slices, Grafana frames and MCP answers are computed per
  request; with tens of thousands of connections per cluster, expect hundreds of
  milliseconds for them. Dashboards should read one cluster (`?cluster=`) and a
  slice only when they need one.
- **`/metrics`** has one series per component and connection: about 8 MB with
  30 × 2 000. Scrape `/metrics?cluster=name` per cluster, or drop
  `teia_edge_info`/`teia_node_info` at scrape time if you only alert on health and
  sources.

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
