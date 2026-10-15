# Landscape

Reviewed on **15 October 2026**, using teia `62ef24b`, official product documentation and Novatec's official
repository/catalog. This compares documented capabilities; it is not proof that a competitor lacks every feature
not mentioned. Priorities are our assessment.

**Implementation follow-up, 15 October 2026:** T01 regressions now cover and fix R1/R2 below. T02 adds partial
latency coverage and explicit percentile approximations in the model, UI and Grafana. T03 adds current-state
labelling and separates collection time, lookback and supplied observation timestamps; original sample age
remains unknown where adapters do not expose it. The tables and reproductions below describe the reviewed
`62ef24b` baseline, not reopened defects. See [the live task status](roadmap.md#prioritized-implementation-backlog)
for verification and remaining work. These fixes alone do not establish overall superiority or usability parity.

**Verdict:** teia covers more of our architecture-contract and coverage-diagnosis problem than a graph panel alone.
It is not yet demonstrably better overall in investigation, installation, usability or production maturity.
The strongest next investment is trustworthy measurements and a shorter path from a failing relationship to
evidence, followed by clearer navigation. Adding more visual decorations is a lower priority.

Evidence in this review:

- The running local lab served 16 nodes and 14 edges; graph, sources, Grafana frames and Grafana health APIs
  responded successfully. PostgreSQL's restricted monitoring permission is an intentional lab scenario.
- `docker compose -f lab/compose.yaml run --rm e2e` passed: telemetry contributions, advisor permission recovery,
  the hidden PostgreSQL client and lock attribution. The test restores its temporary monitoring grant.
- Pure-function executions reproduced the aggregation and generic-frame bugs below. These are not just design
  preferences, and the previous engine health fix does not fix every presentation/import path.
- UI findings use the checked-in captures of the current implementation and code inspection. An interactive
  browser was unavailable in this session. No fresh click-through, responsive/accessibility certification,
  user study or competitive performance benchmark is claimed. Novatec was not installed for this review.
- The [implementation backlog](roadmap.md#prioritized-implementation-backlog) distinguishes fixes, design work
  and validation. Its acceptance criteria are proposed targets, not results already achieved.

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
| Novatec Service Dependency Graph Panel | Configurable table-column mapping, response-time baselines, namespace layers and a node drilldown URL. Distributed through Grafana's catalog. [Repository](https://github.com/NovatecConsulting/novatec-service-dependency-graph-panel), [catalog](https://grafana.com/grafana/plugins/novatec-sdg-panel/) | Learn from the small installation surface, explicit mappings and navigation to evidence. Drawing databases and queues is not by itself a differentiator. |
| Kiali | Service/workload/application graphs, filtering, operations, ranking, side panels and replay. [Topology](https://kiali.io/docs/features/topology/) | Make the path from a failing journey to its dependency and evidence short. Filters and replay are benchmarks, not exclusive features. |
| Grafana / Tempo | A graph connected to metrics and spans, with virtual nodes for some uninstrumented dependencies. [Service graph view](https://grafana.com/docs/tempo/latest/metrics-from-traces/service_graphs/service-graph-view/), [virtual nodes](https://grafana.com/docs/tempo/latest/metrics-from-traces/service_graphs/) | Open traces, logs and dashboards with the selected service and period; distinguish inferred from directly observed identity. |
| Hubble | Service dependency maps built from Cilium network flows. [Hubble UI](https://docs.cilium.io/en/stable/observability/hubble/hubble-ui/) | Keep network evidence distinct from application failures and missing instrumentation; consume established capture tools. |
| Coroot | Application, infrastructure and datastore inspections; PostgreSQL inspections include blockers and replication. [Inspections](https://docs.coroot.com/inspections/), [PostgreSQL](https://docs.coroot.com/inspections/postgres/) | Explain DB/cache/queue problems with their own measurements. Broad component coverage alone is not unique. |
| Datadog | Discovered and manually declared catalog dependencies, plus inferred databases, queues and APIs. [Dependencies](https://docs.datadoghq.com/internal_developer_portal/use_cases/dependency_management/), [inferred services](https://docs.datadoghq.com/tracing/services/inferred_services/) | Position teia around explicit reconciliation across vendors, rather than claiming nobody combines declared and observed dependencies. |

### Novatec: specific advantages and tradeoffs

The catalog lists **4.2.0** with Grafana **>=10.4.0**. Its [official metadata](https://grafana.com/api/plugins/novatec-sdg-panel/)
reports a community signature, signed by `novatec`, and catalog distribution. This is metadata verification,
not a local signature or compatibility test. Repository code was inspected at `bf97e1d`.

| Dimension | Novatec | teia today / consequence |
|---|---|---|
| Install in existing Grafana | Catalog distribution and signed package metadata. | Unsigned panel instructions, Infinity or direct API. Novatec currently has the simpler distribution story. |
| Bring existing query data | Configurable source, target, timing, request, error and baseline columns. [Options](https://github.com/NovatecConsulting/novatec-service-dependency-graph-panel/blob/bf97e1d/src/options/options.tsx) | Teia/Node Graph frame conventions, plus adapters in the engine. No arbitrary-column mapping editor; generic imports need a stricter data contract (T01, T10). |
| Leave the graph to investigate | Configurable URL with node placeholder and Grafana variables; inspected rendering limits it to `INTERNAL` nodes. [Statistics](https://github.com/NovatecConsulting/novatec-service-dependency-graph-panel/blob/bf97e1d/src/panel/statistics/Statistics.tsx) | Internal navigation exists; configurable trace/log/dashboard links are missing. This is a practical gap, not automatic trace-ID correlation in Novatec (T07). |
| Explore a large map | Multiple namespace depths and sortable inbound/outbound tables. [Tree](https://github.com/NovatecConsulting/novatec-service-dependency-graph-panel/blob/bf97e1d/src/processing/node_tree.ts), [tables](https://github.com/NovatecConsulting/novatec-service-dependency-graph-panel/blob/bf97e1d/src/panel/statistics/NodeStatistics.tsx) | One-label grouping with expansion, richer semantic journey/facet scopes. Hierarchy and neighbour sorting would complement those scopes (T11). |
| Decide what is unhealthy | Response-time baseline supplied in data; no-data state also exists. | More types of health signal and explanatory reasons, but global thresholds and remaining unknown-data bugs. Neither a red graph nor an unknown state is an exclusive feature (T01, T09). |
| Architecture and coverage | The inspected panel focuses on supplied graph data. | Declared/observed reconciliation, coverage advisor, flow matrix and heterogeneous discovery are concrete additional capabilities. They also bring an engine and integration setup to operate. |

Kiali remains a strong reference for investigation through side panels, operation views and historical replay.
Grafana/Tempo is a reference for linking the map to existing evidence; Coroot for resource-specific diagnosis.
These comparisons do not establish that teia replaces those products or outperforms them under equal workloads.

## What the code supports today

| Capability | Status | Implementation and limits |
|---|---|---|
| Journeys, filters and shareable views | Available, with limits | `internal/api/ui/teia-ui.js`: labels, scope, ports and facets; same component in Grafana. Page filters are URL-shareable. Grafana supports the configured Journey variable; arbitrary on-panel selections are local state, not automatically shared in the dashboard URL. |
| User-operation journeys | Partial | Label membership and graph expansion can include unrelated traffic through a shared service. This is not an end-to-end transaction trace; the backend has no Journey entity. |
| Services, SQL/document databases, caches and queues | Available | `internal/source/prometheus/probes.go` reads supported exporters. Coverage depends on supplied telemetry and identity labels. |
| Discovery inside a datastore | Partial | PostgreSQL explorer exists. Redis/MySQL/Kafka exporter metrics are not equivalent to explorers of clients or consumer groups. |
| Unknown health and reasons | Partial | `internal/graph/health.go` only judges interpretable readings; retained topology does not replay old measurements. UI aggregation and generic Grafana frame imports still lose unknown-data semantics (T01). Explicit age and partial coverage per signal remain to be exposed (T03). |
| Per-service or operation thresholds | Not implemented | Thresholds are global in `internal/config`. Kiali supports targeted rules. [Kiali health configuration](https://kiali.io/docs/configuration/health/) |
| Drift, coverage advisor and generated flows | Available | `internal/graph`, `internal/advisor`, `internal/export`: ghost/stale, snippets and flow matrix. |
| History and Grafana time range | Partial | Retention and a change feed exist in memory. `internal/api/grafana.go` serves the current snapshot; `TeiaPanel.tsx` does not use the historical `timeRange`. |
| Links to traces/logs/dashboards | Not implemented in the inspected UI | Details offer internal navigation and source information. Configurable evidence links remain a priority. [Grafana data links](https://grafana.com/docs/grafana/latest/visualizations/panels-visualizations/configure-data-links/) |
| Kubernetes/Grafana installation | Partial | Helm, schema, RBAC, token, Ingress, ServiceMonitor and chart checks exist. Panel uses Infinity or direct API; native data source, signing and catalog distribution remain future work. |
| Git review and Backstage | Partial | Flows and CSV exist. Backstage, `teia diff` and opening a PR to accept a flow remain roadmap items. |

### Are the original expectations met?

| Expectation | Assessment | Remaining work |
|---|---|---|
| Use the Grafana a team already has, without fixed lab identities | Functional foundation delivered | Signed distribution, clean-install validation and clearer input mapping (T10); correct time-range semantics (T03). |
| Read rates, errors and latency on connections | Delivered for supported data, with correctness gaps | Unknown error aggregation/import, percentile labels and actual measurement period (T01–T03). |
| Isolate journeys and use existing namespaces/labels | Delivered for topology membership | Kubernetes multi-membership, shared-service ambiguity and backend contract (T08). This is not yet transaction-level isolation. |
| Include services, SQL/NoSQL, caches, queues and legacy components | Delivered for supported integrations | Exporter coverage is not universal discovery; broader component signals/explorers need explicit support (T15). |
| Quickly identify where a problem originates and what to do | Partial | Reasons and advisor exist; visual repetition, competing sidebar findings and missing evidence links lengthen investigation (T04–T07). |
| Clear corporate Kubernetes deployment without a new CRD | Foundation delivered | Helm and local deployment checks exist; public release artifacts and upgrade matrix need release validation (T10). Flows remain configuration documents. |
| Be usable and reliable beyond the small demo | Not yet established | Scenario, accessibility, scale and user-task evidence (T06, T11, T12). A successful demo is not that evidence. |

## Reproduced correctness gaps

These findings describe the audited revision; they remain open until the associated acceptance checks pass.

| Finding | Reproduction and actual result | Required interpretation |
|---|---|---|
| **R1 — Unknown errors dilute aggregates** | Call `core.scopeStats` and `core.buildView(..., {groupBy: 'team'})` with two entry edges merged between groups: 100 req/s with 50% measured errors, plus 900 req/s with `errorsUnknown: true` and error value 0. Both return **5%**; the grouped edge says `errorsUnknown: false`. With all entry errors unknown, scope stats return **0%**. | The observed subset has 50% errors and covers 10% of traffic; the whole population's error rate is unknown. Show partial coverage or an explicitly labelled bound, never an unqualified total. Files: `internal/api/ui/teia-ui.js`, `scopeStats`, `buildView`. **T01**. |
| **R2 — Generic frames invent health and visibility** | Execute `framesToSnapshot` with node rows `{id, secondarystat: 10}` and an edge `{source, target, secondarystat: 10}`, omitting error and visibility fields. Nodes and edge become `healthy`, `errorRate: 0`, `errorsUnknown: false`; nodes have `visibility: full`, producing 100% visibility. | A positive rate alone proves neither absence of errors nor first-hand instrumentation. Files: `plugins/grafana/src/teia/frames.ts`, `toNode`, `toEdge`, `coverageOf`. **T01**. |

R1 ran through the repository's exported JavaScript core. R2 ran the existing TypeScript functions with Node's
type stripping; the unused `DataFrame` import was marked type-only in memory. No source implementation was
changed. These are focused functional reproductions, not browser tests.

Two additional semantic problems are evident in code: weighted summaries of p95 values are labelled simply
`p95` (T02), and Teia-backed Grafana queries return a current snapshot while the dashboard displays a historical
time picker (T03). Arbitrary Grafana queries can themselves respect a selected range; the limitation is not a
claim that all frame sources ignore time. `framesToSnapshot` stamps conversion time, which is not necessarily
the original observation time.

## UI and UX assessment

The visual foundation is coherent: component icons, text reasons alongside health colours, dark/light themes,
selected filter chips and journey boundary ports all help distinguish types of evidence. Keyboard shortcuts and
reduced-motion handling already exist. Preserve them while improving the following:

| Finding / evidence | User consequence | Activity |
|---|---|---|
| The expanded legend covers the lower-left map, including part of `legacy-reports`, in the [overview](assets/screenshots/teia-map.png). Several node/edge badges repeat a dependency reason. | Less room to read topology; several annotations can look like independent incidents. | T04: compact legend, less repeated annotation, explicit access to all reasons. |
| The [journey capture](assets/screenshots/teia-journey.png) leaves substantial unused space while fitting external ports and the permanent sidebar; labels become small. | Isolating a journey does not consistently make it easier to read. | T04: fit the useful scope, reserve space for ports/overlays, collapsible sidebar. |
| The same journey has a failure headline while the sidebar starts with broad coverage advice, including components outside that journey. | The operator must separate current diagnosis from integration maintenance. | T05: selected-scope findings first, concise evidence chain, expandable global advice. |
| The [filter capture](assets/screenshots/teia-filters.png) gives technical labels similar prominence to journeys/health; its large overlay obscures the result. Counts come from the whole snapshot. | Important filters are harder to find and counts can be mistaken for the remaining result. | T06: common filters first, advanced labels searchable, counts explicitly scoped, preserve selected chips. |
| Facet selection rebuilds the filter DOM; focus is restored for text search but not for facet buttons. `Escape` in an input only blurs it before the dialog-close branch. | Keyboard flow needs targeted verification and correction. This is code evidence, not a completed keyboard audit. | T06: stable focus, one-step dismissal, node/edge access, responsive interaction checks. |
| The [light-theme detail](assets/screenshots/teia-cache-light.png) strongly dims unselected context. Coverage rings explain ratios only on hover; `Flow` and `Live` are easy to confuse. | Context and measurement meaning are harder to learn without hovering or prior knowledge. | T04/T05: legible context, explicit denominators and accessible explanations; distinguish animation from refresh. |
| The [Grafana capture](assets/screenshots/grafana-panel.png) shows “Last 15 minutes” around a panel backed by current-snapshot endpoints. | Users can believe they are investigating an earlier interval. | T03: visible effective period and current-state mode before implementing history. |

Use WCAG 2.2 as a verification baseline: normal-text contrast at least 4.5:1, large text 3:1, and pointer targets
at least 24×24 CSS pixels or a documented applicable spacing/other exception. The small legend-close control
deserves inspection; its CSS size alone does not establish a violation.
[Contrast](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html),
[target size](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html).
This review is not an accessibility conformance claim.

## Priorities

**P0: confidence in health and measurements (T01–T03).** Distinguish absent, zero, inferred and expired data. Existing
tests cover uninterpretable saturation, source failure/recovery and retained edges; R1/R2 reveal missing cases.
Next, preserve unknown data in presentation/import, expose observation time and coverage
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

The detailed order, dependencies and completion criteria are in the [roadmap](roadmap.md#prioritized-implementation-backlog).

Component additions to evaluate: oldest-message age, consumers without progress, unacknowledged messages and
dead-letter queues; Redis evictions/rejected clients; PostgreSQL old transactions and replication delay. Keep
measurements separate from estimates: backlog divided by consumption rate estimates time to process the current
backlog assuming no new arrivals, not the age of the oldest message. Catch-up under continuing traffic depends on
net consumption; a nonpositive net rate cannot support a finite catch-up estimate.

## Scope to preserve

- Consume established telemetry rather than shipping another eBPF agent.
- Keep the contract and advisor useful independently of the graph UI.
- Integrate with Grafana and Backstage where teams investigate and review architecture.
- Keep core reconciliation and advice open source.
