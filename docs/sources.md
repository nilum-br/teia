# Signals and sources: using what the platform already has

teia ships no agent. It reads the telemetry a platform already collects, with read-only credentials, and
merges everything into one graph. Each edge records which **signals** saw it (`traces`, `mesh`, `network`,
`logs`, `metrics`, `database`), so mixed platforms are normal, not a special case.

## Do I need a service mesh?

**No.** A mesh is one signal among several. Each signal sees a different slice:

| Signal | What it sees | What it misses | teia source |
|---|---|---|---|
| **Traces** (OpenTelemetry SDKs or agents, Beyla/OBI) | L7 calls between instrumented services; databases, caches and brokers named in client spans | anything without instrumentation that is never called by something instrumented | `prometheus` preset `servicegraph`, `tempo` |
| **Mesh** (Istio sidecar or ambient, Linkerd) | every call through a proxy, with or without an SDK | workloads outside the mesh (they show up as `unknown`) | `prometheus` presets `istio`, `istio-tcp`, `linkerd`, `linkerd-tcp` |
| **Network** (eBPF: Beyla/OBI flows, Cilium Hubble, Retina) | every TCP/UDP conversation on the node, no SDK, no mesh | L7 detail, and it needs eBPF on the nodes | `prometheus` presets `beyla-network`, `obi-network`, `hubble-tcp`, `hubble-http`, `retina` |
| **Logs** (access logs in Loki) | who called a service, from its own access log | services that do not log their callers | `queries` with `dialect: loki` |
| **Database** (the datastore itself) | every client of a database, including jobs and people | nothing about the rest of the platform | `postgres` (more explorers on the roadmap) |
| **Inventory** (Kubernetes, Docker) | everything running, and which IP belongs to whom | traffic | `kubernetes`, `docker` |
| **Intent** (flows files) | what teams say should happen | reality | `declared` |

The more signals, the fewer blind spots, and the advisor says which signal would close each one.

## A mixed platform, for example: part Istio, part OpenTelemetry

This is the lab's shape, and a common one:

```yaml
sources:
  prometheus:
    - url: http://prometheus:9090
      presets: [auto]          # finds service graph metrics AND Istio metrics on its own
  tempo:
    - url: http://tempo:3200   # traces straight from Tempo, even without the metrics-generator
  queries:
    - name: access-logs        # names callers nothing else can
      dialect: loki
      url: http://loki:3100
      query: sum by (service, remote_addr) (rate({namespace=~".+"} | json | __error__="" [$window]))
      from: remote_addr
      fromAddress: true
      to: service
  kubernetes: {}               # names, kinds, pod IPs
```

What happens to each kind of call:

- **Meshed service → meshed service, no SDK:** Istio reports it. Signal `mesh`.
- **Instrumented service → anything:** traces report it. Signal `traces`. If the callee is meshed too, both signals agree on one edge.
- **Instrumented, unmeshed caller → meshed callee:** Istio sees an `unknown` caller, traces name it. teia explains the mesh's unknown caller with the traced edge and raises nothing.
- **Caller with neither:** the service graph sees a `user` caller and Istio sees an `unknown` one. If the callee's access logs carry the caller IP, Loki plus the inventory name it. Otherwise the advisor says exactly that, and suggests access logs, a sidecar or Beyla.

## Auto-detection

With `presets: [auto]`, every few minutes teia checks which metric families exist (one cheap `count()` per preset)
and uses all of them. To see what a platform offers before writing any config:

```sh
teia detect --prometheus http://prometheus:9090 --tempo http://tempo:3200 --loki http://loki:3100 --kubernetes
```

It prints what it found per source and a starting `teia.yaml`.

## Presets

| Preset | Detected by | Edge from | Notes |
|---|---|---|---|
| `servicegraph` | `traces_service_graph_request_total` | `client` → `server` | OTel Collector `service_graph` connector, Tempo metrics-generator, Beyla/OBI. `client="user"` is reported as unknown callers; `db.system` / `messaging.system` dimensions name datastores and brokers. |
| `istio` | `istio_requests_total` | `source_workload` → `destination_workload` | `reporter="source"`, then `"waypoint"`, then `"destination"` for unknown callers, joined with `or` the way Kiali does, so no double count. |
| `istio-tcp` | `istio_tcp_connections_opened_total` | same | Plain TCP through the mesh and ambient ztunnel. Waypoint hops are folded back into end-to-end edges. |
| `linkerd` | `response_total{direction="outbound"}` | `deployment` (or `statefulset`, `daemonset`, `k8s_job`) → `dst_deployment` / `dst_service` | Outbound side only, so each request counts once. Needs the linkerd-viz scrape labels. |
| `linkerd-tcp` | `tcp_open_total{direction="outbound",peer="dst"}` | same | TCP connections opened through the proxy (databases, brokers). Same labels needed. |
| `beyla-network`, `obi-network` | `*_network_flow_bytes_total{direction="request"}` | `k8s_src_owner_name` → `k8s_dst_owner_name` | Proves edges exist; bytes are not reported as requests. |
| `hubble-tcp`, `hubble-http` | `hubble_tcp_flags_total{flag="SYN"}` / `hubble_http_requests_total` | `source_workload` → `destination_workload` | Needs `labelsContext` workload labels in the Hubble metrics config. |
| `retina` | `networkobservability_adv_forward_count` | `source_workload_name` → `destination_workload_name` | Needs remote context (`MetricsConfiguration`). |

### Latency

Four presets also read a duration histogram, so edges carry how long calls take (p50 and p95, in milliseconds,
as the caller waits for them):

| Preset | Histogram |
|---|---|
| `servicegraph` | `traces_service_graph_request_client_seconds_bucket` (the client side, which exists for databases and brokers too) |
| `istio` | `istio_request_duration_milliseconds_bucket`, with the same reporters as the requests |
| `linkerd` | `response_latency_ms_bucket{direction="outbound"}` |
| `hubble-http` | `hubble_http_request_duration_seconds_bucket` |

When one edge is made of several series (HTTP and gRPC, two destination services), their quantiles are weighted
by each series' traffic. `tempo` measures the caller's span of each sampled call, which is only a hint with few
traces: histograms win when both exist. Messages (producer and consumer spans) have no latency: nobody waits for
them. A node's latency is that of the calls it serves, weighted by each caller's traffic. A failing latency query
keeps the edges and says so in the source's message.

Anything else becomes edges through a `queries` entry: any PromQL or LogQL that returns one series per caller and
callee pair, plus a mapping of which label is which. Give it an `errors` query too (failed requests per second,
grouped by the same two labels) and, if you have one, a `latency` query (p95 in seconds): without `errors` teia
knows the traffic but not whether it fails, so those lines stay grey instead of claiming to be green.

### Observation time and trace sampling

Collection time says when teia obtained an answer, not when the reported call happened. For each Tempo edge,
`evidence.sampleCount` counts calls found in the sampled traces, `observedFrom` is their earliest span start
and `observedAt` their latest span end. Neither bound claims continuous coverage or the service's last call;
sample count is not total traffic or req/s. If any contributing call has missing or invalid times, both bounds
remain unknown, even when some other calls are dated. Nanosecond timestamps are preserved in the API.

Each round searches a few services at a time, each for its share of `maxTraces`, and stops starting searches once
less than 40% of the source's `timeout` is left, keeping that time to fetch the traces it found. A round that ran out
of time, or whose searches partly failed, keeps what it sampled and says so (degraded); the next round carries on with
the services this one did not reach, so a slow Tempo or a failing search never stalls the rotation. Trace fetches
are bounded to the search window and one window before it: without a time range, Tempo looks a trace up in every block
it keeps. On a Tempo shared by several clusters, give the source a [`selector`](configuration.md#shared-backends). For
many services, prefer the [service graph metrics](https://grafana.com/docs/tempo/latest/metrics-generator/service_graphs/)
of Tempo's metrics-generator (the `servicegraph` preset), which see every span; a sample of 100–400 traces per round
finds which calls exist and the operations, not their rates.

Tempo searches use one window per collection round. A full trace can contain calls outside that search window;
teia keeps their original times instead of replacing them with the search or collection time. For fully dated
samples, IP resolution also checks the earliest call against the inventory's address ownership start. Old
calls must not be assigned to a service that acquired the IP later. Age in details is measured at the evidence's
collection, so paused views do not imply a ticking live measurement; future times explicitly flag the source clock.

The timestamp contract follows [OTLP span start/end semantics](https://github.com/open-telemetry/opentelemetry-proto/blob/main/opentelemetry/proto/trace/v1/trace.proto).
The [Tempo search API](https://grafana.com/docs/tempo/latest/api_docs/#search) returns a limited sample, not a
guarantee of the newest calls. Sources expose unknown original observation times where their responses do not
establish them. Query evaluation time is not substituted for the underlying samples' time.

### Metric evaluation and input sample age

Each Prometheus or custom PromQL/LogQL collection uses one explicit evaluation time for its measurement
queries, exposed as `evidence.evaluatedAt`. `collectedAt` records when that source finished, independently of
slower sources; the snapshot's `generatedAt` records assembly of the whole round. Retained evidence keeps
its original times. Address attribution checks ownership over the evaluated window, so collection delay
cannot move that window past an IP reassignment. Detection queries do not establish measurement time.

For mapped gauge readings, `evidence.oldestSampleAt` identifies the oldest input sample reported for that
evaluation. Teia applies PromQL `timestamp()` to raw selectors before filtering or aggregation, then takes
the minimum across the reading's inputs, including capacity where required. Missing timing for either the
value or its capacity leaves age unknown. Optional timestamp queries run after all probes have their values;
their failures or timeouts preserve the readings and health and mark the source degraded. `origin` identifies
the probe/exporter; times remain separate for each reading.

Supported readings are Redis memory/clients, Kafka messages waiting, RabbitMQ queue gauges, MongoDB
connections/lock queues, MySQL connections/lock waits, postgres-exporter connections/transaction age/replica
delay and cAdvisor memory. RabbitMQ metric variants are timestamped separately before coalescing to avoid
duplicate labelsets. Derived rates, latency, drain estimates and restart/eviction/rejection counts still have
unknown original sample times. Window-based probes expose their configured lookback separately.

An input sample timestamp belongs to the stored metric, not necessarily the last application activity or
the exporter's internal update. Pinning evaluation time does not freeze ingestion between HTTP requests;
late data can arrive between value and timestamp queries, so these calls do not form an atomic snapshot.
Per-reading times do not establish graph-wide freshness. These distinctions follow the
[Prometheus instant-query API](https://prometheus.io/docs/prometheus/latest/querying/api/#instant-queries),
[`timestamp()` semantics](https://prometheus.io/docs/prometheus/latest/querying/functions/#timestamp) and
[Loki query API](https://grafana.com/docs/loki/latest/reference/loki-http-api/#query-logs-at-a-single-point-in-time).

## Component health

Traffic, errors and latency say how calls go. To say *why* a datastore, a queue or a workload is in trouble,
the `prometheus` source also reads the standard exporters most platforms already run, through **probes**
(detected like presets; `probes: [none]` turns them off):

| Component | Probe | Exporter | What it reads |
|---|---|---|---|
| Cache | `redis` | redis_exporter (Redis, Valkey, KeyDB) | memory/client capacity, evictions and rejected connections in the window |
| Queue | `kafka` | kafka_exporter | per group/topic backlog, drain estimate assuming no new arrivals, unchanged offsets over the window; group backlog on the topic |
| Queue | `rabbitmq` | RabbitMQ's built-in plugin, per-object metrics | ready/unacknowledged counts and valid head-message timestamp age (publisher clock) |
| Document database | `mongodb` | mongodb_exporter | connections against those available, operations queued on locks |
| Relational database | `mysql` | mysqld_exporter (MySQL, MariaDB) | connections against `max_connections`, row lock waits |
| Relational database | `postgres-exporter` | postgres_exporter | connections against `max_connections`, oldest transaction age and applicable replica replay estimate |
| Workload | `containers` | cAdvisor (Kubernetes or Docker) | CPU time throttled by the limit, memory against the limit |
| Workload | `restarts` | kube-state-metrics | container restarts in the window |

Each reading becomes a **measure** on a node, or on the line from a consumer to its topic, and the health limits
(`health.saturation`, `health.backlog`, `health.lag`, `health.locks`, `health.restarts`, `health.evictions`,
`health.rejections`) turn it into a reason. [Resource diagnostics](resource-diagnostics.md) documents
collector/label prerequisites, missing capabilities, pressure limits, queue estimates and explorer decisions.

**Which node a reading belongs to.** Exporters label metrics with their own names, so a reading carries
candidates, best first: the names without an exporter suffix (`catalog-cache-exporter` → `catalog-cache`), the
Kubernetes service, the workload of the pod, the job, and last the instance's address, which resolves to the node
holding it when the exporter is a sidecar. Consumer groups and topics are named as they are. A reading never
creates a node: when no candidate is on the map, the advisor says so and suggests an `alias`.

## Inside the database: the `postgres` explorer

The explorer connects to PostgreSQL with a login of its own and reads `pg_stat_activity`, so it sees every client,
including the ones that emit no telemetry. Its health readings include:

- **Connections** in use against the connections ordinary roles may open (`max_connections` minus
  `superuser_reserved_connections`): the database's saturation, amber from 80% and red from 95% by default.
- **Sessions waiting on a lock** for over a second, per client, with the session holding the lock
  (`pg_blocking_pids`): the line from that client to the database turns amber from one waiting session and red from
  five, and says who holds the lock, for example `3 sessions waiting on locks, over 1, held by legacy-reports`.
- **Oldest open transaction age**, from client transaction starts, and **replica replay acknowledgement
  delay**, when the primary's replication statistics supply it. Both use `health.lag`; NULL replay feedback
  is explicitly unavailable/not applicable, never zero. A transaction start does not become a call timestamp.

Without `pg_monitor` PostgreSQL hides other sessions' addresses and waits, so locks only show once the advisor's
`GRANT pg_monitor` is in place. The role stays read-only: it sees session metadata and statistics, never table
data.

## Docker access

The Docker inventory makes one call per round: `GET /containers/json` (filtered to one compose project with
`sources.docker.project`). The Docker API itself has no read-only mode, and mounting `docker.sock` into a container
**read-only (`:ro`) does not restrict it**: connecting to a socket is not a write to the file, so every API call
still goes through, and a client that can connect can start privileged containers, that is, control the host.

So in a container, give teia the API through a socket proxy that allows only that call:

```yaml
services:
  docker-proxy:
    image: lscr.io/linuxserver/socket-proxy:3.4.6-r0-ls102
    environment:
      CONTAINERS: 1   # list and inspect containers; POST is off by default
      EVENTS: 0       # on by default, not needed by teia
      PING: 0
      VERSION: 0
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    read_only: true
    tmpfs: [/run]
  teia:
    environment:
      DOCKER_HOST: tcp://docker-proxy:2375   # or sources.docker.host
```

Keep the proxy on a network only teia can reach: `CONTAINERS` also allows `GET /containers/{id}/json`, which shows
every container's environment variables. With the proxy, teia needs no access to the socket and runs as its image's
non-root user. A proxy that refuses the call (HTTP 403) shows up as the `docker-socket` finding.

The lab does exactly this, with a second proxy for Alloy (which also needs networks and container logs), each on an
internal network with its one client. Running teia on your own machine instead (`lab/teia.host.yaml`), it uses
`DOCKER_HOST` or the OS default, like the `docker` CLI. On Kubernetes, use the `kubernetes` inventory: it needs
`get`, `list` and `watch` on pods, services and namespaces, and no Docker access at all.

## Identity: names, IPs and recycled addresses

Every source ends up naming nodes by workload name (`orders`). Sources that only know an IP (database sessions,
access logs, flows) are resolved through the inventory. **IPs get recycled.** The inventory says since when each
workload holds its address, and an address seen over a time window (logs, metric queries) is only attributed to a
workload that held it for the whole window. Otherwise the edge waits for the next rounds instead of being guessed.

## Making it available

Everything teia learns is exposed in several ways at once:

| Where | What |
|---|---|
| Grafana | the `nilum-teia-panel` plugin, fed by the dashboard's queries (or straight from teia's stream) |
| teia's own UI | the same map, live through Server-Sent Events |
| `/api/v1/*` | JSON snapshot, graph, findings, coverage, changes, flow matrix |
| `/api/v1/stream` | one Server-Sent Event per discovery round |
| `/metrics` | `teia_edge_info`, `teia_node_info`, `teia_coverage_ratio`, `teia_findings`, for alerting |
| webhooks | `notify.webhooks` (JSON or Slack) with every change: new undeclared communication, health changes, findings |
| CLI | `teia discover --fail-on warning` in CI, `teia export matrix` for firewall requests |

The API listens on every interface by default (`listen: :8080`). Protect it with `auth: { token: ${TEIA_TOKEN} }`
or keep it on a private network, and list browser origins that may call it (Grafana in direct mode) under
`cors.allowedOrigins`. See [Exposing the API](architecture.md#exposing-the-api), and
[configuration.md](configuration.md) for every key.
