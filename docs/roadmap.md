# Roadmap

teia wins on what nobody else does: **drift between declared and observed**, an **advisor that turns blind
spots into exact next steps**, and a **flow matrix** security teams can act on. Drawing the graph and
per-edge RED metrics is what Grafana, Kiali and the APMs already do well, so teia links to them instead of
competing. See [landscape.md](landscape.md).

## v0.1: useful on a real platform

- [x] Engine: merge, drift, coverage, advisor with stable rule IDs
- [x] Sources: declared, servicegraph (OTel Collector / Tempo / Beyla), docker, postgres explorer
- [x] CLI: `serve`, `discover --fail-on`, `export matrix`, `validate`
- [x] Built-in UI, Grafana Node Graph via the Infinity data source
- [x] Lab with staged blind spots
- [ ] **Kubernetes inventory**: pods, services, owners, IP → workload with history; RBAC finding when forbidden
- [ ] Identity resolution with confidence and provenance; annotation `teia.nilum.io/node-id`
- [ ] `teia diff` for pull requests: drift introduced by a change, as a PR comment
- [ ] Persist aggregated edges (first seen, last seen, sources) so short windows do not erase history
- [ ] Container image and Helm chart

## v0.2: where developers work

- [ ] **Backstage backend module**: emits `teia-talksTo` / `teia-talkedToBy` relations, so the existing
      catalog graph shows observed communication without new UI
- [ ] **Backstage entity card**: neighbours, ghost and stale edges, visibility (new and legacy frontend systems)
- [ ] "Accept as declared" opens a PR with the generated flows entry
- [ ] Istio (`istio_requests_total`) and Hubble (Cilium) sources
- [ ] `expectedFrequency` in flows, for rare flows
- [ ] Findings in Portuguese (`pt-BR`)

## v0.3: from contract to enforcement

- [ ] Generate NetworkPolicy, CiliumNetworkPolicy and Istio AuthorizationPolicy from **declared** flows,
      with a "what would break" report based on observed traffic
- [ ] Flow matrix as XLSX, with templates for firewall change requests
- [ ] Grafana app plugin (Go backend, signed)
- [ ] Explorers: MySQL, Redis (`CLIENT LIST`), Kafka consumer groups

## Not planned

- Our own eBPF agent: we consume Beyla/OBI, Hubble and Retina instead.
- A replacement for APM service maps: teia sits above them and can read from them.
