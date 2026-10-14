# Architecture

teia is one Go binary. Each discovery round runs every configured source in parallel, merges what they
return into one graph, and runs the advisor on that graph.

```
            ┌─────────────── one round (every `interval`) ───────────────┐
 sources ──▶│ Discover() ×N ─▶ graph.Build ─▶ advisor.Advise ─▶ Snapshot │──▶ API / UI / CLI / export
            └─────────────────────────────────────────────────────────────┘
```

## Packages

| Package | Role |
|---|---|
| `internal/model` | Shared types: `Node`, `Edge`, `Finding`, `Fix`, `SourceStatus`, `Coverage`, `Snapshot` |
| `internal/source` | The `Source` interface and raw observation types |
| `internal/source/declared` | Reads `Flows` documents (intent) |
| `internal/source/prometheus` | Prometheus-compatible metrics with auto-detected presets (service graph, Istio, Linkerd, Beyla/OBI, Hubble, Retina) |
| `internal/source/tempo` | Samples traces from Tempo and pairs spans into edges |
| `internal/source/query` | Any PromQL or LogQL query mapped to edges (access logs in Loki, custom exporters) |
| `internal/source/promapi` | Instant-query client shared by Prometheus and Loki sources |
| `internal/source/kubernetes` | Inventory: workloads, pod and service IPs, labels, images |
| `internal/source/docker` | Inventory: containers over unix sockets, TCP or Windows named pipes |
| `internal/source/postgres` | Reads `pg_stat_activity` from inside a database (who connects) |
| `internal/graph` | Merge, identity (time-aware address resolution), drift, visibility, coverage, health, technology, retention |
| `internal/advisor` | Rules that turn the graph into findings with a next step |
| `internal/engine` | The discovery loop, snapshots, change events, subscribers and notifiers |
| `internal/notify` | Webhooks (JSON, Slack) |
| `internal/api` | JSON API, SSE stream, `/metrics`, Grafana frames, built-in UI (`ui/teia-ui.js`, shared with the Grafana panel) |
| `internal/export` | Flow matrix (CSV) |
| `cmd/teia` | `serve`, `discover`, `detect`, `export matrix`, `validate` |
| `plugins/grafana` | `nilum-teia-panel`, the Grafana panel plugin |

## Sources

A source answers one question: *what do you see right now?*

```go
type Source interface {
    Name() string
    Type() string
    Discover(ctx context.Context) (Observation, error)
}
```

An `Observation` carries:

- **nodes**, flagged `SelfReported` when the node emitted the data itself (its own spans), plus any
  `Addresses` an inventory knows for it;
- **edges**, flagged `Declared` for intent and unflagged for traffic;
- **findings** the source raises about itself (missing grant, refused credentials, missing metrics);
- **explores**: node IDs this source can look *inside* (a database explorer explores its database), so the
  advisor stops suggesting it;
- **degraded**: the round worked but saw less than it could.

A source that returns an error, panics or ignores its `timeout` is reported as `error`. So that one bad round
(a broken flows file, an inventory restart) does not turn every `ok` edge into a ghost or every name back into an
address, teia keeps using the source's last good observation for up to `retention` and says so in the status
(`failed: ...; using the last good observation from 40s ago`). After that, the source contributes nothing until it
recovers. A source that has not answered a little after `timeout` is given up on for the round, and skipped while
it is still busy.

Every source is read-only and talks to systems you already run. teia ships no agent. Where the API it reads can
do more than read, give teia a narrower door: an RBAC role for Kubernetes, a socket proxy for Docker (see
[Docker access](sources.md#docker-access)). Every config key is in [configuration.md](configuration.md).

## Identity

Everything ends up keyed by a **node ID**, which is a plain name such as `orders`.

| Input | Becomes |
|---|---|
| `component:default/orders` in a flows file | `orders` |
| OTel `service.name=orders` (traces, service graph metrics) | `orders` |
| a workload label in mesh or eBPF metrics, such as Istio `source_workload="orders"` | `orders` |
| a Kubernetes pod of Deployment `orders` (ReplicaSet → Deployment, Job → CronJob), or annotation `teia.nilum.io/node-id: orders` | `orders` |
| compose service `orders` (else the container name), or label `teia.node=orders` | `orders` |
| `ip:172.18.0.14`, from the postgres explorer or a query with `fromAddress` / `toAddress` | `orders`, if an inventory says `orders` held that address for the whole time window |
| a name or `ip:` address listed in `aliases:` | its alias target |

IDs are case-insensitive: every source's names are lowercased (OTel `OrderService` and a flows file's
`orderservice` are one node), and `aliases:` and `ignore:` match whatever the case. `ip:...` IDs are
kept as they are.

Unresolved addresses stay as `ip:...` nodes and trigger the `unresolved-address` finding. This is the
riskiest part of the design: poolers (PgBouncer), SNAT, sidecars and recycled pod IPs all blur
identity. Inventories report since when each address is held, per pod and per Service ClusterIP, so a
rollout only makes the new pods' addresses ambiguous for a while. The plan is to add confidence and
provenance per resolution.

**Known limitation: the namespace is not part of the ID.** `redis` running in ten namespaces (or a
`redis` compose service in two projects) becomes one node, with every caller and address of all ten.
Set `teia.nilum.io/node-id` (Kubernetes) or `teia.node` (Docker) to tell them apart for now. The planned
option is namespace-qualified IDs (`shop/redis`).

## Merge rules

- **Kind** comes from the most trusted opinion: an operator label (`teia.kind`, `teia.nilum.io/kind`)
  beats a flows file, which beats a guess from traffic (`db.system` on client spans). Without that label an
  inventory has no opinion; if nobody has one, the technology (from the image, for instance) decides.
- **External**: a node no inventory lists, that only its callers report, is marked external, but only when
  every inventory answered in full; a failed or degraded inventory leaves it `unknown`.
- **Visibility** of a node:
  - `full`: it reports its own telemetry, or an explorer can see inside it;
  - `partial`: it is only seen through peers;
  - `none`: it is known to exist, but no traffic was seen.
- **Edge state**:
  - `ok`: declared and observed;
  - `ghost`: observed but not declared;
  - `stale`: declared but not observed.
- **Ignore**: `ignore:` in the config or `teia.ignore=true` on a container drops a node, its edges and
  the findings about it.

## Health and its reasons

A colour alone says something is wrong, not what. So every node and edge carries **reasons**: the golden signals
past their limits, each with its value, the limit it crossed and a summary (`p95 900 ms, over 500 ms`). Its health
is the worst of them: `degraded` (amber) or `failing` (red); `healthy` when it has traffic and no reason;
`unknown` when nothing was measured.

| Cause | Measured as | Limits (`health` in teia.yaml) |
|---|---|---|
| `errors` | failed share of calls | 1% amber, 5% red |
| `latency` | p95 of calls | 500 ms amber, 2 s red |
| `saturation`, `backlog`, `locks` | reserved for datastore, broker and resource signals | |

An edge is judged on its own calls. A node is judged on the calls it serves: their error rate and their p95,
weighted by each caller's traffic (a node nobody calls is judged on the calls it makes, without latency).

**Where it comes from.** A caller that waits on a slow callee is slow too, and so is its own caller. To point at
the origin rather than light up the whole chain, a latency or error reason names its likely source (`via`): the
node's outgoing call with the worst value, when that call accounts for most of it (70% of the p95, half of the
error rate). An edge into such a node inherits the same `via`. The map shows it as `p95 900 ms from inventory`.
The rule is deliberately plain and one hop deep: it is a lead, not a verdict.

Reasons reach the map (a tag on each amber or red card and line), the details panel, the Grafana frames
(`reasons`, `detail__why`), change events and webhooks (`orders → payments is degraded (3.7% errors, over 1.0%)`)
and `teia discover`.

## Coverage

- **visibility**: share of non-external nodes with full visibility.
- **declared**: share of observed edges that are declared.
- **observed**: share of declared edges that were observed.

All three are deliberately simple: a team should be able to compute them by hand from the graph.

## The advisor

Rules are plain functions over the merged graph and the source statuses. See [advisor.md](advisor.md).
The guiding rule is **never call silence absence**: a declared edge is only flagged stale when its caller
has full visibility. Otherwise teia cannot tell "not happening" from "not visible".

## API

| Endpoint | Content |
|---|---|
| `GET /api/v1/snapshot` | everything below in one document |
| `GET /api/v1/graph` | nodes and edges |
| `GET /api/v1/findings` | findings, most urgent first |
| `GET /api/v1/coverage` | coverage scores |
| `GET /api/v1/sources` | per-source status of the last round |
| `GET /api/v1/export/matrix.csv?lang=pt-BR&declared=true` | flow matrix |
| `GET /api/v1/changes` | recent changes between rounds |
| `GET /api/v1/stream` | Server-Sent Events: one snapshot per round |
| `GET /api/v1/grafana/{nodes,edges,findings,sources,changes}` | flat frames for Grafana (teia panel, Node Graph, tables) |
| `GET /metrics` | Prometheus metrics: edges, nodes, coverage, findings, sources |
| `GET /healthz` | liveness |
| `GET /` | built-in UI (`teia serve --ui-dir DIR` serves it from disk while you work on it) |

Finding changes (`finding.opened`, `finding.resolved`) carry the finding's `id`, so the two can be matched when
several rules share a subject. Health changes are reported against the last known state: a round without rates
(`unknown`) is silent, and `failing → unknown → failing` alerts once.

### Exposing the API

```yaml
listen: :8080                  # all interfaces; 127.0.0.1:8080 for this host only
auth:
  token: ${TEIA_TOKEN}         # every endpoint but /healthz needs it
cors:
  allowedOrigins: [https://grafana.example.com]
maxStreams: 100                # concurrent /api/v1/stream clients; more get 503 + Retry-After
```

- `listen` binds every interface by default. Set `auth.token`, or keep teia on a private network (or both).
- With `auth.token`, clients send `Authorization: Bearer <token>`; that includes `/metrics` (Prometheus:
  `authorization: { credentials: <token> }` in the scrape config) and the Grafana Infinity data source (a header).
  Tokens are compared in constant time.
- The built-in UI cannot send headers on its live stream, so a browser opens `/?token=<token>` once: teia sets an
  `HttpOnly`, `SameSite=Lax` session cookie (derived from the token, not the token itself) and redirects to the
  clean URL. Opening the UI behind your platform's own auth (an ingress with SSO, Backstage's proxy) works too.
- `${NAME}` that is not set is a config error, so a missing `TEIA_TOKEN` cannot quietly turn auth off. Write
  `${TEIA_TOKEN:-}` to make it optional on purpose.
- `cors.allowedOrigins` lets browser pages on those exact origins read `/api/v1/*`, including the stream (the
  Grafana panel's direct mode). `"*"` allows any origin and must be listed explicitly. Cookies are never accepted
  across origins, and the panel's live stream cannot send a bearer token: with `auth.token` set, feed the panel from
  dashboard queries instead of direct mode.
