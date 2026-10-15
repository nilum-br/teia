# Investigation pilot and installed comparison

This is a runnable formative-study plan, **not a completed user study**. No participant results have been
collected. Automated interactions and agent inspection establish reproducibility, not that people diagnose
incidents faster or prefer Teia. Use the [candidate handoff](installation-candidate.md) to freeze a concrete build.

**Owner and status:** the colleague receiving the handoff will organize the five-person T12 pilot after
delivery, as assigned by the user. The pilot is delegated, not executed: participant recruitment, sessions,
results and acceptance review remain open. The automated scenario baseline below is already complete.

## Automated scenario baseline

On 15 October 2026, `tests/scenarios.spec.ts` passed all 14 Chromium cases: seven deterministic inputs on
standalone and the installed Grafana panel. Standalone mounts the shared UI with an explicit snapshot;
Grafana retains the real Infinity query envelope and substitutes synthetic frames in the browser response.
The tests do not stop collectors, inject failures into the live lab, validate exporters, or measure human
diagnosis time. Each case saves its input JSON and screenshot under `plugins/grafana/artifacts/scenarios-final`.

| Scenario | Browser assertion on both surfaces | Interpretation guarded |
|---|---|---|
| Errors | 20% measured error share, 5% failure threshold, named rule/source | A relationship symptom is not a proved root cause |
| Latency | 800 ms p95 and 500 ms limit with `checkout-latency` rule | Statistic, value and threshold remain distinct |
| Queue backlog | 1,200 ready messages, 1,000 limit and `orders-queue` provenance | Queue pressure is a measured resource signal |
| Source outage | Retained topology, unavailable current measurements, collector timeout and source state | Retention does not renew healthy traffic |
| Shared service | Checkout keeps shared `web`, removes browse-only `catalog`, shows boundary port | Ownership scope does not correlate transactions |
| Ghost flow | Undeclared-but-observed explanation while measured health remains healthy | Architecture drift and measured health are different |
| Unknown errors | Traffic is present; failure measurements and healthy status are absent | Missing errors do not become measured zero |

```powershell
docker compose -f lab/compose.yaml -f lab/compose.visual.yaml run --rm `
  visual npm run e2e -- --project=chromium --workers=2 --output artifacts/scenarios-final scenarios.spec.ts
```

These automated checks are prerequisites for the following human sessions, not substitutes for them.

## Freeze the test before recruiting

Record the Teia commit, plugin ZIP/hash, browser, Grafana version, viewport, input mode and fixture revision.
Confirm all prerequisite automated cases pass. Save one initial snapshot and the exact evidence URLs for each
task; use an isolated lab with synthetic identities. Reset filters, grouping, selection and local storage
between participants. Disable traffic animation for the timed run, then separately ask whether animation helps.

Use five representative users: two platform/SRE engineers, two application developers and one person who
regularly joins incident investigations without owning observability tooling. Include at least two who have
not seen Teia. Record role and prior Grafana/service-map experience with pseudonymous IDs P01–P05. Do not put
names, credentials or customer topology into the repository. Obtain consent before recording screens or audio.
Recruiting and contacting people is a human owner task; this guide does not authorize messages to others.

Give the same two-minute introduction: nodes represent components, lines represent communications, and a
detail panel opens on selection. Do not explain where the failing component is. Ask participants to think
aloud. Start the timer when the task appears; stop on a stated answer or the timeout, not merely on a click.
Record any coaching and retain coached results separately. Alternate Teia/Novatec order across participants
for overlapping tasks and change service names in the second fixture to reduce memorization effects.

## Task cards and moderator answer key

| ID | Neutral task shown to participant | Correct result / evidence to prepare | Limit and measures |
|---|---|---|---|
| P1 | “Requests in this path are failing. Identify the affected communication and explain what the number means.” | Synthetic `checkout → payments`: 10 total req/s, 20% measured errors. Distinguish a relationship symptom from a proved root cause. | 60 s; path, unit, denominator and claimed cause |
| P2 | “Can we conclude the database communication is healthy?” | `payments → payment-db` has traffic and missing error measurements. Answer: errors unknown; traffic alone does not prove health. | 60 s; unknown versus measured zero |
| P3 | “Open the evidence you would inspect next for this communication. Confirm service and period.” | In the live lab choose a failing connection with configured trace/log/dashboard links. Check caller/target and original sample or suggested query interval. Search is not an exact trace ID. | 120 s; time, intentional actions, wrong destination/context |
| P4 | “Investigate checkout without treating every request through the shared frontend as a checkout transaction.” | Lab `web` belongs to checkout and browse; scope is explicit topology membership, with labelled boundaries. It does not correlate individual operations. | 120 s; shared-service interpretation, lost context |
| P5 | “The collector was interrupted and then the view was paused. Is this live, healthy data?” | Use a saved stale/source-outage case. Retained evidence keeps its times; pause freezes the snapshot; collection age is distinct from original observation age. | 120 s; stale/current confusion and unknown health |
| P6 | “Open the earlier stored observation and return to the live map.” | History-enabled candidate with two known snapshot IDs, different topology and a documented gap. Stored observations are not continuous/interpolated history. | 120 s; correct ID, historical/current distinction, gap interpretation |
| P7 | “A service is missing from the declared architecture, or its dependency lacks monitoring permission. Explain the next action.” | Prepared rogue-SQL/ghost and restricted PostgreSQL cases. Review the generated contract/advisor explanation; do not grant production permissions in the session. | 180 s; distinguishes architecture drift, missing coverage and measured failure |

Run P1–P3 in standalone and Grafana, balancing interface order; run P4–P7 once per participant to keep the
session under 45 minutes. Verify keyboard-only filter/selection/back/close and the 600×400 Grafana viewport in
a separate untimed exercise. Record obstructed controls, focus loss and unreadable labels with screenshots.
After each timed task, ask how confident the answer is (1–5) and what evidence would change it.

P1/P2 and selected-node navigation are overlapping comparison tasks. P3 is comparable only after both tools
have working, equivalent destination templates and input periods. P4/P6 require explicit supported equivalents
before assigning scores. P7 measures Teia's contract/advisor value; do not count a graph-only panel's different
scope as a timed failure. Average response time and p95 are different statistics and must not be scored as equal.

## Proposed acceptance gate

At least four of five participants should identify the affected path within 60 seconds and reach supporting
evidence within 120 seconds without coaching. All five must distinguish unknown errors from measured healthy
behavior in P2. Record task completion, timeouts, wrong conclusions, coaching, navigation errors and context
loss. Satisfaction alone does not meet the gate. These are proposed targets from T12, not reported outcomes.

Use [pilot-results.csv](pilot-results.csv), one row per participant/tool/interface/task, leaving uncollected
results empty. Review every severe misinterpretation; fix and rerun affected tasks before release. A sample of
five is useful for finding problems but does not prove market superiority or support population-wide latency
claims. Report observed wins, ties and unresolved issues with the task and environment attached.

## Installed Novatec control

The optional [comparison dashboard](../examples/grafana/dashboards/novatec-comparison.json) uses the same three
component identities and two directed relationships as the Teia candidate. Its mapping explicitly supplies
8 successful and 2 failed calls for the 20% error relationship; Novatec's inspected generator divides failed
calls by successes plus failures. Its mean-duration field is **not** the Teia fixture's p95, so timing statistics
are excluded from the overlap score. A separate panel uses Novatec's official dummy data as a setup control.
The installed detail table uses a different denominator and displays 25% for those same 8 successful calls
and 2 failures. Explain this denominator before P1 and accept an answer that accurately distinguishes the
two definitions; do not score the fixture mismatch as a participant error or a faster diagnosis by Teia.

Download version 4.2.0 from the [official catalog](https://grafana.com/grafana/plugins/novatec-sdg-panel/), validate
the SHA256 from the [version metadata](https://grafana.com/api/plugins/novatec-sdg-panel/versions/4.2.0), and extract
the `novatec-sdg-panel` directory. Set `NOVATEC_PLUGIN_DIR` to that absolute path and append
`-f examples/grafana/compose.novatec.yaml` to the candidate Compose command. No unsigned exception is configured
for Novatec. Its installed settings endpoint must report version 4.2.0 and `signature=valid` before comparing.

```powershell
docker compose -f lab/compose.yaml -f lab/compose.visual.yaml run --rm `
  -e GRAFANA_URL=http://host.docker.internal:3481 -e TEIA_NOVATEC=true `
  visual npm run e2e -- --project=chromium --workers=2 --output artifacts/novatec-13.2.4 novatec.spec.ts
```

Use the repository's [documented capabilities and limitations](landscape.md) as hypotheses to test, not scores.
The [installed comparison](competitive-validation.md) records the verified mappings, baseline, drilldown and
error-denominator differences. Preserve `pluginVersion: "4.2.0"` in the provisioned Novatec panels, and keep
incoming/outgoing frames separate. Its navigation receipt proves URL context only; P3 still needs equivalent
real telemetry destinations before it can be scored across products.
The [official Novatec documentation](https://github.com/NovatecConsulting/novatec-service-dependency-graph-panel)
describes column mappings, layering, sorting and node drilldown. Verify those workflows in the installed build
before changing comparative claims. Record setup time and configuration failures separately from investigation
time; do not make users compensate for an unverified fixture.

Kiali is not installed in this lab: it has no Istio mesh or equivalent Kiali telemetry fixture. A fair Kiali
comparison needs a separate mesh environment with the same task semantics. No performance, usability or
capability win over Kiali is claimed from this omission.
