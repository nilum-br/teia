# Grafana integration

## Today: no plugin needed

The lab provisions a dashboard ([lab/grafana/dashboards/teia.json](../../lab/grafana/dashboards/teia.json))
that reads the teia API through the [Infinity data source](https://grafana.com/grafana/plugins/yesoreyeram-infinity-datasource/):

| Panel | Endpoint | Infinity format |
|---|---|---|
| Node Graph | `/api/v1/grafana/nodes`, `/api/v1/grafana/edges` | `node-graph-nodes`, `node-graph-edges` |
| Next steps (table) | `/api/v1/grafana/findings` | `table` |
| Coverage (stats) | `/api/v1/coverage` | `table` |

Edges carry `color` and `strokeDasharray`: dashed orange is observed but undeclared, dotted is declared but
not observed. Nodes are colored by visibility.

## Planned (v0.3): `nilum-teia-app`

An app plugin with a Go backend (`grafana-plugin-sdk-go`), a data source for the graph and findings,
and deep links from teia edges to Tempo / Kiali for RED metrics.

Before submitting to the catalog, confirm the signature level with Grafana: plugins from organizations with a
commercial relationship may need the Commercial signature.
