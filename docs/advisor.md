# The advisor

Every finding answers three questions: **what** teia cannot see or what disagrees, **why** it matters,
and **the next step**, with the least privilege that unlocks it. Each round of fixes widens what the next
discovery round can see.

Rule IDs are stable. You can filter on them, and they will not be renamed between versions. A finding's
`id` is `<rule>:<subject>`.

## Rules

| Rule | Kind | Severity | Fires when | Next step |
|---|---|---|---|---|
| `source-error` | source | critical | a source failed this round | check reachability and config |
| `servicegraph-no-metrics` | source | warning | Prometheus has no `traces_service_graph_request_total` | enable the OTel Collector `servicegraph` connector or the Tempo metrics-generator |
| `servicegraph-denied` | permission | critical | Prometheus answered 401/403 | read-only credential via `headers` |
| `servicegraph-unnamed-target` | blind-spot | info | client spans with no matching server span and no `peer.service` / `db.*` | set `peer.service`, raise `store.ttl` |
| `invalid-flows-file` | source | warning | a flows file does not parse or validate | `teia validate` |
| `docker-socket` | permission | warning | the Docker socket is missing or not readable | mount it read-only |
| `postgres-login` | permission | warning | teia cannot log into a database | dedicated login with `pg_monitor` |
| `postgres-privilege` | permission | warning | logged in, but without `pg_read_all_stats`, so other sessions' origins are hidden | `GRANT pg_monitor TO teia;` |
| `no-telemetry` | blind-spot | warning | a workload has no telemetry of its own (seen only via peers, or not at all) | OTel SDK env vars, or Beyla (eBPF) |
| `datastore-unexplored` | blind-spot | info | a database, cache or queue is reached but no explorer looks inside it | postgres explorer config + SQL; other systems: declare clients meanwhile |
| `unresolved-address` | blind-spot | warning | an address talks to something and no inventory knows it | enable an inventory, or alias it |
| `no-inventory` | blind-spot | info | no inventory source is configured | enable Docker (Kubernetes soon) |
| `undeclared-flow` | drift | warning | observed communication is not declared | a generated flows file for owners without one, an `egress` entry otherwise |
| `stale-flow` | drift | info | a declared call was not observed **and the caller has full visibility** | remove it, or keep it if it is rare |

## Why least privilege matters here

teia asks for read-only access to systems people are protective of. Every fix names the narrowest grant:

- **PostgreSQL**: `pg_monitor` exposes session metadata and statistics, not table data.
- **Prometheus**: query API only.
- **Docker**: read-only socket in the lab. On Kubernetes this becomes `get/list/watch` on pods and services.

## Writing a rule

A rule is a function from `advisor.Input` (the merged graph and the source statuses) to findings:

```go
func myRule(in Input) []model.Finding { ... }
```

Add it to `advisor.Rules` and cover it in `advisor_test.go`. Findings a source raises about itself (its own
permissions) belong in the source instead, where the error details are.
