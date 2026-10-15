# Capacity runs

How large a map one teia holds: many clusters, connections and series, read by
one process while the API and MCP are under load. The measured results and the
sizing advice are in [docs/capacity.md](../../docs/capacity.md).

## Run it

```sh
make stress        # two scenarios, about 6 minutes
make stress-full   # every scenario of docs/capacity.md, about 25 minutes
```

You need Go, curl, jq, bc and [k6](https://grafana.com/docs/k6/latest/set-up/install-k6/)
(`brew install k6`, or your package manager), and ports 19090, 18480 and 16060
free. Nothing else runs: no Docker, no cluster. Each run ends with a table in the
format of docs/capacity.md:

```text
| Clusters × connections | Series per round | Memory (RSS) | Round per cluster (max) | Snapshot | API under load | Failed |
|---|---|---|---|---|---|---|
| 1 × 300 | 900 | 48 MiB | 18 ms | 1.4 MB | 6210 req/s, snapshot p95 4 ms, MCP p95 6 ms | 0% |
| 10 × 2000 | 240000 | 583 MiB | 76 ms | 8.5 MB | 652 req/s, snapshot p95 38 ms, MCP p95 40 ms | 0% |
```

Compare it with docs/capacity.md: same machine class, same scenario. Memory
within about 10% and latencies within about 30% are noise; more than that is a
change to explain. The first line of the output says where the results are kept.

Your own scenarios, as `clusters services calls-per-service journeys tenants`:

```sh
lab/stress/ladder.sh "20 800 4 1 1" "5 2000 5 3 2"
OUT=/tmp/my-run lab/stress/ladder.sh full          # keep the results somewhere known
INTERVAL=60s VUS=50 DURATION=60s lab/stress/ladder.sh   # tune one run
```

| Variable | Default | What it changes |
|---|---|---|
| `INTERVAL` | `30s` | teia's discovery interval |
| `TIMEOUT` | `25s` | each source's timeout |
| `TRACES` | `100` | traces sampled per cluster and round (`maxTraces`) |
| `VUS` / `DURATION` | `20` / `30s` | k6 virtual users and length |
| `OUT` | a new directory in `$TMPDIR` | where results go |

## What it does

- `synth/` serves Prometheus and Tempo APIs for many clusters, under
  `/c/<i>/`: each runs the same services (`svc-0000`…) with its own traffic, each
  service calls `-fanout` others, and each call is one series per journey and
  tenant combination.
- `teiaprof/` runs teia's server exactly as `teia serve` builds it, plus Go's
  profiler on `127.0.0.1:16060`. It is a measurement tool; teia itself has no
  profiler.
- `run.sh CLUSTERS SERVICES FANOUT JOURNEYS TENANTS [OUT_DIR]` writes the config
  (one `clusters` entry per cluster, each with a query source and a Tempo source),
  waits for two rounds per cluster, samples memory and CPU for a minute, runs k6
  and prints one JSON line.
- `api.js` is the k6 load while rounds run: whole and sliced snapshots, the
  cluster list, Grafana frames, and the MCP overview, map, impact and path tools.
  A failed request prints its method, URL and error.
- `ladder.sh` runs several scenarios and prints the table.

Each scenario keeps its folder: `teia.yaml`, `teia.log`, `k6.log`, `k6.json`,
`snapshot-c0.json` and `heap.pprof`. To see where memory goes:

```sh
go tool pprof -top -sample_index=inuse_space <scenario>/bin/teiaprof <scenario>/heap.pprof
```

## Limits

The synthetic APIs answer in microseconds, so these numbers measure teia, not
your backends: a real round takes as long as your Prometheus and Tempo do. There is
no Kubernetes inventory in these runs (it needs an API server per cluster). Run on
a quiet machine; other load skews CPU and latency.
