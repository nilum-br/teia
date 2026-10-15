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
`GRAFANA_STORAGE_STATE` for authenticated hosts. These are HTTP API tests, not visual/browser validation. The
Chromium project is available for future browser tests and requires Playwright's browser installation.

## Loading it in your Grafana

Until the plugin is signed and in the catalog:

```ini
[plugins]
allow_loading_unsigned_plugins = nilum-teia-panel
```

and copy `dist/` to `<grafana plugins dir>/nilum-teia-panel`. Grafana lets browsers keep plugin files for an hour
under the plugin's version, so a rebuild with the same version needs a hard reload (Ctrl+Shift+R); the lab stamps
each build with its own version instead.

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

### Numeric frame contract

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

Teia endpoints and direct mode provide **current state**. The dashboard time picker does not query history.
The visible time row says so; pausing freezes the received snapshot, including its timestamps. A successful
refresh does not establish the age of an original measurement.

Generic frames can supply `timeMode` (`current` or `query`), `snapshotAt`, `observedAt`, `periodFrom` and `periodTo`
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

Other signals, including RabbitMQ and derived rates/latency, continue to show unknown sample time. A configured
query lookback is not an original sample age or a verified effective interval for arbitrary queries containing
hardcoded ranges, `offset` or `@`. Retained topology keeps its old timestamps and sample count and drops live
readings. Per-edge and per-reading times never become graph-wide freshness.

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
| teia API URL | Direct mode, described above. |

## Roadmap

- a teia data source plugin (Go backend), so no Infinity data source is needed
- signing and the Grafana catalog
