# Changelog

## 0.1.0 (unreleased)

First release of the teia panel.

- The teia communication map inside Grafana: health, live traffic, drift (declared vs observed), visibility and
  the advisor's next steps, the same component teia serves on its own.
- Fed by the panel queries (teia's `nodes`, `edges`, `findings`, `sources`, `changes` and `coverage` frames, or
  plain Node Graph frames such as Tempo's service graph), or streamed straight from teia's API.
- Group by any label, search with `key:value`, export PNG, SVG and the flow matrix.
- Layout, filters and grouping are remembered per dashboard and panel.
- Keyboard and screen-reader support; follows Grafana's theme and the reduced-motion setting.
- Golden signals with their reasons: latency, errors, saturation, queue backlog and lag, locks and restarts, with a
  tag on each amber or red card and line saying why and where it comes from.
- Journeys: show one journey (option **Journey**, dashboard variables welcome) with stubs where it enters and leaves.
- Values on lines: traffic and p95 on every line (option **Values on lines**).
