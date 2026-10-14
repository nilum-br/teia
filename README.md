<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img src="docs/assets/logo-light.svg" alt="teia" width="260">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/nilum-br/teia/actions/workflows/ci.yml"><img src="https://github.com/nilum-br/teia/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-005aff" alt="Apache-2.0"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/nilum-br/teia?color=005aff" alt="Go version"></a>
  <a href="plugins/grafana"><img src="https://img.shields.io/badge/Grafana-panel-a9f549?labelColor=020f59" alt="Grafana panel"></a>
</p>

**Your architecture diagram is lying somewhere. teia tells you where, and what to do about it.**

teia maps who talks to whom across your platform from the telemetry you **already** have (traces, mesh metrics,
eBPF flows, access logs, the databases themselves), compares it with what teams *declared*, and, unlike a plain
service map, tells you exactly where it is blind and which grant, adapter or instrumentation would let it see more.
It runs as a single binary on Linux, macOS and Windows, and shows up where your team already looks: **Grafana**.

> _teia_ is Portuguese for "web": the mesh of threads that holds everything together.

[Português](README.pt-BR.md)

![teia's map of the lab: services, datastores and jobs with their health, live traffic, drift and the next steps to see more](docs/assets/screenshots/teia-map.png)

## Why

Service maps are a commodity: Grafana, Kiali, Hubble and every APM draw one. None of them answers:

- **Drift.** Which communications happen that nobody declared, and which declared ones never happen?
- **Coverage.** How much am I *not* seeing, and what exactly would fix that?
- **Contract.** Where is the reviewed list of "A may call B on port X, because Y" that security, network and platform teams can all work from?

teia merges three layers into one graph:

| Layer | Where it comes from |
|---|---|
| **Declared** (intent) | `Flows` files next to each service's `catalog-info.yaml` |
| **Observed** (reality) | whatever the platform already runs: OpenTelemetry / Tempo, Istio / Linkerd, Beyla / Hubble / Retina, Loki access logs, Kubernetes or Docker inventory, the databases |
| **Drift** | ghost: observed but undeclared · stale: declared but not observed |

The **advisor** then crawls outwards ("fiando a teia") and explains every blind spot with a concrete next step:

```text
[warning/permission] Reached database orders-db but cannot see who else connects to it
    Right now 2 session(s) are hidden, from roles: orders (1), reports (1).
    -> GRANT pg_monitor TO "teia";
```

Apply it and the next round finds the legacy job nobody instrumented, then tells you how to see inside it.

## Agnostic by design

- **No mesh required, no agent shipped.** A mesh is one signal among several; see [docs/sources.md](docs/sources.md).
- **Auto-detection.** Point teia at Prometheus and it finds service graph metrics, Istio, Linkerd, Beyla/OBI, Hubble and Retina on its own (10 presets). `teia detect` prints what your platform offers and a starting config.
- **Anything else** becomes edges through a PromQL or LogQL query plus a label mapping.
- **Mixed platforms are normal:** part Istio, part OpenTelemetry, part nothing. Each edge records which signals saw it, and "unknown callers" are explained per signal.
- **Identity you can trust:** IPs are resolved through inventory, and recycled IPs are never attributed to the wrong workload.

## What you see

The map, in Grafana (`nilum-teia-panel`) or in teia's own UI, is the same component:

![The teia panel inside a Grafana dashboard](docs/assets/screenshots/grafana-panel.png)

- **Golden signals, and why:** each card and line shows its traffic, p95 latency and error rate. Amber or red is the worst signal past its limit, and a tag on the card or line says which one and where it most likely comes from, so you see the cause at a glance, not only the colour: `p95 900 ms from inventory`, `4.4% errors`, `3 on locks held by legacy-reports`, `1.5k messages waiting`, `7 min behind`, `93% memory`.
- **Every kind of component:** services (errors, latency, CPU throttling, memory, restarts), relational databases (connections, sessions waiting on locks and who holds them), MongoDB, Redis (memory, clients) and queues (Kafka lag in messages and in time, consumers without progress, RabbitMQ backlog), read from the exporters you already run.
- **Live traffic:** dots travel along each communication at its speed, and red dots are errors. Updates arrive live (Server-Sent Events, or the dashboard refresh in Grafana).
- **Drift and visibility:** dashed means undeclared, dotted means declared but not seen; card borders say how first-hand teia's view is.
- **One [icon library](docs/icons.md):** microservices, relational and document databases, caches, Kafka streams, queues, gateways, jobs, external SaaS. The technology is detected from spans, images or labels.
- **Labels:** group by `team`, `system`, `k8s.namespace` or any label; search with `team:checkout`, `tech:kafka`.
- **Next steps, live feed, export** (PNG, SVG, flow matrix CSV for firewall requests).
- **Keyboard and screen readers:** every card and control is reachable with Tab; `F` fits, `/` searches, `Space` toggles the animation, which also follows the reduced-motion setting.

| Amber or red says why, and where it comes from | Group by any label, in dark or light |
|---|---|
| ![recommendations is amber: its p95 of 739 ms comes from inventory, with a button to go there](docs/assets/screenshots/teia-details.png) | ![The lab grouped by team, light theme](docs/assets/screenshots/teia-groups-light.png) |

Views are shareable links: `?group=team`, `?q=kind:database`, `?select=orders-db`, `?theme=light`, `?flow=1`, `?legend=icons`.

<p align="center"><img src="docs/assets/icon-library.svg" alt="The teia icon library: one pictogram per kind of component" width="820"></p>

Changes also go out as **webhooks** (JSON or Slack) and **Prometheus metrics** (`/metrics`) for alerting.

## How it works

<p align="center"><img src="docs/assets/architecture.svg" alt="Sources teia reads, the discovery round, and where the result shows up" width="900"></p>

teia is read-only and keeps no database: every interval it asks each source what it saw, merges the answers into
one graph with stable identities, compares it with the declared flows, scores health and coverage, and asks the
advisor what would make the picture more complete. The result is served as JSON, a live stream, metrics and webhooks.

## Try it in 5 minutes

You only need Docker (Linux, macOS or Windows). The lab is a small shop with the mix real platforms have: part
OpenTelemetry, part Istio mesh without OpenTelemetry, a managed Kafka and MongoDB seen only through client spans, a
third-party gateway, Redis, PostgreSQL, and jobs nobody instrumented.

```sh
docker compose -f lab/compose.yaml up -d --build
```

| What | Where |
|---|---|
| **Grafana with the teia panel** | http://localhost:3480/d/teia-overview |
| teia's own UI | http://localhost:8480 |
| Prometheus | http://localhost:9480 |

Follow the advisor, like you would in production:

```sh
docker compose -f lab/compose.yaml exec teia teia discover          # what teia sees and what it misses
docker compose -f lab/compose.yaml exec orders-db psql -U postgres -d orders -c 'GRANT pg_monitor TO teia;'
docker compose -f lab/compose.yaml exec teia teia discover          # the hidden legacy job shows up
docker compose -f lab/compose.yaml exec teia teia export matrix --lang pt-BR --declared-only
docker compose -f lab/compose.yaml run --rm e2e                     # the end-to-end check CI runs
```

Behind a TLS-inspecting proxy? Drop its CA as a `.crt` in `certs/` before building.

## Install

- **From source** (today): `go build ./cmd/teia` (Go 1.27+). Docker and Kubernetes endpoints follow `DOCKER_HOST` / kubeconfig, including Docker Desktop, Colima, OrbStack, Podman and Windows named pipes.
- **Releases** (from the first tag on): binaries for Linux, macOS and Windows (amd64, arm64), a multi-arch image at `ghcr.io/nilum-br/teia` and the Grafana panel zip.
- **Grafana panel:** see [plugins/grafana](plugins/grafana).

Then `teia detect` prints what your platform offers and a starting `teia.yaml`, and `teia serve` runs the API and
the UI. Before exposing the API beyond localhost, set `auth.token` (and `cors.allowedOrigins` for the Grafana
panel's direct mode): see [Configuration](docs/configuration.md) and [Exposing the API](docs/architecture.md#exposing-the-api).

## Declaring flows

```yaml
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

Nobody should write these from scratch: for a service without a file, the advisor generates one from observed
traffic. See [docs/flows-spec.md](docs/flows-spec.md).

## Docs

[Signals and sources](docs/sources.md) · [Configuration](docs/configuration.md) · [Architecture](docs/architecture.md) ·
[Advisor rules](docs/advisor.md) · [Icon library](docs/icons.md) · [Flows spec](docs/flows-spec.md) · [Roadmap](docs/roadmap.md) ·
[Landscape](docs/landscape.md)

## Status

v0, moving fast. What works today is exercised end to end by the lab check in CI. Next: Backstage plugin
(entity card plus catalog relations), `teia diff` comments on pull requests, a teia data source for Grafana, and
more database explorers.

## Contributing and license

Issues and PRs welcome; see [CONTRIBUTING.md](CONTRIBUTING.md), and report vulnerabilities privately as described in
[SECURITY.md](SECURITY.md). Apache-2.0. Built by [Nilum](https://nilum.com.br) and contributors.
