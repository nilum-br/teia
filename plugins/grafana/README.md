# nilum-teia-panel

The teia map as a Grafana panel plugin. User-facing description: [src/README.md](src/README.md).

## How it is built

The map itself is **one component**, `internal/api/ui/teia-ui.js`, shared with the UI teia serves. Every
`npm run build|dev|typecheck|test:ci|lint` first copies it into `src/teia/` (`scripts/sync-ui.mjs`), so there is a
single source of truth. The panel (`src/components/TeiaPanel.tsx`) mounts it in a shadow root and feeds it the
query frames (`src/teia/frames.ts`).

```sh
npm exec --yes --package=npm@12.2.0 -- npm ci
npm run typecheck && npm run test:ci && npm run build   # dist/ is the plugin
```

`create-plugin` and the build tooling only run on Linux and macOS. On Windows, use WSL or a container; the lab
does exactly that (`teia-panel` service in `lab/compose.yaml`) and mounts `dist/` into Grafana.

`.npmrc` keeps `ignore-scripts=true` (supply-chain hygiene), which is why scripts call the sync explicitly instead
of using `pre*` hooks.

Use Node 26.12.0 (latest Current release in the 15 October 2026 audit), npm 12.2.0 and ESLint 10.12.0.
The TypeScript CLI is 7.0.2 via the official
`@typescript/native` alias; tools needing the compiler API use `typescript` aliased to `@typescript/typescript6`
6.0.2, following [Microsoft's migration guidance](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/).
See [dependency maintenance and compatibility exceptions](../../CONTRIBUTING.md#dependency-maintenance).

`npm run deps:check` compares all locked direct dependencies with current stable npm releases. Major upgrades
are in scope. Root ESLint, Playwright and Webpack configs are project-owned replacements for the incompatible
or vulnerable integrations left in the managed `.config/` template. Webpack retains Grafana's bundling helpers
and uses ESLint's API for development rebuilds. Review scaffold updates against these configs.

`npm run deps:audit` audits the complete dependency tree and is a CI gate. The 15 October 2026 remediation
removed all 26 remaining alerts. The panel consumes Grafana's public `ThemeContext` directly, so it does not
install the UI/runtime/router compatibility tree. A scoped YAML 5 override removes Istanbul's legacy formatter
dependency. `npm run test:ci` includes regressions for that YAML loader and Webpack lint reporting; it also checks
live theme changes without remounting the map. See the maintenance notes above for override removal conditions.

With the lab running, `npm run e2e -- --project=api` validates the installed plugin bundle and the saved
dashboard's queries for two requested periods. `GRAFANA_URL` defaults to `http://localhost:3480`; optionally set
`GRAFANA_STORAGE_STATE` for authenticated hosts. These are HTTP API tests.

`npm run e2e -- --project=chromium --workers=2` exercises the rendered standalone UI and installed Grafana
panel in light/dark themes at 1600×960, 1024×768, 900×500 and 600×400. Set `TEIA_URL` if standalone Teia is not
at `http://localhost:8480`. First install Chromium with `npx playwright install --with-deps chromium`, or use
the isolated Docker runner from the repository root with the lab already running:

```sh
docker compose -f lab/compose.yaml -f lab/compose.visual.yaml run --build --rm visual
```

The runner uses Playwright 1.64.1, Node 26.12.0 and npm 12.2.0; it reuses the lab panel's installed dependencies.
Screenshots of overview, node/edge details, sample evidence, journeys and grouping are written to `test-results/`
and uploaded by CI as `visual-review`. Tests check controls stay in view, details remain closable, the selected
node/edge endpoints are not covered by the sidebar, metric/reason labels and journey boundary ports open their
details, manual pan/zoom/selection survive resumed updates, and a 30-day Grafana range still
says current state. Captures require human/agent review; passing interactions alone does not certify legibility,
contrast or absence of every label collision. Live values change, so these are not fixed pixel baselines.

## Loading it in your Grafana

Development candidates are unsigned. For an explicitly configured test installation:

```ini
[plugins]
allow_loading_unsigned_plugins = nilum-teia-panel
```

and copy `dist/` to `<grafana plugins dir>/nilum-teia-panel`. Grafana lets browsers keep plugin files for an hour
under the plugin's version, so a rebuild with the same version needs a hard reload (Ctrl+Shift+R); the lab stamps
each build with its own version instead.

Use the [candidate installation and release process](../../docs/installation-candidate.md) for a versioned ZIP,
portable three-node dataset, clean-install/upgrade matrix and signing gates. A valid signature and catalog
acceptance are separate external milestones. Tagged release packaging requires public signing; manual builds
remain unsigned candidates until those requirements are met.

## Feeding it

**Dashboard queries** (recommended, and the only way when teia has `auth.token`). Grafana's server calls teia, so
the URL is one Grafana reaches (`http://teia:8080` in the lab) and the token stays on the server. Any JSON data
source works; the lab's dashboard (`lab/grafana/dashboards/teia.json`) uses Infinity, with teia's address set once as
the data source's URL and paths only in the queries, so the dashboard imports into any Grafana. One query per frame,
with these refIds:

| refId | teia endpoint |
|---|---|
| `nodes` | `/api/v1/grafana/nodes` |
| `edges` | `/api/v1/grafana/edges` |
| `findings` | `/api/v1/grafana/findings` |
| `sources` | `/api/v1/grafana/sources` |
| `changes` | `/api/v1/grafana/changes` |
| `coverage` | `/api/v1/coverage` (already one row; there is no `/api/v1/grafana/coverage`) |

Only `nodes` and `edges` are required. With `auth.token`, add the header `Authorization: Bearer <token>` in the data
source (from a secret, not in the dashboard JSON).

**Slices.** To pick [slices](../../docs/journeys.md#slices-follow-the-calls) on the panel, add a dashboard variable (a
hidden textbox named `slice`, as in the lab's dashboard) and pass it to teia in the `nodes`, `edges`, `findings` and
`coverage` queries as `?slice=${slice:percentencode}`. The panel's **Calls of** buttons then set the variable, the
frames that come back say which slice they hold, and ✕ empties it for every call. Without the variable the panel
offers no buttons but still labels frames a query sliced. Direct mode slices through teia's API instead.

**Clusters.** For a teia that maps [several clusters](../../docs/configuration.md#clusters), the lab's dashboard has
a `cluster` variable: an Infinity query of teia's `/api/v1/clusters` (root `clusters`, column `name`), passed to every
query as `cluster=${cluster:percentencode}`. Grafana shows it as the dashboard's cluster picker, and the dashboard's
other panels can use `$cluster` too. With a single scope it has one empty option: teia's only map.

**Refresh.** teia's map changes once a round (30 s by default). The panel does not redraw a refresh that brings the
same round, but Grafana still fetches and decodes the frames each time: for a cluster with thousands of connections
that is seconds of Grafana's CPU per viewer and refresh. For large maps, refresh at teia's interval or slower.

### Numeric frame contract

For queries with different column names, use the panel's setup form or the optional **Column mapping (JSON)**
option. The stored `frameMapping` maps canonical fields to source columns:

```json
{
  "nodes": {"frame": "ref:A", "id": "service_name", "kind": "component_kind"},
  "edges": {"frame": "ref:B", "source": "caller", "target": "callee", "requestRate": "calls_per_second", "errorRate": "failed_share", "latencyP95Ms": "p95_ms"}
}
```

Frame keys come from the setup's input descriptors: `ref:A`, `name:services`, or `ref:A/name:services` when both
exist. Each part is URI-encoded; frame order never determines identity. Duplicate identities, unnamed selected
frames, duplicate columns and missing selected columns produce an actionable error. Give queries unique refIds
or frame names. Mapping adds aliases while preserving existing canonical fields, source values and metadata;
it refuses to overwrite an existing canonical field with another column. Other frames remain available.

Convert units in the query before mapping. Declared units incompatible with the target (for example seconds
for `latencyP95Ms` or 0–100 percent for `errorRate`) are rejected. Unitless columns still require the contracts
below; choosing a column asserts its meaning and does not infer that meaning from its name. Formatted values
and missing errors retain their invalid/unknown states. Empty mapping restores ordinary canonical imports.

`requestRate` is a nonnegative requests/second number; `errorRate` is a share in `[0, 1]`, and
`latencyP50Ms`/`latencyP95Ms` are nonnegative milliseconds. Numeric strings are accepted, but formatted strings
such as `10 req/s` are invalid. Canonical columns take precedence over aliases, including invalid/null cells.
For generic Node Graph inputs, `secondarystat` can supply a numeric rate and node `arc__failed` an error share.

An explicit zero error rate is measured zero. Missing, null or invalid errors remain unknown, even with positive
traffic or `errorsUnknown: false`. The importer retains their distinction in `metricStatus`. An explicit
`errorsUnknown: true` overrides a numeric error cell. A supplied health/reason from another signal is preserved;
rate alone cannot establish health. Missing/invalid visibility defaults to `partial`, never `full`.

`errorCoverage`, when supplied, is a share in `[0, 1]`: `errorRate` describes that measured subset. Without it,
a valid measured error rate covers the reported traffic. Aggregates weight by measured traffic and display
partial coverage; unknown traffic does not enter the error-rate denominator. Visibility means first-hand
instrumentation, not the fraction of error measurements, and must be supplied explicitly to claim `full`.

`latencyApproximate: true` labels a summary of percentiles, not a percentile of a combined distribution. A group
of multiple measured latencies is approximate even if each member is exact. `latencyCoverage` is the fraction
of reported traffic with p95 measurements; unknown values do not enter the weighted denominator. A single
sampled edge can supply latency without rate, but cannot establish traffic-weighted group latency.

### Time and evidence contract

The `/api/v1/grafana/*` endpoints and direct mode initially provide **current state**. The dashboard time picker does not query history.
The visible time row says so; pausing freezes the received snapshot, including its timestamps. A successful
refresh does not establish the age of an original measurement.

Generic frames can supply `timeMode` (`current`, `query` or `historical`), `snapshotAt`, `observedAt`, `periodFrom` and `periodTo`
as ISO 8601 columns or in `frame.meta.custom.teia`. Row columns take precedence. Collection/observation times
are only reported globally when all relevant node/edge records provide them (the oldest is shown); effective
period requires a consistent interval. Mixed current/query inputs are labelled mixed; missing timing stays
unknown. Conversion time and Grafana's requested range never substitute for these fields.

`evidence` is a JSON array with `source`, `signal`, `collectedAt`, optional `observedAt`, `windowSeconds`, `retained`
and `metrics`. Optional `origin` identifies the probe/exporter; `evaluatedAt` records query evaluation, distinct
from source collection completion. Sampled evidence can also supply `sampleCount` and `observedFrom`: the earliest start and latest
end (`observedAt`) of the sampled calls. This envelope is neither continuous coverage nor the service's last
call; count is not total traffic or req/s. The detail pane preserves each evidence entry through grouping and
shows age **at that evidence's collection**, including a warning for future timestamps. Missing or reversed
sample bounds leave the sample time range unknown, while keeping its count.

The Tempo adapter supplies these original span timestamps; if any sampled call lacks valid times, both bounds
stay unknown. Its queries use one search window per round. Mapped Prometheus gauge readings supply
`oldestSampleAt`, the oldest input sample reported for the evaluation, including capacity when required.
Missing timestamps leave that reading's age unknown; a timing-query failure does not discard measured health.
The UI shows each reading separately. This is the stored metric's time, not necessarily an application's
last activity. Queries share an evaluation time but do not freeze ingestion or form an atomic snapshot.
See [supported readings and limits](../../docs/sources.md#metric-evaluation-and-input-sample-age).

RabbitMQ gauges also preserve their input timestamps. Derived rates/latency and signals without supplied
original timestamps continue to show unknown sample time. A configured
query lookback is not an original sample age or a verified effective interval for arbitrary queries containing
hardcoded ranges, `offset` or `@`. Retained topology keeps its old timestamps and sample count and drops live
readings. Per-edge and per-reading times never become graph-wide freshness.

For stored observations, import [`lab/grafana/dashboards/history.json`](../../lab/grafana/dashboards/history.json)
and set its `snapshot` textbox variable to an ID from `/api/v1/history`. Every query uses
`/api/v1/history/${snapshot}/grafana/<role>`, including `coverage`. All nonempty rows must carry the same
`timeMode: historical` and `historyId`, either as columns or in `frame.meta.custom.teia`; mixing auxiliary
findings/sources/changes from another capture produces an explicit mixed state. JSON `history` metadata
on the first node or coverage row preserves identity/presence, including an empty topology. The dashboard URL
keeps `var-snapshot`; changing its requested time range does not resample or interpolate that capture.
Standalone and direct mode expose a History picker with UTC interval filtering and a return-to-current action.
See [retention, restart and replay semantics](../../docs/configuration.md#observation-history).

`links` is a JSON array of labeled HTTP(S) destinations with evidence periods and descriptions;
see [evidence-link templates](../../docs/evidence-links.md). `healthRules` and reason `rule`/`unit` identify the
effective threshold. Connections can include `expectedFrequencySeconds` and JSON `frequency` with
`status`, original `lastObservedAt`, `nextExpectedAt` and a qualified `detail`; see
[contract review](../../docs/contract-review.md). None of these fields establishes missing telemetry.

**Direct mode** (`teia API URL` in the panel options). The viewer's **browser** loads `snapshot` and `changes` and
then follows `stream` (Server-Sent Events), so:

- the URL is one the browser reaches, ending in `/api/v1/`: `http://localhost:8480/api/v1/` for the lab, not
  `http://teia:8080`, which only resolves inside the lab network;
- teia must allow Grafana's origin: `cors.allowedOrigins: [http://localhost:3480]` for the lab, your Grafana's
  `https://grafana.example.com` otherwise;
- it only works when teia has no `auth.token`: `EventSource` cannot send an `Authorization` header, and teia never
  accepts its session cookie from another origin. Keep such a teia on a private network, or use dashboard queries.

## Options

| Option | What it does |
|---|---|
| Journey | Open on one journey instead of the whole map: a journey name (`checkout`), any label (`team=payments`), or what a service depends on (`from:orders`). Takes dashboard variables such as `${journey}`; "All" or an unresolved variable shows the whole map. Viewers pick journeys and any other filter on the panel itself. See [journeys and filters](../../docs/journeys.md). |
| Journey label | The label naming journeys. Empty: the first your services carry of `journey`, `app.kubernetes.io/part-of` and `system`. |
| Values on lines | Traffic and p95 latency on every line; an amber or red line shows why instead. |
| Group by label | Initial grouping, for example `team`, `system` or `k8s.namespace`. Viewers can change it. |
| Animate traffic | Dots travel along each communication at the speed of its traffic; red dots are errors. |
| Slice variable | The dashboard variable the queries pass as `?slice=` (default `slice`); the **Calls of** buttons set it. Empty turns the buttons off. Not used in direct mode. |
| teia API URL | Direct mode, described above. |

The map's **Share / export** dialog encodes filters, grouping, expanded hierarchy, selection and display
options in a panel-specific `teiaView-<storageKey>` URL parameter. Dashboard variables remain separate.
Opening the link in a clean session restores that view; edits update an existing shared parameter so reload
does not bring stale filters back. Share links contain identities and filters, so treat them like dashboard URLs.

**Investigate** opens the [investigation workspace](../../docs/investigation.md): bounded neighbourhood, directed
path and table; comparison of two captures; source and column setup; and sampled operations. With query data,
compare uploaded or captured snapshots, map columns, and export a proposal's input for `teia review propose`.
Stored-history comparison and server-validated proposals need a teia API the browser reaches (direct mode).

Where labels exist, ordered grouping supports cluster → namespace → service, with stable identities through
expand/collapse. Neighbours can be ranked by traffic, measured errors or latency. Large overviews suppress
annotations progressively; **Read names** focuses a component at a readable zoom, and search/selection/details
retain access to every item. No topology is truncated by that display policy. **Flow** controls animation;
**Live** controls incoming updates. Keyboard, contrast and scale evidence is recorded in
[UI validation](../../docs/ui-validation.md).

## Roadmap

- a teia data source plugin (Go backend), so no Infinity data source is needed
- signing and the Grafana catalog
