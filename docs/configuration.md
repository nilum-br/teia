# Configuration reference

teia reads one YAML file: `--config PATH`, else `$TEIA_CONFIG`, else `./teia.yaml` (the container image sets
`TEIA_CONFIG=/etc/teia/teia.yaml`). Every key is optional. [`examples/teia.yaml`](../examples/teia.yaml) sets all of
them, with comments; a test loads it, and checks that every key below is in both places.

## Rules for the whole file

- **Unknown keys are errors**, with the line: `line 4: unknown key "preset" (misspelled, misplaced, or not supported
  by this version)`. A typo never silently drops a setting.
- **All problems are reported at once**, not one per run.
- **Durations** use Go syntax: `30s`, `5m`, `1h30m`. Negative durations are rejected. `0` means the default.
- **Error rates** are fractions between 0 and 1: `0.05` is 5%.
- **Environment variables**: `${NAME}` is replaced by the variable `NAME`, and `${NAME:-default}` by `default` when
  `NAME` is unset or empty. Secrets can stay out of the file.
  - An unset `${NAME}` without a default is an error that names it, so a missing `TEIA_TOKEN` cannot quietly turn
    auth off. Write `${NAME:-}` to make one optional on purpose.
  - Only the braced form is expanded: `$window` and regex anchors such as `web$` in queries are left alone.
  - Variables in comments do not count.
  - Expansion happens before the YAML is parsed. If a value may contain YAML syntax (`: `, ` #`, a leading `*` or
    `&`), quote it: `token: "${TEIA_TOKEN}"`.

## API and UI

| Key | Default | Description |
|---|---|---|
| `listen` | `:8080` | Address of the API and UI. `:8080` binds every interface; `127.0.0.1:8080` this machine only. Set `auth.token` or keep teia on a private network. |
| `auth` | | Protects every endpoint but `/healthz`. |
| `auth.token` | empty: open | Clients send `Authorization: Bearer <token>`. Browsers open `/?token=<token>` once and get an `HttpOnly` session cookie derived from the token. See [Exposing the API](architecture.md#exposing-the-api). |
| `cors` | | Lets browser pages on other origins read `/api/v1/*`. |
| `cors.allowedOrigins` | none | Exact origins, scheme and host only, such as `https://grafana.example.com` (no path). `"*"` allows any origin and must be listed explicitly. Credentials (cookies) are never allowed across origins. Needed for the Grafana panel's direct mode. |
| `maxStreams` | `100` | Concurrent `/api/v1/stream` clients. More get `503` with `Retry-After`. |

`teia serve --ui-dir DIR` (or `TEIA_UI_DIR`) serves the UI from a directory instead of the copy built into the
binary, for working on it. It is a flag, not a config key.

## Discovery rounds

| Key | Default | Description |
|---|---|---|
| `interval` | `30s` | Time between discovery rounds. |
| `timeout` | `20s` | How long each source may take in a round. A source still busy a little after that is given up on for the round, and skipped while it is busy. |
| `retention` | 3 × `interval` | How long an observed edge stays after it was last seen, so sampled signals do not make it blink. A failing source's last good observation is reused for as long, and the status says so. |
| `aliases` | none | Map of name → node ID: both sides are one node. Matched case-insensitively. A key may be an address no inventory knows, such as `"ip:192.0.2.10"` (quote it). One hop: aliases do not chain. |
| `ignore` | none | Node IDs to leave out of the graph, with their edges and findings. Containers can say the same with the label `teia.ignore=true`, pods with the annotation `teia.nilum.io/ignore: "true"`. |
| `health` | | Limits of each golden signal. A node or edge takes the worst of its signals, and the map says which one (its `reasons`). |
| `health.degraded` | `0.01` | From this error rate a node or edge is `degraded` (amber). |
| `health.failing` | `0.05` | From this error rate it is `failing` (red). Must not be below `health.degraded`, defaults included. |
| `health.latency` | | Limits on the p95 of calls, where a signal measures durations. |
| `health.latency.degraded` | `500ms` | From this p95 a node or edge is `degraded`. |
| `health.latency.failing` | `2s` | From this p95 it is `failing`. Must not be below `health.latency.degraded`, defaults included. |
| `health.saturation` | | Limits on the used share of a capacity (0 to 1), such as connections out of `max_connections`. |
| `health.saturation.degraded` | `0.8` | From this share a node is `degraded`. |
| `health.saturation.failing` | `0.95` | From this share it is `failing`. |
| `health.locks` | | Limits on sessions waiting on a lock for over a second (from the `postgres` explorer, with `pg_monitor`). |
| `health.locks.degraded` | `1` | From this many waiting sessions a line is `degraded`. |
| `health.locks.failing` | `5` | From this many it is `failing`. |
| `health.backlog` | | Limits on items waiting in a queue, such as consumer lag. |
| `health.backlog.degraded` | `1000` | From this many waiting items a node or line is `degraded`. |
| `health.backlog.failing` | `10000` | From this many it is `failing`. |

## Notifications

| Key | Default | Description |
|---|---|---|
| `notify` | | Sends the changes of each round to webhooks. |
| `notify.webhooks` | none | List of webhooks. |
| `notify.webhooks[].url` | required | `http` or `https` URL. It is never quoted in errors or logs (for Slack, the URL is the secret). |
| `notify.webhooks[].format` | `json` | `json` (the changes as JSON) or `slack` (an incoming-webhook message). |
| `notify.webhooks[].events` | all | Change types to send: `edge.added`, `edge.removed`, `edge.state`, `edge.health`, `node.added`, `node.removed`, `node.health`, `node.visibility`, `finding.opened`, `finding.resolved`. |
| `notify.webhooks[].headers` | none | Map of extra request headers, such as `Authorization`. |

## Sources

| Key | Description |
|---|---|
| `sources` | Where teia looks. Every source is optional and read-only. See [sources.md](sources.md). |

Each source has a name, used in statuses, findings and each edge's `sources`: `declared`, `kubernetes`, `docker`,
`postgres/<node>`, and for lists `prometheus` (one entry), `prometheus/<name>` or `prometheus/<n>` (several without a
name); the same for `tempo`, and `<dialect>/<name>` for queries. Two sources with one name are rejected.

### `sources.declared`: flows files

| Key | Default | Description |
|---|---|---|
| `sources.declared` | | Reads `Flows` documents (intent). See [flows-spec.md](flows-spec.md). |
| `sources.declared.paths` | none | Files or directories, walked recursively, relative to the directory teia runs in. Only `*.yaml` and `*.yml` files up to 1 MiB are read; `.git`, `node_modules`, `vendor`, `dist` and `.terraform` are skipped, and documents of other kinds (a Backstage entity in the same file) are ignored. A broken file is a finding, not a failure. |

### `sources.prometheus`: metrics

| Key | Default | Description |
|---|---|---|
| `sources.prometheus` | | List of Prometheus-compatible query APIs: Prometheus, Thanos, Mimir, VictoriaMetrics, Grafana Cloud. |
| `sources.prometheus[].name` | | Source name suffix. |
| `sources.prometheus[].url` | required | Base URL, without `/api/v1/query`. Credentials in the URL work and are masked in messages, but prefer a header. |
| `sources.prometheus[].window` | `5m` | Range of the `rate()` in every query. |
| `sources.prometheus[].presets` | `[auto]` | Metric families to read. Empty or `[auto]` detects which exist (re-checked every 5 minutes). Names: `servicegraph`, `istio`, `istio-tcp`, `linkerd`, `linkerd-tcp`, `beyla-network`, `obi-network`, `hubble-tcp`, `hubble-http`, `retina`. Unknown names are rejected. See [Presets](sources.md#presets). |
| `sources.prometheus[].headers` | none | Map of request headers, such as `Authorization: Bearer ${PROM_TOKEN}` or `X-Scope-OrgID` for multi-tenant Mimir. |

### `sources.tempo`: traces

| Key | Default | Description |
|---|---|---|
| `sources.tempo` | | List of Tempo query APIs. teia samples traces and pairs client and server spans into edges, with no metrics-generator needed. |
| `sources.tempo[].name` | | Source name suffix. |
| `sources.tempo[].url` | required | Base URL, such as `http://tempo:3200`. |
| `sources.tempo[].window` | `15m` | How far back to search for traces. |
| `sources.tempo[].tracesPerService` | `20` | Most traces sampled per service in a round. |
| `sources.tempo[].maxTraces` | `400` | Most traces per round. Each service gets an even share (at least one); when that cannot cover every service, rounds take turns. |
| `sources.tempo[].headers` | none | Map of request headers. |

### `sources.queries`: any PromQL or LogQL

For signals no preset knows: access logs in Loki, ingress controllers, custom exporters, flow logs as metrics. The
query is an instant query that returns one series per caller and callee pair; its value is read as requests per
second.

| Key | Default | Description |
|---|---|---|
| `sources.queries` | | List of queries. |
| `sources.queries[].name` | | Source name suffix: `loki/access-logs`. |
| `sources.queries[].dialect` | `prometheus` | `prometheus` (sent to `/api/v1/query`) or `loki` (`/loki/api/v1/query`, a LogQL metric query). |
| `sources.queries[].url` | required | Base URL of the API. |
| `sources.queries[].query` | required | The query. `$window` is replaced by `window` as a PromQL duration (`5m`, `90s`), so write `[$window]`. |
| `sources.queries[].window` | `5m` | Value of `$window`. Addresses seen over a window are only resolved to a workload that held them for all of it. |
| `sources.queries[].from` | required | Label holding the caller. |
| `sources.queries[].to` | required | Label holding the callee. |
| `sources.queries[].fromAddress` | `false` | The `from` label is an IP address (a port is dropped), resolved through the inventory. |
| `sources.queries[].toAddress` | `false` | The same for `to`. |
| `sources.queries[].protocol` | empty | Protocol of every edge, such as `http`. |
| `sources.queries[].protocolLabel` | empty | Label holding the protocol; `protocol` is used for series without it. |
| `sources.queries[].toKind` | `unknown` | Kind of the callees: `service`, `database`, `cache`, `queue`, `external` or `unknown`. |
| `sources.queries[].headers` | none | Map of request headers. |

Series without both labels, or with `-` as a value, are skipped.

### `sources.kubernetes`: inventory

| Key | Default | Description |
|---|---|---|
| `sources.kubernetes` | | Lists pods and Services: workload names, kinds, labels, images, and which pod or Service IP belongs to whom. `kubernetes: {}` uses the defaults. Needs `get`, `list` and `watch` on pods, services and namespaces, nothing else (the `kubernetes-rbac` finding has the role). |
| `sources.kubernetes.kubeconfig` | empty | Path to a kubeconfig. With this and `context` empty: in-cluster config when running in a pod, else `KUBECONFIG` or `~/.kube/config`. |
| `sources.kubernetes.context` | empty | Context in that kubeconfig; empty means its current context. |
| `sources.kubernetes.namespaces` | all | Namespaces to read. |
| `sources.kubernetes.excludeNamespaces` | `kube-system`, `kube-public`, `kube-node-lease` | Namespaces to skip. Setting it replaces the default list. |

### `sources.docker`: inventory

| Key | Default | Description |
|---|---|---|
| `sources.docker` | | Lists containers (one `GET /containers/json` per round): names, kinds and labels (`teia.node`, `teia.kind`, `teia.tech`, `teia.ignore`), images and addresses. `docker: {}` uses the defaults. |
| `sources.docker.host` | `DOCKER_HOST`, then the OS default | `unix:///path`, `npipe:////./pipe/docker_engine` or `tcp://host:port`. The default is the named pipe on Windows, else the first socket found among `/var/run/docker.sock`, Docker Desktop, OrbStack, Colima, Rancher Desktop and Podman. In a container, point it at a socket proxy: see [Docker access](sources.md#docker-access). |
| `sources.docker.socket` | | Shorthand for `host: unix://<socket>`; `host` wins when both are set. |
| `sources.docker.project` | all | Only containers of this compose project (`com.docker.compose.project` label). |

### `sources.postgres`: inside a database

| Key | Default | Description |
|---|---|---|
| `sources.postgres` | | List of PostgreSQL servers to explore from the inside: `pg_stat_activity` names every connected client, including jobs and people that emit no telemetry. |
| `sources.postgres[].node` | required | Node ID of this database in the graph, such as `orders-db`. |
| `sources.postgres[].dsn` | required | Connection string, URL (`postgres://teia:${PG_PASSWORD}@orders-db:5432/orders`) or `key=value`. Percent-encode special characters of a password in a URL. The password never appears in messages. The login needs `pg_monitor` to see other sessions' origins; the advisor asks for it. |
