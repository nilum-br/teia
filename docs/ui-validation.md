# UI and performance validation, 15 October 2026

The T01–T16 candidate was exercised in the running Docker lab, standalone and as an installed Grafana 13.2.4
panel. The final ordinary suite passed **43 browser/API cases**, with five opt-in cases skipped (candidate,
Novatec and scale). A subsequent dedicated dense-map navigation case also passed. Separately, the candidate
passed two cases on each of Grafana 13.2.0 and 13.2.4, the installed Novatec control passed two, and the scale
benchmark passed. The 14 synthetic diagnostic scenarios are included in the ordinary suite, not extra counts.

Pure/shared UI checks passed 79 tests; the panel passed 53 Jest tests and two dependency integration tests,
plus TypeScript, ESLint and production build. These numbers describe this candidate, not human pilot results.
The [pilot](usability-pilot.md) is assigned to the colleague after handoff, as requested by the maintainer.

## Investigation and visual review

The browser suite covers light/dark standalone 1600×960 and 1024×768, Grafana panels 900×500 and 600×400,
and keyboard/reflow fixtures at widths 640/960/1600. Captures include overview, node, edge, journey, grouping,
expanded evidence, history and actual external destinations. Representative compact, grouped, evidence,
journey and dense-map captures were visually inspected; live values prevent fixed pixel baselines.

- Details prioritize the selected signal, effective rule/threshold, provenance and next action. Expanded
  evidence, scroll and focus survive refresh; unrelated advice is collapsible.
- Filters retain keyboard focus, Escape works from the search box, tabs support arrow/Home/End navigation,
  and nodes, edges and boundary ports can open details without a mouse. Shared links restore filters,
  hierarchy, selected identity/port and stored history in a clean browser context.
- Current snapshots explicitly ignore Grafana's requested history range. Historical snapshots keep their
  immutable ID/original time; changes from another capture do not contaminate their Activity list.
- Actual Tempo, Loki and metrics dashboard links opened from both interfaces with their identity/time
  context. These are searches, not exact request correlation. Grafana Explore requires an appropriate role.
- Palette checks measure ordinary text against panel/background colors at ≥4.5:1 and graph state colors
  against the map background at ≥3:1. Primary controls measure at least 24 CSS pixels high. Health uses text
  and shapes as well as color. Reduced-motion fixtures disable traffic animation.
- A 200% **CSS zoom** fixture checks reduced-width reflow and visible controls; it is not a substitute for
  every browser's native zoom behavior or a screen-reader audit. Tiny SVG overview targets have equivalent
  search/focus/detail controls. These checks are scoped evidence, not complete WCAG certification.

At 600×400, names are deliberately truncated in the overview. Compact labels retain 12 CSS-pixel text;
Read names/Focus on map exposes complete identities. At 1,000 nodes the overview is a density/orientation
view, not a page on which every name is simultaneously readable. The dense-map browser regression verifies
all 1,000 nodes/1,800 edges remain present and Read names reveals a full selected identity at ≥12 CSS pixels.
Use hierarchy, journey filters and search for investigation. No topology is removed by display suppression.

## Rendered scale measurement

Reference host: AMD Ryzen 7 7700X, 8 cores/16 logical processors; Docker reported 16 CPUs and approximately
33.2 GB memory. Chromium **156.0.8078.4** ran in the Linux Playwright **1.64.1** container on the Windows host,
1600×960 viewport, one worker, reduced motion and **Flow off**. The Playwright desktop device's user-agent
string emulates Windows; it does not change the container OS. The regular telemetry lab remained running.

Fixtures have ten layers, two incoming relationships per non-first-layer node, hierarchy labels, one failing
CPU measurement every seven nodes and provenance. Each case mounts/paints ten times, then measures twenty
selections, health filters, changed-data refreshes and hierarchy operations. Timings include the handler and
two animation frames, so include scheduling/paint latency; they are not layout-only timings. Functional
assertions verify that selection opened, filters cleared and topology counts survived. The initial page-load
measurement includes fixture setup/Playwright overhead; it is reported separately from render samples.

All values below are milliseconds, **median / p95**:

| Nodes / edges | Mount + paint | Selection | Filter | Refresh | Group |
|---|---:|---:|---:|---:|---:|
| 100 / 180 | 237.0 / 495.3 | 50.0 / 66.1 | 33.4 / 42.8 | 33.2 / 46.5 | 33.3 / 33.5 |
| 500 / 900 | 133.3 / 171.9 | 66.6 / 117.7 | 83.3 / 100.1 | 33.3 / 136.4 | 33.4 / 66.9 |
| 1,000 / 1,800 | 272.1 / 318.2 | 120.9 / 151.2 | 95.7 / 117.5 | 64.4 / 83.0 | 33.3 / 135.9 |

Initial page-load measurements were 403/471/802 ms respectively. The 100-node mount is sometimes slower
because it still renders detailed annotations; above 200 nodes the overview suppresses those annotations.
The proposed **200 ms p95 selection/filter budget passed this reference run at all three sizes**. This is
not a guarantee for arbitrary graph density, animation enabled, low-end hardware or 1,000-node small panels.
No evidence from this run requires replacing the renderer; profile again for a workload outside this envelope.

CDP garbage collection was requested after the interaction phase and after each of four batches of five
additional refreshes. Retained JS heap (decimal MB) was:

| Nodes | Baseline | +5 refreshes | +10 | +15 | +20 |
|---|---:|---:|---:|---:|---:|
| 100 | 5.25 | 5.15 | 5.14 | 5.16 | 5.14 |
| 500 | 8.47 | 7.66 | 7.67 | 7.79 | 7.69 |
| 1,000 | 12.14 | 11.29 | 11.30 | 11.29 | 11.32 |

This short run shows no sustained retained-heap growth; it is not a long-running leak proof or total browser
memory measurement. `performance.memory` was quantized, so the CDP samples are used for this conclusion.
[Raw samples](validation/scale-2026-10-15.json) retain all timings, counts and memory readings.

## Reproduce

With the lab and browser runner built as in the [panel README](../plugins/grafana/README.md):

```powershell
docker compose -f lab/compose.yaml -f lab/compose.visual.yaml run --rm `
  visual npm run e2e -- --workers=2 --output=artifacts/final-review
docker compose -f lab/compose.yaml -f lab/compose.visual.yaml run --rm `
  -e TEIA_BENCHMARK=true visual npm run e2e -- scale.spec.ts --workers=1 --output=artifacts/scale-final
```

The first command now also includes the new dense-map navigation regression. Use a quiet reference machine
for timings. Local screenshots live under `plugins/grafana/artifacts/`; the benchmark also writes
`artifacts/scale/results.json`. They are ignored build artifacts. CI runs the ordinary interaction suite and
uploads `visual-review`; candidate/comparison/scale are explicit separate runs. See
[installation evidence](installation-candidate.md) and [the installed comparison](competitive-validation.md).
