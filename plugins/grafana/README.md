# nilum-teia-panel

The teia map as a Grafana panel plugin. User-facing description: [src/README.md](src/README.md).

## How it is built

The map itself is **one component**, `internal/api/ui/teia-ui.js`, shared with the UI teia serves. Every
`npm run build|dev|typecheck|test:ci|lint` first copies it into `src/teia/` (`scripts/sync-ui.mjs`), so there is a
single source of truth. The panel (`src/components/TeiaPanel.tsx`) mounts it in a shadow root and feeds it the
query frames (`src/teia/frames.ts`).

```sh
npm ci
npm run typecheck && npm run test:ci && npm run build   # dist/ is the plugin
```

`create-plugin` and the build tooling only run on Linux and macOS. On Windows, use WSL or a container; the lab
does exactly that (`teia-panel` service in `lab/compose.yaml`) and mounts `dist/` into Grafana.

`.npmrc` keeps `ignore-scripts=true` (supply-chain hygiene), which is why scripts call the sync explicitly instead
of using `pre*` hooks.

## Loading it in your Grafana

Until the plugin is signed and in the catalog:

```ini
[plugins]
allow_loading_unsigned_plugins = nilum-teia-panel
```

and copy `dist/` to `<grafana plugins dir>/nilum-teia-panel`.

## Feeding it

**Dashboard queries** (recommended, and the only way when teia has `auth.token`). Grafana's server calls teia, so
the URL is one Grafana reaches (`http://teia:8080` in the lab) and the token stays on the server. Any JSON data
source works; the lab's dashboard (`lab/grafana/dashboards/teia.json`) uses Infinity. One query per frame, with
these refIds:

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
| Journey | Show one journey instead of the whole map: a value of the `journey` label (`checkout`), any label (`team=payments`), or what a service depends on (`from:orders`). Takes dashboard variables such as `${journey}`; "All" or an unresolved variable shows the whole map. See [journeys](../../docs/journeys.md). |
| Values on lines | Traffic and p95 latency on every line; an amber or red line shows why instead. |
| Group by label | Initial grouping, for example `team`, `system` or `k8s.namespace`. Viewers can change it. |
| Animate traffic | Dots travel along each communication at the speed of its traffic; red dots are errors. |
| teia API URL | Direct mode, described above. |

## Roadmap

- a teia data source plugin (Go backend), so no Infinity data source is needed
- signing and the Grafana catalog
