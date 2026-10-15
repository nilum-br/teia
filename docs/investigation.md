# Investigation workspace

The **Investigate** button, on teia's own page and in the Grafana panel, opens one workspace with four tools:
focus and list, snapshot comparison, data onboarding, and sampled operations. Two more investigation aids live
in the details of a selected item: identity provenance and contract proposals. They keep the rules the map
already follows: missing data stays unknown, and a view that leaves part of the topology out says how much.

## Focus and list

- **Limited neighbourhood:** the components within one to three hops of a start component, following callers,
  dependencies or both. **Show neighborhood** in a component's details opens the one-hop view directly.
- **Shortest directed path:** the fewest connections from a start component to a destination, in the
  direction of the calls. **Find path…** in a component's details opens the form with the start filled in.
  A topology path shows that these communications exist; it does not prove that one request traversed them.
  "No directed path in the available topology" is not proof that no communication exists.
- **Table:** the same scope as rows of components and connections, 50 per page, sorted by health, name or
  traffic. Search narrows the rows; choosing a row opens its details. The table replaces the map, so the map's
  zoom and legend are hidden while it is shown.

A focused view shows at most 200 components. A bar above the map counts the components outside the view and
the connections that cross its border. A path longer than the limit is rejected rather than shortened. Journey
and facet filters still apply first, and so does a [slice](journeys.md#slices-follow-the-calls): the view then
starts from the selected calls only. **Reset investigation** restores the whole map, and shared links keep
the investigation scope and presentation.

## Compare snapshots

Choose a **before** and an **after** capture: an exported snapshot JSON file (at most 16 MiB, 10,000
components and 50,000 connections), the snapshot currently shown, or a stored observation when history is
enabled and the browser reaches teia's API (the newest 200 are listed). The comparison keeps the order you
chose, and says so when "before" is the newer capture.

A difference is a change in presence; in a component's kind, technology, health, visibility, labels, addresses
or error-measurement availability; in a connection's state, health, declared or observed status, protocol or
port; or in an identity decision (input, resolved ID and method). Rates, latency values, collection times and
round numbers are not differences: they move on every collection. Identity is compared only when both
captures carry identity metadata, so an older store without it does not mark every component as changed.

Both captures appear side by side as frozen maps. Marks are text as well as colour: **+** only in the after
capture (green outline), **−** only in the before capture (red dashed outline), **~** changed (amber outline).
Unchanged items are muted. A table lists every difference, with buttons that locate an item on both maps, and
**Download comparison** saves both captures with the differences as JSON.

"Only before" means absent from the second capture. It does not prove that traffic stopped or that a
declaration should be removed: each capture is a discrete observation, not continuous coverage.

Captures are compared only within the same scope. With a slice on the map, "the snapshot currently shown" holds
only the selected calls, while stored observations hold every call; a slice against the whole map, or against
another slice, is not compared, because everything outside the slice would read as a difference.

## Connect data

- **Source status:** one card per configured source with its state, the components and connections it
  contributed, its last message and whether its topology is retained from an earlier round. The workspace also
  counts the components and connections whose errors are not measured.
- **Server sources** (teia's page and direct API mode): prepare a Prometheus or Tempo entry for `teia.yaml`.
  The endpoint must be HTTP(S) without credentials, query parameters or fragment, and reachable from the teia
  server. The fragment is downloaded for review; the running server is not changed. Supply credentials through
  the server's environment, never in the URL. See [configuration](configuration.md).
- **Grafana query fields:** choose the query frames for components and connections and the columns that hold
  their identities and measurements. Units must already be requests/second, a 0–1 error share and milliseconds;
  declared units that disagree are rejected rather than converted. The mapping is stored in the panel's
  `frameMapping` option; save the dashboard to keep it. See the [panel README](../plugins/grafana/README.md#numeric-frame-contract).

## Operations and traces

Pick an entry operation, such as `web · POST /checkout`, and optionally one trace. The map then shows only the
calls descended from those sampled entry spans. Health, rates and error shares are not carried into this view:
a sample neither establishes service health nor counts traffic. The workspace states the Tempo query period,
how many traces were requested and returned, structural gaps, root outcomes and the range of sampled root
durations, which is not a percentile. Ownership journeys remain a separate, topological view.
With a slice, the list holds the operations whose sampled entries carried it, each narrowed to those traces
and their calls in the slice; an operation shown on the map stays open across slices and says when one did not
sample it. See [sampled operations](operations.md) for instrumentation and limits.

## Identity and provenance

The details of a component or connection list how each name was resolved: one card per decision, with every
source that reported it. Consequential methods come first: ambiguous, unresolved, inferred, IP temporal match,
alias, explicit name, reported name. Cards show the original input, the result, the time basis, inventory
candidates with their reported start, and whether the decision is retained from an earlier round. No confidence
percentage is assigned. See [identity resolution and provenance](identity-resolution.md).

## Contract proposals

The details of an observed, undeclared connection offer **Review contract proposal** when the data carries
declared intent; plain query frames without that intent do not. Write the reason, and teia validates the
request against the exact capture you reviewed: a newer capture returns a conflict instead of silently
proposing against different evidence. The result is a Flows fragment, a pull-request body and their metadata.
Nothing is committed, published or allowed. Without a reachable API, download the snapshot and request and run
`teia review propose`. See [communication contract proposals](review-proposals.md).

## Validation

Automated checks cover each tool in light and dark themes and narrow widths, in teia's page and the installed
Grafana panel, including comparison of real stored captures and a sampled operation read from Tempo. The
coverage, results and remaining limits are recorded under T17–T22 in the [roadmap](roadmap.md).
