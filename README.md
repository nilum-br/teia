# teia

**Your architecture diagram is lying somewhere. teia tells you where, and what to do about it.**

teia maps who talks to whom across your platform, compares it with what teams *declared*, and,
unlike a plain service map, tells you exactly where it is blind and which grant, adapter or
instrumentation would let it see more.

> _teia_ is Portuguese for "web": the mesh of threads that holds everything together.

[Português](README.pt-BR.md)

## Why

Service maps are a commodity: Grafana, Kiali, Hubble and every APM draw one. What none of them does
is answer these questions:

- **Drift.** Which communications happen that nobody declared, and which declared ones never happen?
- **Coverage.** How much of the picture am I *not* seeing, and what exactly would fix that?
- **Contract.** Where is the reviewed, versioned list of "service A may call B on port X, because Y"
  that security, network and platform teams can all work from?

teia merges three things into one graph:

| Layer | Where it comes from | Example |
|---|---|---|
| **Declared** (intent) | `Flows` files next to each service's `catalog-info.yaml` | `orders` calls `payments` over HTTP to charge the customer |
| **Observed** (reality) | adapters over what you already run: OpenTelemetry / Tempo service graph metrics, inventory, the databases themselves | `legacy-reports` holds a connection to `orders-db` |
| **Drift** | the difference | ghost: observed but undeclared · stale: declared but not observed |

On top of that, the **advisor** crawls outwards and explains every blind spot with a concrete next step:

```text
 1. [warning/permission] Reached database orders-db but cannot see who else connects to it
    teia is connected to orders-db as "teia" but PostgreSQL hides where other sessions come from.
    Right now 2 session(s) are hidden, from roles: orders (1), reports (1).
    -> Grant the built-in monitoring role. It is read-only: it exposes session metadata, not table data.
       GRANT pg_monitor TO "teia";
```

Apply it, and the next round finds the job nobody instrumented, then tells you how to see inside it.
Each round of fixes widens what the next round can see.

## Try it in 5 minutes

You only need Docker. The lab is a small shop with the blind spots real platforms have: a third-party
gateway without telemetry, a Redis cache, a PostgreSQL database and a legacy job that only the database
knows about.

```sh
docker compose -f lab/compose.yaml up -d --build
```

| What | Where |
|---|---|
| teia UI (graph, next steps, export) | http://localhost:8480 |
| Grafana (Node Graph + findings table) | http://localhost:3480 |
| Prometheus | http://localhost:9480 |

Then follow the advisor, like you would in production:

```sh
# 1. what does teia see, and what is it missing?
docker compose -f lab/compose.yaml exec teia teia discover

# 2. do what the first finding says
docker compose -f lab/compose.yaml exec orders-db psql -U postgres -d orders -c 'GRANT pg_monitor TO teia;'

# 3. look again: legacy-reports shows up, with the next step to see inside it
docker compose -f lab/compose.yaml exec teia teia discover

# flow matrix for a firewall request (CSV, Portuguese headers)
docker compose -f lab/compose.yaml exec teia teia export matrix --lang pt-BR --declared-only
```

`make lab-up`, `make lab-report`, `make lab-grant` and `make lab-revoke` do the same if you have `make`.
Behind a TLS-inspecting proxy? Drop its CA as a `.crt` in `certs/` before building.

## Declaring flows

```yaml
# catalog-info.yaml, next to the Backstage Component (or in its own file)
apiVersion: teia.nilum.io/v1alpha1
kind: Flows
metadata:
  name: orders
spec:
  owner: component:default/orders
  egress:
    - to: component:default/payments
      protocol: http
      port: 8080
      reason: charge the customer
    - to: resource:default/orders-db
      kind: database
      protocol: postgresql
      port: 5432
      reason: persist orders
```

Nobody should write these from scratch: for a service without a file, the advisor generates one from
observed traffic, ready to review and commit. `teia validate` checks files in CI and editors get completion from
[`schema/flows.schema.json`](schema/flows.schema.json). See [docs/flows-spec.md](docs/flows-spec.md).

## Status

**Early, v0 skeleton.** What works today:

- engine in Go: merge, identity resolution through inventory addresses and aliases, drift, coverage scores
- sources: `declared` (flows files), `servicegraph` (Prometheus with OTel Collector or Tempo service graph
  metrics, so Beyla works too), `docker` (inventory), `postgres` (`pg_stat_activity` explorer)
- advisor with stable rule IDs and copy-paste fixes ([docs/advisor.md](docs/advisor.md))
- `teia serve | discover | export matrix | validate`, JSON API, built-in UI, Grafana Node Graph endpoints
- the lab above

Next: Kubernetes inventory, Backstage plugin (entity card + catalog relations), `teia diff` comments on
PRs, Istio and Hubble adapters. See [docs/roadmap.md](docs/roadmap.md) and how it compares to existing tools in
[docs/landscape.md](docs/landscape.md).

## Design principles

- **Read what you already run.** No new agent, no eBPF of our own: teia consumes OpenTelemetry, Prometheus,
  Beyla, Hubble, the Kubernetes API and the datastores themselves, with read-only credentials.
- **Never call silence absence.** A declared flow is only "stale" when teia can actually see the caller.
- **Every finding has a next step**, with the least privilege that unlocks it.
- **Calm UI.** State is carried by color and line style, never by motion.

## Architecture

```
 sources (adapters)                engine (Go)                         surfaces
 declared  flows files ──┐
 servicegraph (Prom)  ───┤      ┌──────────────────────────┐      ┌──────────────────────────────┐
 docker / k8s inventory ─┼────▶ │ merge + identity + drift │─API─▶│ built-in UI · Grafana        │
 postgres explorer ──────┘      │ coverage + advisor       │─CLI─▶│ discover · export · validate │
                                └──────────────────────────┘      │ Backstage plugin (next)      │
                                                                  └──────────────────────────────┘
```

Details in [docs/architecture.md](docs/architecture.md).

## Contributing

Issues and PRs are welcome, especially new sources and explorers (MySQL, Redis, Kafka, Istio, Hubble, Kubernetes).
See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache-2.0. Built by [Nilum](https://nilum.com.br) and contributors.
