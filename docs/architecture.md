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

A source that returns an error is reported as `error` and contributes nothing to that round.

Every source is read-only and talks to systems you already run. teia ships no agent.

## Identity

Everything ends up keyed by a **node ID**, which is a plain name such as `orders`.

| Input | Becomes |
|---|---|
| `component:default/orders` in a flows file | `orders` |
| OTel `service.name=orders` | `orders` |
| compose service `orders` or label `teia.node=orders` | `orders` |
| `ip:172.18.0.14`, seen by the postgres explorer | `orders`, if an inventory source owns that address |
| anything listed in `aliases:` | its alias target |

Unresolved addresses stay as `ip:...` nodes and trigger the `unresolved-address` finding. This is the
riskiest part of the design: poolers (PgBouncer), SNAT, sidecars and recycled pod IPs all blur
identity. The plan is to add confidence and provenance per resolution, and a Kubernetes inventory with
IP history.

## Merge rules

- **Kind** comes from the most trusted opinion: an operator label (`teia.kind`) beats a flows file, which
  beats a guess from traffic (`db.system` on client spans).
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
| `GET /` | built-in UI |

The API is unauthenticated and meant to sit behind your platform's ingress or Backstage's proxy.
