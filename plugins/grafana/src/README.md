# teia

**Who talks to whom, compared with what teams declared.** A live communication map for Grafana.

- **Health at a glance**: each card's tile turns green, amber or red with its error rate.
- **Traffic you can see move**: dots travel along each communication at the speed of its traffic; red dots are errors.
- **Drift**: dashed lines are communication nobody declared, dotted ones were declared but never seen.
- **Blind spots**: card borders show how first-hand teia's view is, and the *Next steps* drawer says exactly what to do to see more.
- **One icon library**: microservices, relational and document databases, caches, Kafka streams, queues, gateways, jobs, external SaaS.
- **Group by any label** (team, system, namespace), search with `team:checkout` or `tech:postgresql`, drag, zoom, export PNG/SVG.

## Data

The panel reads the frames returned by its queries:

| Query refId | Rows | Typical source |
|---|---|---|
| `nodes` | one per service/datastore: `id`, `kind`, `tech`, `health`, `visibility`, `labels`... | teia `/api/v1/grafana/nodes` |
| `edges` | one per communication: `source`, `target`, `state`, `health`, `requestRate`, `errorRate`, `signals`... | teia `/api/v1/grafana/edges` |
| `findings`, `sources`, `changes` | optional extras | teia `/api/v1/grafana/findings`, `/sources`, `/changes` |
| `coverage` | optional, one row | teia `/api/v1/coverage` |

Any Node Graph shaped data (`id` / `source` / `target`, such as Tempo's service graph) also renders.

Alternatively, set **teia API URL** in the panel options to stream straight from teia. The viewer's browser connects
to it, so use an address the browser reaches (for example `http://localhost:8480/api/v1/`) and list Grafana's origin
in teia's `cors.allowedOrigins`. Browsers cannot send a token on that stream: when teia has `auth.token`, use the
queries above instead.

Built by [Nilum](https://nilum.com.br). Source and docs: https://github.com/nilum-br/teia
