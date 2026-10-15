# Open supporting evidence

Configure `evidenceLinks` to attach investigation destinations to individual services, resources,
external endpoints and directed connections. The API exposes resolved `links` arrays on nodes and edges;
the Grafana table endpoints carry the same array as a JSON string in `links`.

Grafana destinations require the viewer's own Grafana permissions. A basic **Viewer** can query
data but cannot open Explore; use an authenticated Editor or the appropriate Explore RBAC role.
See [Grafana's role matrix](https://grafana.com/docs/grafana/latest/administration/roles-and-permissions/).
The loopback-only lab uses an anonymous Editor for this investigation demo. Production provisioning
must use authenticated access rather than copy that lab setting. Teia does not grant destination
permissions or forward source credentials; without Explore access Grafana may redirect to its home dashboard.

```yaml
evidenceLinks:
  - label: Search service traces
    target: node
    kind: traces
    source: tempo
    url: https://grafana.example.com/explore
    explore:
      type: tempo
      datasourceUID: tempo
      query: '{ resource.service.name = {{id|json}} }'
  - label: Search caller logs
    target: edge
    kind: logs
    source: loki/access-logs
    url: https://grafana.example.com/explore
    explore:
      type: loki
      datasourceUID: loki
      query: '{service={{from|json}}}'
  - label: Connection dashboard
    target: edge
    kind: dashboard
    source: prometheus
    url: https://grafana.example.com/d/connection
    params:
      var-client: '{{from}}'
      var-server: '{{to}}'
      var-namespace: '{{toNamespace}}'
      var-technology: '{{toTech}}'
      from: '{{period.fromMs}}'
      to: '{{period.toMs}}'
```

All referenced context is required: the last example is hidden when the destination namespace or technology
is unknown. Omit those parameters if the destination dashboard does not use them. Namespace-sensitive
environments should include the namespace in the query, for example
`{resource.service.name = {{id|json}} && resource.service.namespace = {{namespace|json}}}`.

`params` values are expanded and URL encoded. In Explore queries, `|json` emits a complete quoted string
literal, including escaped quotes, backslashes and control characters. Do not add another pair of quotes.
The complete query is then serialized inside Grafana's panes JSON and encoded as one URL parameter.
Only fixed absolute HTTP(S) destinations are allowed; credential parameters, URL userinfo, fragments and
executable schemes are rejected. The context whitelist cannot access source credentials or arbitrary labels.

The URL uses Grafana's documented `panes` JSON and `schemaVersion=1`; dashboard variables use `var-` and
absolute `from`/`to` milliseconds. Destination dashboards must actually define/use those variables.
See the official [Explore URL structure](https://grafana.com/docs/grafana/latest/visualizations/explore/get-started-with-explore/#generate-explore-urls-from-external-tools)
and [dashboard URL variables](https://grafana.com/docs/grafana/latest/visualizations/dashboards/build-dashboards/create-dashboard-url-variables/).

These links search by identity and evidence time. They do not select a known trace ID, prove a direct call,
or correlate a log line to a particular request. A service pair TraceQL search can match both services in
one trace without proving adjacency. For resources with no own spans, the lab searches client-side
`peer.service`/`server.address` attributes as well. Adjust selectors to your instrumentation and naming;
Teia aliases are not automatically reverse-mapped to backend service names. See official
[TraceQL examples](https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/)
and [LogQL log queries](https://grafana.com/docs/loki/latest/query/log_queries/).

The period is the envelope of qualifying evidence: original sampled-call bounds, or query evaluation minus
its configured lookback through evaluation. Arbitrary queries with literal ranges, offsets or `@` may cover a
different interval; this is a suggested search range, not proof of their effective coverage. It is not
dashboard-selected history or guaranteed continuous coverage. Millisecond URL bounds round outwards, with a
minimum one-millisecond interval; the API preserves the original timestamp precision.
Collection timestamps never stand in for unknown observation times. Source outage retention preserves the
old interval. Set `source` to the intended evidence source to avoid combining unrelated windows.

## Local lab

Both `lab/teia.yaml` and `lab/teia.host.yaml` include service and connection links for Tempo, Loki and the
provisioned **teia · evidence** dashboard (`teia-evidence`). Set `GRAFANA_PUBLIC_URL` to the address reachable
by the browser, normally `http://localhost:3480`; the Compose environment defaults to the published
`GRAFANA_PORT`. This is deliberately separate from Grafana's internal `http://grafana:3000` address.

The dashboard uses `service` and `target` variables for outgoing/failed calls, plus incoming calls to the
selected service. The caller-log link searches that service's log stream, not a guaranteed peer-specific
line. Links requiring Loki timing appear only where that source supplies a dated query window. A search
may be empty because of retention, sampling, backend labels or absent instrumentation.

For UI consumers, render `label` as the action, keep `description` visible, and show `periodFrom`/`periodTo`
when supplied. Open external destinations with `noopener`/`noreferrer` and validate HTTP(S) again when
accepting arbitrary external Grafana frames. A link can be reached by selecting its node/connection and
activating the named action. Do not invent links for missing context or call a service search an exact trace.
