# Landscape

How teia relates to what already exists, as of October 2026, based on public documentation, GitHub activity
and plugin catalog data gathered at that date.

## The short version

**Mapping who talks to whom is a commodity. Reconciling it with what teams declared is not.**

- Every active open source tool *observes*: Kiali, Hubble, Coroot, Pixie, Kubeshark, Retina, NetObserv,
  Beyla/OBI, Tempo service graphs.
- Only Backstage's catalog graph and Otterize *declare*.
- The only one that compared the two (Otterize's shadow mode) lived in a SaaS that stopped after the company
  was acquired in 2025.
- Commercial APMs come closest:
  - Datadog shows declared and observed dependencies side by side, but does not diff them.
  - New Relic and Dynatrace turn blindness into a score and a button, but the answer is always "install our agent".

Nobody gives advice at the level of a **data source permission** ("grant `pg_monitor`"), across OTel, mesh,
eBPF and databases at once.

## Open source

| Tool | Observes | Declares | Drift | Tells you where it is blind | Backstage | Status |
|---|---|---|---|---|---|---|
| Tempo service graphs / OTel `servicegraph` | traces | no | no | implicitly (virtual nodes) | no | active; **teia's main source** |
| Kiali | Istio telemetry | no | Istio config only | "Missing Sidecar", validations | community plugin | active |
| Hubble / Hubble UI | Cilium eBPF flows | no | policy verdicts | no | no | active, Cilium only |
| Coroot | eBPF + DB agents | no | no | some inspections | no | active, own UI |
| Beyla / OBI, Odigos | eBPF / auto-instrumentation | no | no | no | no | active; **sources for teia** |
| Otterize | eBPF/DNS, Istio, Kafka | ClientIntents | shadow mode (in SaaS) | no | no | stalled since 06/2025 (acquired) |
| Caretta | eBPF TCP | no | no | no | no | no commits in a year |
| Weave Scope | /proc, conntrack | no | no | no | no | unmaintained (company closed) |
| Novatec SDG panel | any table | no | no | no | no | ~13.5M downloads, no commits in a year |
| Backstage catalog-graph | declared relations | yes | no | no | core | active |

## Commercial

| Product | Declared + observed | Drift | Coverage advice |
|---|---|---|---|
| Datadog | yes (imports `catalog-info.yaml`) | side by side, no diff | inferred services |
| Dynatrace | topology injection | no | Discovery & Coverage: "install OneAgent here" |
| New Relic | user-defined relations | no | dotted uninstrumented entities, "Instrument what's missing", score |
| ServiceNow Discovery | manual CIs | no | **the reference UX**: stable error codes, root cause, remediation |
| Tufin / AlgoSec | application connectivity | partial | firewall-centric; CSV "communication matrix" |

## Internal developer portals

Backstage, Port, Cortex, OpsLevel and Roadie treat dependencies as declared data. Some import an APM's
service map. None shows the difference: Cortex and Port merge and de-duplicate, which hides drift.

## Where teia fits

| teia does | teia leaves to others |
|---|---|
| drift (ghost / stale) as a first-class concept, vendor-neutral | drawing graphs with RED metrics (link to Grafana / Kiali instead) |
| coverage advisor at the permission level, across sources | eBPF capture (consume Beyla / OBI / Hubble / Retina) |
| datastore-side discovery (`pg_stat_activity`) for clients nothing else sees | NetworkPolicy from observed traffic only (Kubescape, Inspektor Gadget, RHACS) |
| a declared contract in git, next to `catalog-info.yaml` | |
| flow matrix export for firewall and security reviews | |

## Lessons from those that stopped

They did not stop for lack of users. They stopped because the open source part was a funnel for a SaaS, or
a showcase, and the company changed course, or because maintaining their own agent and UI was too expensive.
teia's answer:

- a small maintenance surface: no eBPF of its own, UI inside Backstage and Grafana;
- the core features stay fully open source;
- neutral governance as it grows.
