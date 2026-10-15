# Roadmap

teia focuses on **drift between declared and observed**, an **advisor that turns blind spots into exact next
steps**, and a **flow matrix** security teams can act on. Graphs and per-edge RED metrics are established in
Grafana, Kiali and APMs. Contextual links now connect the graph to their detailed evidence.
See the [comparison and implementation audit](landscape.md).

## Completed in the first October 2026 audit

- [x] Preserve topology without presenting retained rates, latency or capacity readings as live health.
- [x] Keep health unknown when a saturation reading has no known capacity or its cause cannot be judged.
- [x] Validate externally managed Kubernetes credentials and chart-managed token rotation.
- [x] Add Helm lint, chart regression tests and versioned packaging to CI/release workflows.

## Prioritized implementation backlog

Updated **15 October 2026** for the T01–T16 candidate, following the
[market, requirements and UI/UX review](landscape.md). **T01–T09, T11 and T13–T16 have implemented deliverables
and technical validation. T10 has a tested unsigned candidate; public signing/distribution is open. T12 has
automated scenarios and an installed Novatec comparison; the five-person human pilot is open.** These external
gates are deliberately not marked complete. [Candidate guide](candidate-guide.md), [UI/performance evidence](ui-validation.md),
[candidate matrix](installation-candidate.md), [pilot/comparison](usability-pilot.md) and
[dependency constraints](dependency-status.md) record the actual acceptance limits. v0.2/v0.3 remains outside
this delivery except overlapping capabilities already implemented under a T-ID.

The tested unsigned candidate is available for evaluation. Human usability validation and public signing
are separate release gates and must not be represented as already passed.

P0 protects the interpretation of data; P1 completes the primary investigation and adoption experience; P2
extends the product after those foundations. Size is relative scope (S small, M medium, L large), not a delivery
date or an estimate backed by an implementation spike. Areas identify where work belongs, not assigned people.

| ID | Priority / size | Deliverable | Depends on |
|---|---|---|---|
| T01 | P0 / M | Unknown and partial error coverage through aggregation and Grafana import | — |
| T02 | P0 / M | Honest latency and journey-summary semantics | T01 for partial coverage |
| T03 | P0 / M | Effective period, observation age and current-state mode | T01 |
| T04 | P1 / M | Readable map, compact legend and useful journey fit | — |
| T05 | P1 / M | Investigation-first details and scoped findings | T01–T03 |
| T06 | P1 / M | Focused filters, keyboard flow and responsive accessibility | T04 |
| T07 | P1 / M | Contextual links to traces, logs and dashboards | T03 |
| T08 | P1 / L | Explicit journey membership and shared-service boundaries | T01–T03 |
| T09 | P1 / M | Threshold overrides with visible rule provenance | T02–T03 |
| T10 | P1 / L | Repeatable Grafana/Kubernetes onboarding and release distribution | T01, T03 for candidate; T12 before release |
| T11 | P1 / L | Measured scale, hierarchical navigation and neighbour ranking | T01, T04, T06 |
| T12 | P1 / M | Scenario-based usability and competitive validation | T01–T07 for first pilot; T10 candidate and T11 for release gate |
| T13 | P2 / L | Persisted observation history and replay | T03, T08 |
| T14 | P2 / L | Reviewable Git changes and expected-frequency drift | T13 for historical comparisons |
| T15 | P2 / L | Deeper resource signals and clearly scoped explorers | T03, T09 |
| T16 | P1 / M | Latest stable dependencies and a repeatable update policy | Independent; validate with the current implementation |

### T01: Preserve unknown data throughout aggregation and frame import

**Areas:** shared UI core, Grafana `frames.ts`, frame contract and regression fixtures.
**Why:** reproduced R1/R2 can imply fewer errors or stronger visibility than the evidence supports.

- [x] Separate total traffic from traffic with error measurements. For 100 req/s at 50% measured errors plus
      900 req/s with unknown errors, display 50% **of the measured 10%**, or an explicit overall unknown state;
      never present an unqualified 5%. All-unknown inputs must not display 0% errors.
- [x] Generic frames containing only rate remain unknown for error health and first-hand visibility. Preserve
      explicit zero, missing, null and invalid values distinctly; document supported fields, units and defaults.
      An absent/incompatible measure must not silently become healthy or full coverage.
- [x] Add regressions for mixed/all-unknown, zero traffic, invalid data and typed Teia versus generic Node Graph
      frames. Check scope header, grouped edge, details and coverage rings together; retain valid backend health
      reasons from other measured signals.

### T02: Make latency and journey summaries mean what their labels say

**Areas:** model/aggregation, shared UI labels/tooltips, documentation.

- [x] Identify weighted summaries of percentiles as approximations throughout API/UI/Grafana; do not label them
      as a combined-distribution p95 or an end-to-end transaction p95. When compatible histograms are available,
      aggregate the distributions before calculating a percentile.
- [x] Distinguish entry-edge traffic/errors from worst dependency health. A journey with successful entry calls
      and a failing downstream dependency must explain both facts without suggesting a contradictory status.
      Fan-out totals must not be described as a count of unique user transactions.
- [x] Validate with unequal traffic distributions, fan-out, partial latency coverage and zero-rate samples.
      Document which input supports each statistic and remove unsupported competitor-equivalence comments.

### T03: Expose actual time, freshness and coverage

**Areas:** source/model metadata, graph and Grafana APIs, `TeiaPanel`, shared UI.

Implemented: current/query/mixed modes, explicit ignored dashboard range, individual source completion times,
fixed Prometheus/LogQL evaluation times, lookbacks, retained evidence, supplied observation timestamps, unknown
timing, grouped detail provenance and last-successful source collection. Automated coverage includes
failure/recovery, old supplied samples, retention expiry, pause/errors and two Grafana ranges. Tempo supplies
original sample bounds and call counts, with unknown bounds if any contributing span lacks valid times.
Mapped gauges now report the oldest input sample per reading, including capacity and RabbitMQ inputs.
Derived rates and inputs without timestamps explicitly retain unknown original age; collection/refresh never
substitutes for it. Saved dashboards have browser/API validation. T13 separately adds discrete stored replay;
neither feature claims an atomic snapshot across source queries or arbitrary historical telemetry queries.

- [x] Preserve Tempo sample start/end times without substituting collection time; keep one search window per
      round. Qualify sample counts and bounds in shared/Grafana details, including age at collection and future clocks.
- [x] Reproduce and fix attribution of old sampled calls to a newer owner of a recycled IP. Validate old data
      in successful responses, failure/recovery, unknown/reversed times and grouping/paused panel provenance.
- [x] Keep the original component/relationship identity in grouped evidence. Equal timestamps and sample
      counts on different relationships must remain separately inspectable, without summing their samples.
- [x] Pin measurement queries to one evaluation time per Prometheus/PromQL/LogQL collection; keep evaluation
      distinct from original samples and actual per-source completion. A slow source must not advance another
      source's collection time; retention must not renew these timestamps. Address ownership uses the evaluated
      window instead of shifting it forward by collection/assembly latency.
- [x] Expose the oldest input sample for mapped Redis, Kafka, MongoDB, MySQL, postgres-exporter and cAdvisor
      gauges. Include required capacity, preserve per-reading origin, and leave missing input age unknown.
      Fetch optional timestamps after all probe values; errors/cancellation must preserve measured health.

Validation on 15 October 2026: Go vet/race tests and golangci-lint passed; 74 shared-UI tests, 41 panel/frame
tests, TypeScript/lint/build and the full local lab scenario passed. The running lab exposed original Tempo
sample bounds and separate Redis/Kafka gauge sample times. Three Grafana API tests verified the installed
plugin and preserved evaluation/gauge/sample metadata in the saved dashboard at 15-minute and 30-day ranges.
Playwright with Chromium in Docker subsequently verified the rendered evidence and current-state warning in
the saved dashboard, including a 30-day range. Pausing via `live=0` and resuming now reconnects to a real new
collection round while preserving manual pan, zoom and selection; previously that URL disabled streaming permanently.

- [x] Show **current state**, effective measurement window and last observation for Teia snapshot/direct modes.
      Selecting a past Grafana period must not imply that these modes queried that period. For external frames,
      preserve supplied period/observation metadata and show unknown when absent.
- [x] Separate conversion/refresh time from observation time; expose age, source and partial/missing/expired
      evidence for relevant signals. Keep topology visible during source failure without making old data live.
- [x] Validate failed/recovered sources, stale samples in successful responses, paused updates, two different
      Grafana ranges and saved dashboards. This task does not require historical storage; that is T13.

### T04: Make the map easier to read at its normal size

**Areas:** shared layout, annotation policy, legend, sidebar and both themes.

- [x] Default to a compact, expandable legend; prevent it from covering important nodes in the demo fixtures.
      Make the sidebar collapsible and fit a journey using the available canvas while keeping boundary ports
      reachable. Preserve manual pan/zoom and selection across updates.
- [x] Prefer one concise primary reason per incident path, exposing propagated/additional reasons on demand.
      Prevent badge/metric collisions and keep the unselected context legible; provide a way to inspect every
      suppressed label. This is progressive disclosure, not removal of evidence.
- [x] Compare overview, selected node, edge, journey and grouped screenshots in light/dark themes at standalone
      1600×960 and 1024×768, plus Grafana panel 900×500 and 600×400. Mark any remaining overlap for follow-up.

Browser review on 15 October 2026 found and corrected overflowing controls and an unreachable drawer close
button at 600 px, a legend covering nodes, and a permanently allocated empty sidebar that squeezed journeys.
Selection now reveals nodes/edge endpoints when they fit beside the drawer; a selected edge keeps its own
context highlighted. Clicking a metric/reason label or boundary port no longer starts map panning and swallows
the click. A different selection starts its details at the top; background updates retain the current scroll.

Validation: 11 Chromium tests plus 3 Grafana API tests, 49 review screenshots, 74 shared-core tests, 41 panel/frame
tests and 2 dependency regressions. CI runs browser interactions and uploads `visual-review`; screenshot review
is still required because live values prevent a fixed pixel baseline. The Docker runner is documented in the
[panel README](../plugins/grafana/README.md).

Completed follow-up: compact overviews suppress badges/metrics and use 12 CSS-pixel labels with explicit
truncation. Very dense maps hide overview labels until focus/hover. Read names, search, keyboard selection and
Focus on map expose complete identities and evidence; fit reserves room for zoom controls. Details prioritize
rules/evidence with expandable provenance. At 600×400 use the overview to orient and focus a component for
investigation; it cannot show every full name at once. Both themes, 200% CSS zoom and widths 640/960/1600
are covered by interaction checks and palette measurements; see [limits and results](ui-validation.md).

### T05: Put the selected problem and its evidence first

**Areas:** details, advisor presentation, scope summary and wording.

- [x] Selecting a journey/node/edge prioritizes its health and relevant findings. Present a concise sequence:
      affected component → dependency → measured value → rule/threshold → source and time → next action.
      Keep unrelated/global coverage advice in an explicitly separate expandable section.
- [x] Distinguish measured causes from propagated symptoms or hypotheses; deduplicate repeated explanations
      without hiding their affected callers. Sort actionable findings by severity and relevance to the scope.
- [x] Make ratio denominators available without hover-only help (for example, 8 of 14 internal components seen
      first-hand). Distinguish animation (`Flow`) from data refresh (`Live`) and unknown from measured zero.

### T06: Improve filters, sharing, keyboard flow and accessibility

**Areas:** shared controls, filter state, Grafana view integration, browser interaction checks.

- [x] Prioritize journey, namespace and health; keep other labels searchable under advanced filters. Explain
      whether counts are global or reflect the current intersection. Keep selected chips and a clear reset;
      present an actionable empty state when a combination matches nothing.
- [x] Preserve focus after toggling facets, close the filter panel with one Escape even from its search input,
      and restore focus to its trigger. Make nodes, connections, details and dismiss controls reachable without
      a mouse. Check focus visibility and reduced-motion behavior rather than only the presence of ARIA labels.
- [x] Verify both themes, keyboard-only operation, 200% zoom and widths 640/960/1600. Measure contrast and target
      sizes against the [review's WCAG references](landscape.md#ui-and-ux-assessment), recording applicable
      exceptions. Verify copy-link restoration in a clean session; in Grafana distinguish shared dashboard
      variables from local-only view state and support an explicit shareable scope.

### T07: Open the supporting trace, logs or dashboard

**Areas:** configurable evidence-link templates, backend/public configuration, node/edge details, Grafana.

- [x] Support node **and connection** links using stable identity, namespace, technology and effective period;
      include external services and resources. Show only links with the required context, with clear labels.
- [x] Provide lab templates for Tempo, Loki and a Grafana dashboard. Verify destination query/time context,
      special-character encoding and permitted URL schemes; do not place source credentials in shared links.
- [x] Reach relevant evidence from a selected failing connection in at most two intentional actions in both
      interfaces. Describe service/time search as such; only claim a specific trace link when its ID is known.

### T08: Give journeys an explicit, portable contract

**Areas:** Kubernetes metadata, model/API, graph scope logic, UI/docs.

- [x] Support multiple journey memberships in Kubernetes through a documented annotation or equivalent
      structured configuration; retain single-value labels. Use one backend membership/scope definition in
      the API and both interfaces, with explicit entry points, expansion limits and boundary ports.
- [x] Test shared frontend, two journeys, a shared database, an unlabelled upstream caller, unlabelled downstream
      services, cycles and missing telemetry. An upstream caller must not silently become a member solely
      because it calls a member; label ownership must not be presented as request correlation.
- [x] Specify a separate operation/trace-derived mode before implementing transaction-level journeys. Its
      acceptance fixture must separate checkout and browse traffic through the same service, disclose sampling
      gaps and avoid claiming end-to-end coverage from topology labels alone.

### T09: Allow targeted thresholds and explain the chosen rule

**Areas:** configuration/schema, health engine, reason metadata and details.

- [x] Keep global defaults and add overrides by stable component/connection identity or label selector.
      Define deterministic precedence and reject ambiguous/invalid configuration with useful messages.
- [x] Show measured value, unit, effective period, threshold and matching rule. API, standalone and Grafana
      must agree; distinguish component signals such as queue lag, saturation and latency.
- [x] Verify two components with different acceptable latencies and traffic levels, invalid overrides, fallback
      defaults and missing data. Approximate/unavailable latency must not silently become an exact SLO result.

### T10: Make installation and input setup repeatable

**Areas:** Grafana packaging/provisioning, Helm/release, examples and installation docs.
Prepare the installable candidate first, validate it through T12, then publish and check the released artifacts;
T12 does not depend on publication being complete.

- [x] Validate the unsigned candidate on a clean Grafana 13.2.0 install and a same-volume upgrade to 13.2.4;
      record version/hash and test states. See the [matrix and packaging procedure](installation-candidate.md).
- [ ] Obtain public signing approval and a valid signature; install the signed package without an unsigned-plugin
      exception. Catalog approval remains a separate external milestone.
- [x] Provide portable provisioning, a small example dataset and actionable no-data/invalid-frame/source-error
      states. Document mappings from supported query results to typed fields. Evaluate an arbitrary-column
      mapping editor against real onboarding cases; do not force adoption of a new data source without evidence.
- [ ] Publish and download-test tagged chart/image/plugin artifacts when release work is authorized. Cover fresh
      Helm install, upgrade, configured authentication and external ServiceAccount; preserve the checks already
      delivered. First release validates the existing Infinity/direct paths; a native data source remains later.

### T11: Validate scale and add navigation that remains useful

**Areas:** fixtures/benchmarks, layout, grouping, detail lists and UI performance.

- [x] Measure loading, layout, filtering, selection and refresh on 100/500/1,000-node fixtures with documented
      edge counts, hardware/browser and animation settings. Record median/p95, memory trend and truncation.
      A pure layout benchmark does not prove rendering or operator usability.
- [x] Support ordered grouping such as cluster → namespace → service where labels exist; expand/collapse without
      losing identity, selection or external context. Show hidden/truncated counts and sort neighbours by
      traffic, measured errors and latency with their coverage/approximation labels.
- [x] Set a proposed p95 selection/filter response budget of 200 ms on the agreed reference machine and measure
      it before claiming compliance. Use the results to decide whether level-of-detail rendering, animation
      reduction or a different renderer is needed; document the supported operating envelope.

### T12: Validate the investigation workflow with tasks, not feature counts

**Areas:** QA, product/design, repeatable lab scenarios and user sessions.

- [x] Use the same error, slow dependency, queue backlog, source outage, shared service and undeclared-flow
      scenarios in standalone and Grafana. Add fresh browser checks for filtering, selection, history/current
      distinction, keyboard navigation and evidence links. Turn R1/R2 into permanent regressions via T01.
- [ ] Run a formative pilot with five representative platform/development users. Proposed target: at least four
      identify the affected path within 60 seconds and reach the supporting evidence within 120 seconds without
      coaching; all must distinguish unknown from healthy in the prepared missing-data case. Record failures,
      misinterpretations and task completion, not just satisfaction. Five users do not prove market superiority.
- [x] Compare overlapping tasks with an installed Novatec/Grafana setup, and Kiali where the environment fits,
      using comparable data and documented setup effort. Contract/advisor-only tasks assess Teia's own value.
      Separate observed wins, ties and unresolved gaps before changing product claims or closing release validation.

Evidence: [seven scenarios on two surfaces, installed comparison and pilot protocol](usability-pilot.md).
Kiali was assessed from official documentation; this lab has no Istio mesh, so no installed Kiali parity result
is claimed. Human diagnosis speed, preferences and superiority remain unmeasured.

### T13: Persist observations and support a real historical view

**Areas:** storage, time-range API, replay/diff UI and Grafana integration.

- [x] Persist first/last seen, source provenance and time-windowed evidence with explicit retention/size limits;
      verify restart recovery and schema upgrades.
- [x] List actual captures in an inclusive `from/to` interval and replay the selected immutable snapshot with its
      original topology and evidence. Shared links restore that same ID; unavailable history is not no traffic.
      Arbitrary interval aggregation/resampling is outside this discrete-snapshot contract.
- [x] Validate disappearance/reappearance, late data, identity reuse and a process restart. Keep historical data
      from being mistaken for current health and avoid presenting interpolated periods as directly measured.

### T14: Complete the contract-review loop

**Areas:** CLI diff, flows, Git integration, advisor and scheduling semantics.

- [x] Generate a minimal, reviewable flows change for accepted observed communication, preserving protocol,
      destination, port, rationale and evidence. Validate it before opening a PR; do not automatically allow
      traffic or deploy network policies.
- [x] Add `teia diff` and an explicit expected-frequency model for rare/batch flows. Test a daily job across a
      short observation window so absence does not imply an unjustified stale warning.
- [x] Demonstrate observed drift → reviewed diff → declared flow → reconciled graph. Include identity confidence
      and historical evidence when available; retain manual review and existing repository conventions.

### T15: Deepen resource diagnosis where telemetry supports it

**Areas:** probes, explorers, signal schema, health rules and resource details.

- [x] Prioritize supported queue signals (oldest-message age, consumer progress, unacknowledged/dead-letter
      messages), Redis evictions/rejected clients and PostgreSQL old transactions/replication delay. Document
      exporter/version/label prerequisites and distinguish absent capabilities from zero values.
- [x] Keep backlog, estimated drain time and actual oldest-message age separate. Use controlled fixtures for
      growing queues, stopped consumers, cache pressure and blocked transactions; link each reason to evidence.
      State whether a time estimate assumes no new arrivals or uses net consumption; nonpositive net consumption
      cannot support a finite catch-up estimate under continuing traffic.
- [x] Evaluate MySQL/Redis/Kafka explorers individually for the discovery gaps they close and their permissions
      and cost. Exporter metrics alone must not be advertised as discovering clients or consumer ownership.

See [resource diagnostics](resource-diagnostics.md) for tested PromQL/PostgreSQL fixtures and prerequisites.
RabbitMQ head timestamp age depends on publisher clocks; Kafka drain time assumes no new arrivals. Dead-letter
ownership and absolute Kafka message age are not inferred. The three additional explorers were evaluated,
not implemented; missing capabilities produce informative findings instead of fabricated zero measurements.

### T16: Keep toolchains and dependencies on current stable releases

**Areas:** Go/TypeScript, direct and transitive libraries, container images, plugin tooling and CI actions.
Dependency audit updated on 15 October 2026 alongside the P0 fixes.
See [versions, sources and compatibility evidence](../CONTRIBUTING.md#dependency-maintenance).
Project policy: latest **stable** across the whole stack, including Current runtimes and
major/breaking upgrades. Adapt code or replace blocking tools instead of using compatibility as a default reason
to stay behind. Prereleases and untagged development HEADs are tracked separately.

- [x] Inventory current versions and verify latest stable releases against official registries/upstream sources.
      Include both Go modules, the Grafana plugin, language runtimes, build/lint/test tools and lab/release images.
      Distinguish stable from prerelease tags and declared versions from those actually resolved in lockfiles.
- [x] Apply updates and required compatibility changes, align related packages, refresh locks and review changes
      in supported runtimes. Record an evidence-backed reason and follow-up for any dependency that cannot yet
      run on the latest stable release; avoid arbitrary downgrades or unreviewed forced audit fixes.
- [x] Pass Go race/vet/lint, plugin types/lint/tests/build and local lab end-to-end validation on the updated
      toolchain. Add a repeatable dependency-update mechanism and document how version alignment is checked.

Verification on 15 October: Go race/vet/lint and tidy checks; 73 shared UI tests and 33 plugin tests; TypeScript 7,
ESLint and Webpack build; Helm lint/default/full-values and five chart regressions; actionlint; Go vulnerability
checks for root/demo; real npm 12.2 clean install; rebuilt lab end-to-end scenarios passed. The saved dashboard's
Infinity queries returned current-mode metadata/evidence for both 15-minute and 30-day requested ranges with
the same snapshot timestamp. The Node 26/ESLint 10 migration also passes three repeatable API integration tests;
the REST client change passes the full Go suite and builds all six Linux/macOS/Windows release targets.
This is API/DOM validation, not a new interactive visual/accessibility audit.

- [x] T16.1: migrate to Node 26.12.0 Current throughout the project; replace the unused Node-24-only Grafana E2E
      scaffold, align Node types and enforce the runtime. Three API integration tests cover the actual lab/dashboard.
- [x] T16.2: migrate to ESLint 10.12.0 with the current React lint implementation, preserving Hooks and project
      checks; enable future major updates in Dependabot and add `npm run deps:check` (now 51 direct packages current).
- [x] Remove JSON patch v4 from compiled Kubernetes inventory/test dependencies by using client-go's official
      REST transport for pod/service reads. Preserve pagination, namespaces, authentication/TLS, timeout and RBAC;
      the used Linux/Windows module set drops from 51 to 32, with real HTTP transport regression tests.
- [x] T16.3: document the selected stable Kubernetes SDK's kube-openapi revision, FTP/YAML override removal
      conditions and the stable-versus-RC webpack-subresource-integrity check. These recurring upstream
      maintenance conditions are tracked in [dependency status](dependency-status.md), not unimplemented fixes.
- [x] T16.4: remediate residual npm advisories in braces, sprintf-js and Grafana's router compatibility dependency.
      Audit is now **zero**, from 26 alerts (3 high, 23 moderate). Use the public Grafana data theme context,
      ESLint's API in an owned Webpack config, and a validated YAML 5 override for the Istanbul loader. No
      downgrade or audit suppression. CI now gates on `npm run deps:audit`; 34 panel tests (including theme
      changes), two dependency integration tests and coverage generation pass. Production/development builds
      and 73 shared UI tests pass. The CI Grafana health wait also retries connection resets during startup.
- [x] T16.5: track the older internal API majors still imported by the latest Kubernetes/OpenTelemetry/Grafana
      packages. Fresh Go scans include SMD6/YAML2, kube-openapi and backoff5 and find no known vulnerabilities.
      Both official OTLP protocols still use backoff5; removing SMD6 also means replacing kubeconfig/exec auth.
      Keep these version constraints distinct from the now-resolved npm advisories; no incomplete SDK forks.

## Suggested execution order

1. **Trust the data:** T01–T03. Close the reproduced bugs and temporal/percentile ambiguity first. T04 design
   and onboarding research can proceed independently.
2. **Shorten diagnosis:** T04–T07, then the first T12 user pilot. Keep the existing shared map and visual identity;
   judge each change by clearer evidence and fewer steps, not by a larger number of controls.
3. **Prepare broader adoption:** T08–T11 and T12 release validation. Release signing/catalog availability and
   human pilot participation are explicit dependencies; they are not completed by preparing files alone.
4. **Extend the differentiator:** T13–T15, then Backstage and policy-generation milestones below as demand warrants.

The version sections below preserve the longer-term scope. Their overlapping items are tracked by the T-IDs
above rather than treated as separate duplicate work.

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
- [x] `teia diff` for reviewable snapshot/declaration changes and accepted Flows fragments (T14).
- [ ] Automated PR creation/comment publication (future integration).
- [x] Opt-in persisted snapshots with first/last seen, source evidence, bounded retention and exact replay (T13).
- [x] Helm chart: one read-only pod, no CRD/operator, optional auth/Ingress/ServiceMonitor, hot-reloaded flows

## v0.2: where developers work

- [ ] **Backstage backend module**: emits `teia-talksTo` / `teia-talkedToBy` relations, so the existing
      catalog graph shows observed communication without new UI
- [ ] **Backstage entity card**: neighbours, ghost and stale edges, visibility (new and legacy frontend systems)
- [ ] "Accept as declared" opens a PR with the generated flows entry
- [x] `expectedFrequency` in flows, for rare flows (delivered under T14)
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
