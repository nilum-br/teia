# Installed Novatec comparison, 15 October 2026

This is a reproducible technical inspection, not a human usability result or a claim of universal superiority.
The earlier [market review](landscape.md) describes documented scope; this report adds installed observations.
The [pilot plan](usability-pilot.md) keeps the five-participant study separate from automated cases.

## Environment and controls

- Grafana 13.2.4, Linux amd64, Chromium via Playwright 1.64.1, viewport 1024×768.
- Novatec Service Dependency Graph 4.2.0, downloaded from the
  [official catalog](https://grafana.com/grafana/plugins/novatec-sdg-panel/). ZIP SHA256:
  `8fc375b56525bf8045b013a6f8e391d2627142808d6a665de0429c49e8595a7e`, checked against
  [official version metadata](https://grafana.com/api/plugins/novatec-sdg-panel/versions/4.2.0).
  Installed settings reported `signature=valid`, community signature, Novatec Consulting GmbH.
- Teia candidate `0.1.0-candidate.20261015.4`; installation evidence is in
  [the candidate report](installation-candidate.md). No production traffic or credentials are used.
- [Prepared dashboard](../examples/grafana/dashboards/novatec-comparison.json): the same three identities
  and two directed relationships as the Teia CSV fixture, plus Novatec's official dummy-data control.
  Teia receives 10 total requests/s and a 0.2 error share. Novatec receives 8 successful requests and 2 errors.
  Its supplied duration is a mean, while Teia's is p95: those values are deliberately excluded from ranking.

Two setup mistakes were corrected before comparison. The provisioned panel needs `pluginVersion: "4.2.0"`:
without it, the inspected migration handler may replace modern `options` with legacy `settings`, losing
metric mappings. Separate incoming/outgoing frames avoid simultaneously supplying null source and target
columns; this version's preprocessor treats present null columns as conflicting endpoints. Neither setup
failure establishes general Grafana incompatibility. Signed loading and graph rendering worked on 13.2.4.

## Observed behavior

| Workflow | Installed Novatec 4.2.0 observation | Teia observation / implication |
|---|---|---|
| Installation | Publicly signed catalog plugin loads without an unsigned exception | Teia has reproducible unsigned candidates; public signing/catalog remain external gaps |
| Hierarchy | Layer 0/2 expands to 2/2, preserving namespace containers; fit and selection work | Teia grouping, expansion and scope are implemented and tested; neither layout is universally better |
| Metric details | Selecting `payments` opens incoming/outgoing tables, 600 ms average, and `Bad (> 300ms)` baseline | Teia exposes p95 and the chosen rule/limit/source; mean and p95 are not interchangeable |
| Error denominator | With 8 successful calls and 2 errors, detail shows Requests 8, Errors 2, Error Rate 25; incoming table shows 25%. The inspected graph generator computes success share as 8/(8+2) | Teia shows 20% for 2 failures out of 10 total. The inputs and denominators must be stated before comparing colors or percentages |
| Missing failures | For `payment-db`, errors are null. Its main Errors row is omitted, but the incoming-neighbour table displays 0%; the latency baseline remains good | Teia explicitly says failures are not measured and does not label that communication healthy from traffic alone. This is a concrete difference in this fixture, not proof that every Novatec configuration loses unknowns |
| Node drilldown | `payments` produces a URL with `var-service=payments` and numeric `from`/`to` bounds; after selecting `payment-db`, clicking its link opens a receipt visibly naming `payment-db` | This verifies node/time URL substitution. The receipt has no telemetry; it is not equivalent to opening real traces/logs. Teia's separate lab browser test opens actual Tempo evidence for an edge |
| Source timing and explanations | Not supplied in this comparator fixture; no claim about unsupported functionality from that omission | Teia's seven synthetic scenarios verify source outage, retained topology, unknown sample age and signal/rule explanations on both surfaces |

The official [Novatec source and documentation](https://github.com/NovatecConsulting/novatec-service-dependency-graph-panel)
support the mapping, hierarchy, baseline and drilldown mechanisms. The installed observations above, including
the two different error denominators and the null-error neighbour display, come from the frozen fixture and
its screenshots. The main missing-errors row is **omitted**, not displayed as a measured zero; do not flatten
that nuance into a claim that the whole panel always invents healthy data.

## Reproduce and remaining work

Follow the optional Novatec install in [the pilot guide](usability-pilot.md), then run `novatec.spec.ts` with
`TEIA_NOVATEC=true`. The test verifies the installed version/signature, hierarchy, selected metrics/baseline,
node/time drilldown and the null-error display. Canvas clicks use fixed coordinates after fitting at the
fixed viewport; a layout/version change requires reviewing the screenshots before updating those coordinates.
Artifacts are local under `plugins/grafana/artifacts/novatec-reviewed`; they are regenerated, not release assets.

Before drawing usability conclusions, run P1/P2 with explicitly explained denominators, configure equivalent
real destinations for P3, and collect the five human sessions. Retest custom endpoint names with URL-reserved
characters before claiming drilldown escaping parity. Test light theme, keyboard navigation and a 600×400
Novatec panel separately before claiming accessibility or small-panel superiority. No speed comparison was run.

Kiali was not installed: the existing lab has no Istio mesh or equivalent data fixture. A fair comparison
requires its own mesh environment. No Kiali result is inferred from this omission.
