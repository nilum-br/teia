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

## Observation history

| Key | Default | Description |
|---|---|---|
| `history` | disabled | Opt-in archive written by `teia serve`; one process owns the directory. One-shot `discover` does not write it. |
| `history.directory` | empty: disabled | Writable directory for immutable snapshot records. Use a persistent volume to survive container replacement. |
| `history.retention` | `168h` | Retain snapshots by their original `generatedAt`, without renewing old data on restart. |
| `history.maxBytes` | `268435456` | Maximum total published record bytes (256 MiB). The oldest records expire first; an individually oversized record is rejected without deleting existing history. Temporary atomic publication can use up to one extra record. |
| `history.maxSnapshots` | `10000` | Maximum retained records, capped at 100000. Zero selects the default. |

Historical snapshots preserve topology, identities, source provenance, query windows, original sample times,
health rules, evidence links and ownership scopes as captured. They are marked `timeMode: "historical"` on
replay; old health is never relabelled current. Archive write errors are logged and reported by the history API
without interrupting current discovery. Retention bounds are enforced on writes, startup and history reads.

`GET /api/v1/history?from=<RFC3339>&to=<RFC3339>` lists actual snapshots captured in the inclusive interval.
Omit both parameters to list all retained records. `availablePeriod` is the oldest/newest retained capture,
not proof of continuous coverage between them. `coverage: "discrete-snapshots"` and `unavailable: true` make an
empty requested interval explicit; unavailable history never means zero traffic. Late records retain their
original capture and evidence times; a restart or repeated round number cannot overwrite another snapshot.

`GET /api/v1/history/{id}` replays the exact snapshot with `history.id` for a shareable link and `recordedAt`
for archive publication. Per-node and per-connection presence metadata includes `firstSeen`/`lastSeen` in the
snapshot graph and an `episode` that resets after absence or an address/identity change. These are presence
times, not call timestamps. `firstObservedAt`/`lastObservedAt` exist only for supplied original observations;
collection time is never substituted. The identity fingerprint is based on stable IDs, namespace, kind and
addresses, and cannot prove that an identical address still belongs to the same process incarnation.

For a Grafana dashboard pinned to one stored ID, use
`/api/v1/history/{id}/grafana/{nodes,edges,findings,sources,coverage,changes}` for all its queries. Every nonempty
table carries `timeMode: "historical"`, `historyId` and `snapshotAt`; the first node row carries JSON `history`
and `journeyContract` metadata. Coverage also carries `history`, preserving empty captures. Import the lab's
[`history.json`](../lab/grafana/dashboards/history.json) dashboard and supply its `snapshot` variable. The
Grafana time picker does not synthesize or aggregate an unrecorded interval. Source measurements keep their
own lookback periods; no interpolation, percentile combination or rate rescaling is performed for a wider range.

Archive schema version 1 uses JSON records published by temporary-file write, file sync and rename. A known
version 0 envelope containing `snapshot` migrates to version 1 while preserving evidence; migration derives
presence metadata only from that record. Incomplete temporary files are removed on startup. Corrupt, identity-mismatched
or future-version records stop startup with a useful error and are not silently discarded. Keep backups before
upgrading; this bounded local archive is not a distributed database or a substitute for telemetry retention.

With Helm, set `persistence.enabled: true`; the chart mounts storage, sets the directory and uses a single-writer
`Recreate` rollout. See the [chart values](../charts/teia/README.md). The local Docker lab enables a 24-hour,
64-MiB, 2000-record archive on its `teia-history` volume.

## Evidence links

| Key | Default | Description |
|---|---|---|
| `evidenceLinks` | none | Public investigation links attached to nodes and connections. |
| `evidenceLinks[].label` | required | Action label, for example Search caller logs. |
| `evidenceLinks[].target` | required | `node` or `edge`. Configure separate templates for each. |
| `evidenceLinks[].kind` | required | `traces`, `logs` or `dashboard`. Search links do not promise an exact trace or correlated log lines. |
| `evidenceLinks[].url` | required | Fixed absolute public `http`/`https` destination; no userinfo, fragment, credential query keys or template tokens. |
| `evidenceLinks[].source` | all evidence | Exact source name used to select the evidence period, such as `tempo/traces` or `loki/access-logs`. |
| `evidenceLinks[].params` | none | Query-parameter templates. Expanded values are URL encoded once; do not pre-encode them. |
| `evidenceLinks[].requires` | none | Context fields that must be present, in addition to every field referenced by a template. Missing context hides the link. |
| `evidenceLinks[].explore` | none | Structured Grafana Explore destination. Requires a known evidence period. |
| `evidenceLinks[].explore.type` | required with explore | `tempo` for traces or `loki` for logs. |
| `evidenceLinks[].explore.datasourceUID` | required with explore | Grafana datasource UID, not its name or backend URL. |
| `evidenceLinks[].explore.query` | required with explore | TraceQL or LogQL; substitute string literals with `{{id\|json}}`, without surrounding quotes. JSON quoting precedes panes serialization and URL encoding. |

Node context: `id`, `namespace`, `tech`. Connection context: `id` (`from->to`), `from`, `to`,
`fromNamespace`, `toNamespace`, `fromTech`, `toTech`, `protocol`. Namespace comes from `k8s.namespace`,
`namespace` or `service.namespace`. Arbitrary labels, source URLs, headers, DSNs and credentials are not
available to templates. Resource and external node identities are supported on the same basis as services.

Both targets also support `period.from`/`period.to` (RFC3339) and `period.fromMs`/`period.toMs` (Unix
milliseconds). A period comes from known sampled-span bounds, or a pinned query evaluation and lookback.
Collection time is never substituted. Node links consider their own and incident-connection evidence.
When several windows qualify, the enclosing search interval is used; this does not claim continuous
coverage. Retained evidence keeps its original period. A source filter with no dated evidence yields no
period; any link requiring it is omitted. A static dashboard without period tokens may still be shown.

See [evidence link examples and limitations](evidence-links.md). The public URLs are visible to every API
reader; use destinations protected by their own login. Source authentication is never copied into links.

## Discovery rounds

| Key | Default | Description |
|---|---|---|
| `interval` | `30s` | Time between discovery rounds. |
| `timeout` | `20s` | How long each source may take in a round. A source still busy a little after that is given up on for the round, and skipped while it is busy. |
| `retention` | 3 × `interval` | How long an observed edge stays after it was last seen, so sampled signals do not make it blink. A failing source's last good topology is reused for as long, and the status says so. Old rates, latency and readings are not reused as current health. |
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
| `health.lag` | | Limits on how far behind a queue's consumers are, or how long it went without progress. |
| `health.lag.degraded` | `1m` | From this lag a consumer's line is `degraded`. |
| `health.lag.failing` | `5m` | From this lag it is `failing`. |
| `health.restarts` | | Limits on container restarts within the source's window. |
| `health.restarts.degraded` | `1` | From this many restarts a workload is `degraded`. |
| `health.restarts.failing` | `3` | From this many it is `failing`. |
| `health.evictions` | | Cache eviction counts in the configured source lookback; policy pressure, not proof of failed requests. Tune to the intended cache policy. |
| `health.evictions.degraded` | `1` | From this count it is `degraded`. |
| `health.evictions.failing` | `100` | From this count it is `failing`. |
| `health.rejections` | | Rejected client connections in the source lookback. |
| `health.rejections.degraded` | `1` | From this count it is `degraded`. |
| `health.rejections.failing` | `10` | From this count it is `failing`. |

The count window is part of each reading's evidence. Changing the Prometheus lookback changes its count
budget. See [resource diagnostics](resource-diagnostics.md) for exporter prerequisites and the distinction
between queue backlog, age and drain estimates. Transaction age and replica delay use `health.lag`.

### Targeted health limits

`health.overrides` applies different budgets to components and directed connections. A component rule does
not change its incoming or outgoing connection rules. Match the canonical identity returned by the API,
after aliases; connection IDs are `caller->callee`. Label selectors use exact, case-sensitive equality,
require every specified key to exist, and support arbitrary inventory labels. They are not PromQL or
Kubernetes selector expressions. External services and resources work the same way as other components.

Count-based cache overrides use `health.overrides[].limits.evictions` and
`health.overrides[].limits.rejections`, with `health.overrides[].limits.evictions.degraded`,
`health.overrides[].limits.evictions.failing`, `health.overrides[].limits.rejections.degraded` and
`health.overrides[].limits.rejections.failing`. Each omitted limit inherits its global count budget.

```yaml
health:
  latency: {degraded: 500ms, failing: 2s}
  overrides:
    - name: reporting-budget
      target: node
      id: reports
      limits:
        latency: {degraded: 3s, failing: 8s}
    - name: worker-calls
      target: edge
      fromLabels: {namespace: workers}
      toLabels: {tier: database}
      priority: 10
      limits:
        latency: {degraded: 2s, failing: 10s}
    - name: specific-reporting-call
      target: edge
      id: frontend->reports
      limits:
        latency: {degraded: 3s, failing: 8s}
```

For each signal independently, an exact identity wins over any selector. Within the same matching class,
the larger integer `priority` wins. YAML order and the number of selector labels never break ties. Rules
that could overlap for the same target, matching class, priority and signal are rejected, even if their
limits currently agree; give them different priorities or disjoint selectors. Rules for distinct signals
can share a selector and priority. Rule names must be unique, nonempty and different from `global`.

A winning rule replaces only the signals it sets. Each omitted or zero limit inherits the **global** value,
not a lower-priority rule. The final degraded limit must not exceed the failing limit. Negative, non-finite
and invalid share values are rejected. An override must set at least one positive limit; zero does not
disable evaluation. Values equal to a limit cross it, just as with the global thresholds.

| Key | Default | Description |
|---|---|---|
| `health.overrides` | none | List of targeted rules. |
| `health.overrides[].name` | required | Stable rule name included in API reasons and effective rules. |
| `health.overrides[].target` | required | `node` or `edge`. |
| `health.overrides[].id` | none | Exact canonical node ID or directed connection ID. Mutually exclusive with selectors. |
| `health.overrides[].priority` | `0` | Integer precedence within identity or selector rules; larger wins. |
| `health.overrides[].labels` | none | Node label equality map; node target only. |
| `health.overrides[].fromLabels` | none | Caller label equality map; edge target only. |
| `health.overrides[].toLabels` | none | Callee label equality map; edge target only. At least one endpoint selector must be nonempty. |
| `health.overrides[].limits` | required | Signal overrides; unset values inherit global limits. |
| `health.overrides[].limits.degraded`, `health.overrides[].limits.failing` | global | Error shares, 0 to 1. |
| `health.overrides[].limits.latency` | global | Call p95 latency pair. |
| `health.overrides[].limits.latency.degraded`, `health.overrides[].limits.latency.failing` | global | Durations such as `250ms` or `3s`. |
| `health.overrides[].limits.saturation` | global | Used-capacity share pair. |
| `health.overrides[].limits.saturation.degraded`, `health.overrides[].limits.saturation.failing` | global | Shares, 0 to 1; missing capacity still cannot be judged. |
| `health.overrides[].limits.locks` | global | Waiting-session count pair. |
| `health.overrides[].limits.locks.degraded`, `health.overrides[].limits.locks.failing` | global | Counts of sessions waiting on locks. |
| `health.overrides[].limits.backlog` | global | Waiting-item count pair. |
| `health.overrides[].limits.backlog.degraded`, `health.overrides[].limits.backlog.failing` | global | Counts of queued items/consumer messages. |
| `health.overrides[].limits.lag` | global | Time-behind pair, distinct from call latency and message counts. |
| `health.overrides[].limits.lag.degraded`, `health.overrides[].limits.lag.failing` | global | Durations such as `1m` or `5m`. |
| `health.overrides[].limits.restarts` | global | Restart-count pair. |
| `health.overrides[].limits.restarts.degraded`, `health.overrides[].limits.restarts.failing` | global | Counts within the source's reported window. |

Each API node/edge contains `healthRules`: one effective `{cause, name, match, priority, degraded, failing,
unit}` per supported signal, including global fallback and unmeasured signals. `match` is `global`, `identity`
or `selector`; `global` includes the built-in defaults plus configured global limits. Reasons additionally
carry `rule`, `unit` and signal-specific `evidence`, alongside measured `value`, crossed `limit`, coverage
and approximation flags. Grafana tables encode `healthRules` and `reasons` as JSON arrays, preserving the
same contract. Saturation comparisons use shares, latency uses milliseconds, time lag uses seconds, and
locks/backlog/restarts use counts. Each evidence item retains its own source and effective period; missing
periods remain unknown and retained evidence is not attached to a current health reason.

These limits classify observed signals, not transaction SLO compliance. Weighted percentiles remain
explicit approximations and partial error coverage remains qualified. A rule does not create missing data:
absent latency, errors, or capacity stays unavailable, and a component without an interpretable signal
stays `unknown`. Traffic rate is not an error or latency threshold; low and high traffic keep the same
configured units and the existing measurement qualifications.

## Notifications

| Key | Default | Description |
|---|---|---|
| `notify` | | Sends the changes of each round to webhooks. |
| `notify.webhooks` | none | List of webhooks. |
| `notify.webhooks[].url` | required | `http` or `https` URL. It is never quoted in errors or logs (for Slack, the URL is the secret). |
| `notify.webhooks[].format` | `json` | `json` (the changes as JSON) or `slack` (an incoming-webhook message). |
| `notify.webhooks[].events` | all | Change types to send: `edge.added`, `edge.removed`, `edge.state`, `edge.health`, `node.added`, `node.removed`, `node.health`, `node.visibility`, `finding.opened`, `finding.resolved`. |
| `notify.webhooks[].headers` | none | Map of extra request headers, such as `Authorization`. |

## Clusters

One teia can map several clusters. Each cluster is discovered **on its own**, from its own inventory and telemetry:
services with the same name in two clusters stay two services, each with its own traffic, errors, latency and health.
Without `clusters`, every source describes one scope, as before.

```yaml
clusters:
  - name: prod-east
    sources:
      kubernetes: {kubeconfig: /etc/teia/kube/prod-east.yaml}
      prometheus: [{url: https://prometheus.prod-east.example.com}]
      tempo: [{url: https://tempo.prod-east.example.com}]
  - name: prod-west
    sources: {...}
sources:
  declared: {paths: [./flows]}   # shared: declarations are not per cluster
```

| Key | Default | Description |
|---|---|---|
| `clusters` | none | The clusters teia maps, in the order the UI lists them; the first is the default. Each one gets its own discovery round, snapshot, history and activity. See [Kubernetes](kubernetes.md#several-clusters-from-one-teia). |
| `clusters[].name` | required | Unique name shown in the UI and API (`?cluster=`), on `/metrics` series (`cluster="…"`), in webhook messages and MCP answers. 1–128 bytes, without `,` `:` `=` `/`. |
| `clusters[].sources` | required | The cluster's sources, with the same keys as `sources` below. Source names only need to be unique within a cluster. |

With `clusters`, the top-level `sources` holds only `declared`, which applies to every cluster; a telemetry or
inventory source there would be read into every cluster and mix them, so it is rejected. `history.directory` gets one
subdirectory per cluster, and each cluster's history has the configured limits.

Why not one map: two backends that observe the **same** calls (a mesh and traces) are merged per connection, and the
busiest observation wins, so nothing is counted twice. Two clusters observe **different** calls; merging them would
show one cluster's numbers as the other's. Calls between clusters show as calls to an external or unknown component
of the calling cluster.

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
| `sources.prometheus[].probes` | `[auto]` | Component health to read from standard exporters (memory, connections, consumer lag, locks, restarts). Empty or `[auto]` detects which exist; `[none]` turns them off. Names: `redis`, `kafka`, `rabbitmq`, `mongodb`, `mysql`, `postgres-exporter`, `containers`, `restarts`. See [Component health](sources.md#component-health). |
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
| `sources.tempo[].dimensions` | none | Map of business dimension to span attribute, such as `journey: business.journey`. A span without the attribute inherits it from its closest ancestor in the same trace. Each combination becomes a sampled slice of the edge (sample count and sampled p50/p95, no rate or errors). See [slices](journeys.md#slices-follow-the-calls). |

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
| `sources.queries[].query` | required | The query. `$window` is replaced by `window` as a PromQL duration (`5m`, `90s`), so write `[$window]`. Its value is requests (or log lines) per second. |
| `sources.queries[].errors` | none | Failed requests per second, with the same `from` and `to` labels (other labels may differ: series of one pair are added up). Without it, the edges' failures are **unknown**: they are not judged healthy, and the advisor asks for this query. |
| `sources.queries[].latency` | none | The p95 of calls in **seconds**, with the same `from` and `to` labels, for example a `histogram_quantile(0.95, ...)`. |
| `sources.queries[].window` | `5m` | Value of `$window` and the configured lookback reported in evidence. Hardcoded ranges, `offset` and `@` are not parsed to verify the query's effective interval. Addresses seen over the configured window are only resolved to a workload that held them for all of it. |
| `sources.queries[].from` | required | Label holding the caller. |
| `sources.queries[].to` | required | Label holding the callee. |
| `sources.queries[].fromAddress` | `false` | The `from` label is an IP address (a port is dropped), resolved through the inventory. |
| `sources.queries[].toAddress` | `false` | The same for `to`. |
| `sources.queries[].protocol` | empty | Protocol of every edge, such as `http`. |
| `sources.queries[].protocolLabel` | empty | Label holding the protocol; `protocol` is used for series without it. |
| `sources.queries[].toKind` | `unknown` | Kind of the callees: `service`, `database`, `cache`, `queue`, `external` or `unknown`. |
| `sources.queries[].headers` | none | Map of request headers. |
| `sources.queries[].dimensions` | none | Map of business dimension to series label, such as `journey: journey`. `query`, `errors` and `latency` must keep those labels in their grouping. Each combination becomes a slice of the edge with its own rate, error share and p95; the edge itself stays the total. See [slices](journeys.md#slices-follow-the-calls). |

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
