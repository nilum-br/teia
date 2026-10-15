# Capacity runs

How large a map one teia can hold, measured with synthetic telemetry at sizes
no lab cluster reaches. Results and sizing advice are in
[docs/capacity.md](../../docs/capacity.md).

- `synth/` serves Prometheus and Tempo APIs for many clusters: each runs the
  same services (`svc-0000`…) with its own traffic, each service calls
  `-fanout` others, each call is one series per journey and tenant.
- `teiaprof/` runs teia's server exactly as `teia serve` builds it, plus Go's
  profiler on `127.0.0.1:16060`. It is a measurement tool; teia itself has no
  profiler.
- `api.js` is the [k6](https://k6.io) load while rounds run: snapshots (whole and
  sliced), the cluster list, Grafana frames and MCP tools (overview, map,
  impact, path).
- `run.sh CLUSTERS SERVICES FANOUT JOURNEYS TENANTS [OUT_DIR]` writes the
  config (one `clusters` entry per cluster, each with a query source and a
  Tempo source), waits for two rounds per cluster, samples memory and CPU for a
  minute, runs k6 and prints one JSON line. `INTERVAL`, `TIMEOUT`, `TRACES`,
  `VUS` and `DURATION` override the defaults.

```sh
lab/stress/run.sh 30 500 4 2 2 /tmp/teia-stress-30
go tool pprof -top /tmp/teia-stress-30/bin/teiaprof /tmp/teia-stress-30/heap.pprof
```

Needs Go, curl, jq, bc and k6; ports 19090, 18480 and 16060 free. The numbers
measure teia, not your backends: the synthetic APIs answer in microseconds,
so real rounds take longer by your Prometheus and Tempo latency.
