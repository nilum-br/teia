# Landscape

Reviewed on 14 October 2026 against official product documentation and this repository. This compares documented
capabilities; it is not proof that a competitor lacks every feature not mentioned. Priorities are our assessment.

## Where teia fits

teia's strongest proposition combines **declared communication contracts, observed traffic and actionable
coverage advice**, using telemetry a platform already runs. Service maps and RED metrics are established
capabilities elsewhere. The defensible differences are:

- **A reviewable contract:** flows in Git describe destination, protocol, port and reason; observed traffic is
  reconciled with that intent as `ghost` and `stale`, rather than merely added to a diagram.
- **Advice at the missing permission or integration:** the advisor can suggest `pg_monitor`, an adapter or
  instrumentation, with a concrete snippet to review.
- **Heterogeneous and legacy discovery:** traces, mesh, network telemetry, logs, inventory and PostgreSQL-side
  discovery contribute to one graph without requiring a teia agent.
- **One relationship for operations and architecture review:** the graph explains a problem while the flow matrix
  supports platform/security review. Automated pull requests remain future work.
- **Evidence that can be evaluated:** attribution and unknown health exist. Age and confidence per measurement
  would strengthen this proposition further.

These are differentiating combinations, not claims of worldwide exclusivity.

## References worth learning from

| Reference | Documented capabilities | Application to teia |
|---|---|---|
| Kiali | Service/workload/application graphs, filtering, operations, ranking, side panels and replay. [Topology](https://kiali.io/docs/features/topology/) | Make the path from a failing journey to its dependency and evidence short. Filters and replay are benchmarks, not exclusive features. |
| Grafana / Tempo | A graph connected to metrics and spans, with virtual nodes for some uninstrumented dependencies. [Service graph view](https://grafana.com/docs/tempo/latest/metrics-from-traces/service_graphs/service-graph-view/), [virtual nodes](https://grafana.com/docs/tempo/latest/metrics-from-traces/service_graphs/) | Open traces, logs and dashboards with the selected service and period; distinguish inferred from directly observed identity. |
| Hubble | Service dependency maps built from Cilium network flows. [Hubble UI](https://docs.cilium.io/en/stable/observability/hubble/hubble-ui/) | Keep network evidence distinct from application failures and missing instrumentation; consume established capture tools. |
| Coroot | Application, infrastructure and datastore inspections; PostgreSQL inspections include blockers and replication. [Inspections](https://docs.coroot.com/inspections/), [PostgreSQL](https://docs.coroot.com/inspections/postgres/) | Explain DB/cache/queue problems with their own measurements. Broad component coverage alone is not unique. |
| Datadog | Discovered and manually declared catalog dependencies, plus inferred databases, queues and APIs. [Dependencies](https://docs.datadoghq.com/internal_developer_portal/use_cases/dependency_management/), [inferred services](https://docs.datadoghq.com/tracing/services/inferred_services/) | Position teia around explicit reconciliation across vendors, rather than claiming nobody combines declared and observed dependencies. |

## What the code supports today

| Capability | Status | Implementation and limits |
|---|---|---|
| Journeys, filters and shareable views | Available | `internal/api/ui/teia-ui.js`: labels, scope, ports and facets; same component in Grafana. |
| User-operation journeys | Partial | Label membership and graph expansion can include unrelated traffic through a shared service. This is not an end-to-end transaction trace; the backend has no Journey entity. |
| Services, SQL/document databases, caches and queues | Available | `internal/source/prometheus/probes.go` reads supported exporters. Coverage depends on supplied telemetry and identity labels. |
| Discovery inside a datastore | Partial | PostgreSQL explorer exists. Redis/MySQL/Kafka exporter metrics are not equivalent to explorers of clients or consumer groups. |
| Unknown health and reasons | Available, with limits | `internal/graph/health.go` only judges interpretable readings. Retained topology does not replay old measurements. Explicit age and partial coverage per signal remain to be exposed. |
| Per-service or operation thresholds | Not implemented | Thresholds are global in `internal/config`. Kiali supports targeted rules. [Kiali health configuration](https://kiali.io/docs/configuration/health/) |
| Drift, coverage advisor and generated flows | Available | `internal/graph`, `internal/advisor`, `internal/export`: ghost/stale, snippets and flow matrix. |
| History and Grafana time range | Partial | Retention and a change feed exist in memory. `internal/api/grafana.go` serves the current snapshot; `TeiaPanel.tsx` does not use the historical `timeRange`. |
| Links to traces/logs/dashboards | Not implemented in the inspected UI | Details offer internal navigation and source information. Configurable evidence links remain a priority. [Grafana data links](https://grafana.com/docs/grafana/latest/visualizations/panels-visualizations/configure-data-links/) |
| Kubernetes/Grafana installation | Partial | Helm, schema, RBAC, token, Ingress, ServiceMonitor and chart checks exist. Panel uses Infinity or direct API; native data source, signing and catalog distribution remain future work. |
| Git review and Backstage | Partial | Flows and CSV exist. Backstage, `teia diff` and opening a PR to accept a flow remain roadmap items. |

## Priorities

**P0: confidence in health and measurements.** Distinguish absent, zero, inferred and expired data. Tests cover
uninterpretable saturation, source failure/recovery and retained edges. Next, expose observation time and coverage
per signal, and identify aggregated latency correctly: current node/group p95 summaries are weighted approximations,
not the percentile of a combined distribution or an end-to-end journey. Percentiles cannot generally be aggregated;
aggregate histogram buckets first where available. [Prometheus guidance](https://prometheus.io/docs/practices/histograms/)

**P1: finish the diagnostic path.** Show value, threshold, period, source and age together, then open the relevant
trace/log/dashboard. Bring journey scope to the backend and allow targeted thresholds, keeping membership distinct
from operation-specific traffic.

**P1: repeatable installation.** Validate upgrades, authentication and platform-managed accounts; package the chart
with the release. Complete signed Grafana distribution and assess a native data source before adding an operator.

**P2: close the contract-review loop.** Persist first/last observation, account for expected frequency of batch jobs,
compare before/after, and produce a reviewable Git change to accept observed communication as declared.

Component additions to evaluate: oldest-message age, consumers without progress, unacknowledged messages and
dead-letter queues; Redis evictions/rejected clients; PostgreSQL old transactions and replication delay. Keep
measurements separate from estimates: backlog divided by consumption rate estimates drain time, not necessarily
the age of the oldest message.

## Scope to preserve

- Consume established telemetry rather than shipping another eBPF agent.
- Keep the contract and advisor useful independently of the graph UI.
- Integrate with Grafana and Backstage where teams investigate and review architecture.
- Keep core reconciliation and advice open source.
