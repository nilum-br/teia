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
callee pair, plus a mapping of which label is which.

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
