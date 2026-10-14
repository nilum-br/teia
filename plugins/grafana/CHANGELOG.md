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
