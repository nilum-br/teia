# Roadmap

teia wins on what nobody else does: **drift between declared and observed**, an **advisor that turns blind
spots into exact next steps**, and a **flow matrix** security teams can act on. Drawing the graph and
per-edge RED metrics is what Grafana, Kiali and the APMs already do well, so teia links to them instead of
competing. See [landscape.md](landscape.md).

## v0.1: useful on a real platform

- [x] Engine: merge, drift, coverage, advisor with stable rule IDs
- [x] Sources: declared, Docker inventory, postgres explorer
- [x] CLI: `serve`, `discover --fail-on`, `detect`, `export matrix`, `validate`
- [x] Built-in UI; flat Grafana frames (`/api/v1/grafana/*`) for the teia panel and the Node Graph panel
- [x] Lab with staged blind spots; Docker access through a socket proxy, teia as non-root
- [x] Strict config (unknown keys are errors), `${VAR:-default}`, API token, CORS, stream cap
- [x] **Kubernetes inventory**: pods, services, owners, IP → workload; RBAC finding when forbidden
- [x] Agnostic Prometheus source with 10 auto-detected presets (service graph, Istio, Linkerd, Beyla/OBI, Hubble, Retina)
- [x] Tempo (traces without the metrics-generator) and generic PromQL/LogQL query sources (access logs in Loki)
- [x] Health per node and edge, live stream (SSE), change events, webhooks (JSON, Slack), `/metrics`
- [x] Grafana panel plugin `nilum-teia-panel` sharing the map component; icon library with technology detection
- [x] Recycled IPs are never attributed to the wrong workload; annotation `teia.nilum.io/node-id`
- [x] Binaries for Linux, macOS, Windows (amd64, arm64); multi-arch image; CI on all three OSes; lab end-to-end check in CI
- [ ] Identity confidence and provenance per resolution; IP history in the Kubernetes inventory (today it knows
      since when each current address is held, not who held it before)
- [ ] `teia diff` for pull requests: drift introduced by a change, as a PR comment
- [ ] Persist aggregated edges (first seen, last seen, sources) so short windows do not erase history; today edges
      live in memory for `retention`
- [ ] Helm chart

## v0.2: where developers work

- [ ] **Backstage backend module**: emits `teia-talksTo` / `teia-talkedToBy` relations, so the existing
      catalog graph shows observed communication without new UI
- [ ] **Backstage entity card**: neighbours, ghost and stale edges, visibility (new and legacy frontend systems)
- [ ] "Accept as declared" opens a PR with the generated flows entry
- [ ] `expectedFrequency` in flows, for rare flows
- [ ] Findings in Portuguese (`pt-BR`)

## v0.3: from contract to enforcement

- [ ] Generate NetworkPolicy, CiliumNetworkPolicy and Istio AuthorizationPolicy from **declared** flows,
      with a "what would break" report based on observed traffic
- [ ] Flow matrix as XLSX, with templates for firewall change requests
- [ ] teia data source for Grafana (Go backend), so dashboards need no Infinity data source; signing and catalog
      listing for the panel and the data source
- [ ] Explorers: MySQL, Redis (`CLIENT LIST`), Kafka consumer groups

## Not planned

- Our own eBPF agent: we consume Beyla/OBI, Hubble and Retina instead.
- A replacement for APM service maps: teia sits above them and can read from them.
