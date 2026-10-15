# Sampled operations

The operation view isolates calls descended from a particular entry span, using traces queried from Tempo.
For example, `web · POST /checkout` and `web · GET /browse` can share `web` and a database while displaying
different intermediate calls. A shared service, ownership label or global topology edge does not establish
operation membership. [Ownership journeys](journeys.md) remain a separate view.

## Instrumentation

An entry span must have a `service.name`, be a server or consumer span, and provide one of these identifiers,
in precedence order:

1. `teia.operation`: an explicitly normalized application operation, such as `checkout`.
2. `http.route` with `http.request.method` (or legacy `http.method`), such as `POST /orders/{id}`.
3. `rpc.service` and `rpc.method`, combined as `Shop/Checkout`.

Identifiers are bounded to 200 bytes per attribute and reject control characters, URL schemes, queries and
fragments. HTTP methods must contain letters or underscores and are normalized to uppercase. Raw span names,
request URLs, SQL statements and ownership labels are not used to derive an operation. Instrumentation must
still supply a low-cardinality, non-sensitive route or explicit operation; Teia cannot determine whether a
literal path embeds a customer identifier. The OpenTelemetry [HTTP span conventions](https://opentelemetry.io/docs/specs/semconv/http/http-spans/)
describe normalized route attributes.

Every matching entry span is a sample root, including an entry nested beneath an upstream service. A trace
can contain several roots and operations. Counts therefore describe sampled entry spans, not distinct user
transactions.

## Attribution and gaps

Only descendants joined by parent span IDs in the same fetched trace enter a root's scope:

- A cross-service server or consumer child produces a `parent-span` call. Its span ID, parent ID and original
  timestamps are retained.
- A client or producer span without a linked remote child can produce a `client-peer` call to its explicit
  peer, address or database system. This is one-sided evidence; it does not demonstrate server instrumentation.
- Retries remain separate calls with distinct span IDs. Overlapping roots in the same operation do not
  duplicate the same call.
- Span links are preserved but are not followed as parent-child ancestry. An asynchronous continuation in
  another trace does not silently join the operation. Consumers can have their own explicitly named operation.
- Missing parents, duplicate or missing span IDs, parent cycles, links and output truncation qualify a sample
  as `gaps-detected`. Spans without a matched entry ancestor appear in `unattributed`, with a reason.

`linked-sample` means no structural gap was detected in the available material. It never means every request,
span or downstream dependency was captured. Missing parents elsewhere in a fetched trace conservatively
qualify its included operation roots. A timestamp omitted by the tracing source remains unavailable.

## Measurements

`sampledRootCount` counts retained matching roots. `incompleteTraceCount` counts distinct fetched trace IDs
with detected gaps for that operation. `sampleRootDurationsMs` contains individual valid root span durations,
including zero; it is not a traffic percentile. Call durations describe the referenced span, not necessarily
network time or complete request latency. Producer/consumer span durations do not establish message delivery
latency.

Root outcomes are separate from platform or dependency health. Explicit error status, `error.type`, or an
HTTP 5xx response marks a root as `error`. Explicit success status or an HTTP 1xx–4xx response marks it `ok`
under server-span semantics. Otherwise its outcome is `unknown`. `ok` does not claim successful business
processing or healthy downstream services. Missing spans cannot establish zero errors.

The Tempo search is bounded and is not a population counter. `sampling.policy` is currently `unknown`;
`sampling.totalRequests` and `sampling.coverage` remain `null`. No request rate, sampling percentage,
population error percentage or end-to-end coverage is inferred from these traces.

Each source records its actual requested `queryPeriod`, collection time, searched/available service counts,
requested/fetched/failed trace counts, and a status:

| Status | Meaning |
|---|---|
| `sampled` | Searches and fetches completed without a detected search/output limit. Sampling completeness is still unknown. |
| `partial` | A fetch failed, a service search was skipped, or a known search/output bound was reached. |
| `unavailable` | The current source could not produce operation evidence. Previous operation samples are not reused as current. |

An empty scope list can mean no matching normalized entry attributes or no matching sampled traces. It does
not prove there were no calls. Retained global topology remains independent from current operation availability.

## API and storage

`GET /api/v1/operations` exposes the version 1 `mode: "operation"` contract. The same object is available as
`operations` on `/api/v1/snapshot` and `/api/v1/graph`. A scope contains its stable source/entry/operation ID,
nodes, edges with call and trace references, roots with spans and gaps, query period and sample summaries.
The [runtime fixture](../internal/journey/testdata/operation-runtime.json) is generated from the
[acceptance specification](../internal/journey/testdata/operation-contract.json) and checked by Go and UI tests.

Grafana receives the contract as a JSON `operations` column on the first node row and on the coverage row.
Coverage transport also works when the global topology has no nodes. Generic external frames without this
contract do not acquire inferred operation correlation. Explicit node aliases are applied to operation IDs
and endpoints; platform health and metric aggregates are not copied into operation scopes.

When snapshot history is enabled, the original operation contract is stored inside each snapshot. Historical
replay preserves its original trace IDs, query interval and collection time; replay does not requery Tempo.
Retention of a Teia snapshot does not guarantee that Tempo still retains the referenced trace.

Operation output has independent limits per source and collection round: 20,000 inspected spans, 20,000
emitted span references across all roots, 200 scopes, 100 roots per scope, 128 gap IDs per root and 32 links per
span. Limits set `truncated` and source status `partial`. The existing Tempo trace-search limits also apply.

## Local lab

The demo supports `/checkout` and `/browse` on `web`. Checkout calls `orders`; browse calls `catalog` and
`recommendations`. Requests to `/` retain the existing combined demonstration. Set `OPERATION_ROUTES: "true"`
on the lab's `loadgen` to alternate `POST /checkout` and `GET /browse`.

To exercise a genuinely shared database, add the following to `catalog` in a local Compose override:

```yaml
services:
  loadgen:
    environment:
      OPERATION_ROUTES: "true"
  catalog:
    environment:
      POSTGRES_DSN: postgres://orders:orders@orders-db:5432/orders?sslmode=disable
      POSTGRES_PEER: orders-db
```

Both catalog and orders then execute a real query against `orders-db`, under their own sampled trace
ancestry. The lab's uninstrumented recommendations service remains one-sided client evidence; its downstream
inventory call cannot appear in a trace operation just because mesh metrics observe it.
