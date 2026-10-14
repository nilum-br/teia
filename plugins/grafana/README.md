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

and copy `dist/` to `<grafana plugins dir>/nilum-teia-panel`. Then add the queries listed in `src/README.md`
(the lab's dashboard, `lab/grafana/dashboards/teia.json`, uses the Infinity data source).

## Roadmap

- a teia data source plugin (Go backend), so no Infinity data source is needed
- signing and the Grafana catalog
