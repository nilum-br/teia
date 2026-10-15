# Identity resolution and provenance

Node IDs continue to use normalized names and configured aliases. Each node now carries an additive `identity` array explaining the names contributed by its sources. Each connection carries `identity.from` and `identity.to` arrays, so its caller and callee can have different resolution methods. These records describe evidence and limitations; they are not confidence percentages.

| Method | Meaning |
|---|---|
| `explicit` | A flows declaration, explicit inventory label/annotation, or configured explorer node names the entity. A declaration remains intent, not proof of traffic. |
| `name` | The source reports a name, such as a service name, Compose service, container name or Kubernetes owner name. Matching names alone do not establish globally unique identity. |
| `alias` | A configured alias maps the normalized input to the chosen ID. It expresses an operator decision and does not validate an IP lease. |
| `ip-temporal` | The available inventories name one canonical owner, provide a start time, and that start covers the available observation period. The period can come from query evaluation, collection of current state, or original sample times. |
| `inferred` | A name is derived by an inventory convention, or a unique IP owner supplies a name without enough temporal evidence. Retained inventory and incomplete sampled-call timestamps cannot establish current temporal ownership. |
| `ambiguous` | Inventories disagree on an IP owner, or a current owner's start is newer than part of the observed period. No competing owner is silently selected. |
| `unresolved` | No successful inventory observation supplies an owner for an address. The `ip:` ID remains available for investigation. |

`input` preserves the source's original identifier; `resolvedId` is the canonical result, which can still be an `ip:` ID. `source` and `sourceType` identify the reporting adapter. Optional `basis` explains the relevant configuration or naming convention. `candidates` preserves inventory claims separately, including their source, naming basis, reported `since`, original `collectedAt`, and `retained` status. Duplicate identical records are removed; candidate lists are not truncated and are ordered independently of source completion order.

Exporter readings retain their ordered name attempts in optional `alternatives`. They continue to attach to the first known candidate; this is an explicit selection rule, not proof that every candidate is the same entity.

## Time and ambiguity

`collectedAt`, `evaluatedAt`, `observedFrom`, `observedAt` and `windowSeconds` keep their original meanings. Absent times remain absent. `timeBasis` identifies the anchor used for address checks: `evaluation-window`, `collection-window`, `sample-envelope`, `sample-point`, `unknown`, or `not-applicable`. A collection timestamp anchors a current-state reading or an adapter's lookback; it is not substituted for a call timestamp.

A trace sample can contain calls older than the search window. The address check includes its earliest original time. When the reported owner acquired the address after that point, the connection remains withheld, preserving the existing recycled-IP protection. Its candidate nodes carry an `ambiguous` record explaining the withheld attribution. This does not create a new current connection.

If two inventories report different canonical owners for one address, the observed connection stays attached to the unresolved `ip:` endpoint with both candidates. Input ordering does not choose the winner. An explicit alias can intentionally resolve the conflict. A failed discovery contributes no address claims; retained last-good inventory is distinguishable from fresh inventory.

Container creation, pod start and Service creation timestamps are inventory assertions, not complete address-allocation history. In particular, Service creation does not establish when its selector last changed. Kubernetes Deployment/CronJob names derived from ReplicaSet/Job naming conventions are marked `inferred`; their owner objects were not read. Missing times and invalid/future sample periods are never promoted to a verified temporal match. The conservative recycled-IP guard may use the current clock when an older adapter provides no period anchor, but that clock is not written as an observation time and the association remains `inferred`.

## Retention, replay and compatibility

Topology retention preserves the original identity decision and timestamps, marks its records and candidates as retained, and does not extend retention because metadata is read. Previously returned snapshots are not mutated. Persisted history stores the decision made at capture time: replay does not resolve an old IP against today's inventory.

The existing node IDs, connection keys, alias matching and API fields remain available. Older stored snapshots omit identity metadata. Grafana node rows encode `identity` as a JSON array string (`[]` when absent); connection rows encode the endpoint object as a JSON string (`null` when absent).

Names are still global within one Teia graph: the namespace and Compose project are not automatically part of the node ID. Equal names in different scopes can merge. Clusters are separate graphs when the config lists them under [`clusters`](configuration.md#clusters): the same name in two clusters is never merged. Explicit `teia.nilum.io/node-id`, `teia.node`, and aliases can distinguish them. This provenance metadata explains the inputs; it does not introduce namespace-qualified IDs, infer owners through NAT/poolers, or recover uncollected historical IP leases.
