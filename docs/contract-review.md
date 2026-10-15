# Local contract review

Save a graph from `GET /api/v1/snapshot` or an exact snapshot from the opt-in history API. The JSON must contain
`nodes` and `edges`. Review input files are limited to 64 MiB. These commands read local files and write
stdout; they never open a PR, alter a repository file, permit traffic or deploy network policy.

Compare topology, contract and health states between captures:

```sh
teia diff --before before.json --after after.json
teia diff --before before.json --after after.json --format json > topology-review.json
```

Rate/percentile jitter and collection timestamps alone are excluded. Added, removed or changed nodes
and connections are ordered by identity; JSON retains the original records and their evidence. A
removed connection means absence from that capture, not proof that a dependency can be removed.

Compare observed communication with the owning repositories' declarations:

```sh
teia diff --snapshot graph.json --flows orders/flows.yaml --flows payments/flows.yaml
teia diff --snapshot graph.json --flows orders/flows.yaml --config teia.yaml --format json > contract-review.json
```

Pass the same `--config` used to collect the graph when identities depend on aliases; the command
reads only its configuration and does not connect to its sources. Omit `--flows` to compare with an
empty contract. Existing matching declarations are omitted from candidates. Conflicting protocol or
port is an explicit `conflict`, requiring an edit to the existing owner file. Unobserved declarations
are listed separately and never become deletion proposals. Unknown protocol/port is not a conflict.

After the owner reviews identity and evidence, generate only explicitly accepted additions:

```sh
teia diff --snapshot graph.json --flows orders/flows.yaml --format flows \
  --accept 'orders->orders-db=Persist checkout orders' > orders-additions.yaml
teia validate orders-additions.yaml
```

The artifact preserves destination, kind, protocol, port and the supplied rationale. YAML comments
retain source, observation period, retained status, original timestamps when known and the separately
labelled capture time. It is an additive fragment: merge its `spec.egress` entries into the existing
owner document using the repository's conventions, validate the final file, and review that diff in
the normal PR process. It does not duplicate or replace unrelated declarations. An unresolved IP or
identity that cannot round-trip through a Flows reference must be resolved before generation. Named
identities without catalog references remain explicitly qualified; they are not assigned a confidence
percentage. Evidence comments are historical context, not a promise that a later call will occur.

On the next discovery round the accepted observation and new declaration reconcile into an `ok` edge.
The regression suite exercises observed → reviewed artifact → declared source → reconciled graph,
including protocol/port conflicts, aliases, malicious YAML-like evidence and preservation of rationale.

For a batch dependency, edit the accepted declaration to include its intentional interval:

```yaml
apiVersion: teia.nilum.io/v1alpha1
kind: Flows
metadata:
  name: daily-export
spec:
  owner: component:default/daily-export
  egress:
    - to: resource:default/archive
      kind: database
      protocol: postgresql
      port: 5432
      reason: publish the daily finance snapshot
      expectedFrequency: 24h
```

Frequency is not inferred from a short snapshot. [Frequency semantics](flows-spec.md#rare-and-batch-flows)
explain the separate `unknown`, `not-due` and `overdue` assessments and their sampling limitations.
