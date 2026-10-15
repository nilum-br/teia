# Agents (MCP)

teia serves the [Model Context Protocol](https://modelcontextprotocol.io) at `/mcp`, on the same port as the UI and
the API. An agent (Claude Code, Claude Desktop, an IDE assistant, your own) can then ask what calls what, what is
failing and why, and what teia cannot see, with the same data the map shows.

- **Read-only.** Every tool reads the last discovery round. Nothing changes the configuration, the sources or the
  cluster, and the tools are annotated `readOnlyHint`.
- **Same access as the API.** With `auth.token` set, MCP clients send `Authorization: Bearer <token>`. Without a token,
  `/mcp` is as open as `/api/v1/*`. Browsers on other sites cannot read it: `cors.allowedOrigins` covers `/api/v1/*` only.
- **Stateless streamable HTTP**, answered in JSON. There is no session to keep and nothing to configure.

## Tools

| Tool | Answers |
|---|---|
| `teia_overview` | When the round ran; components and connections per health and state; coverage; every source's state; findings per severity; dimensions seen. Start here. |
| `teia_map` | Every component and connection with health, state, signals, rate, error share and p95, without evidence. `node` keeps one component's neighbourhood. |
| `teia_component` | One component in full (reasons, evidence per source, links, labels, measures), its incoming and outgoing connections, and the findings about it. |
| `teia_connection` | The connections from one component to another in full: declared/observed state, reasons, evidence with its evaluation time, links, dimensions. |
| `teia_findings` | Findings with their next step; `severity` filters `critical`, `warning` or `info`. |
| `teia_changes` | Changes between rounds, newest first (`limit`, 50 by default). |
| `teia_journeys` | Ownership journeys: members, frames and boundary ports (`key` picks another journey label). |
| `teia_dimensions` | Business dimensions and the values the calls carried. |

With [several clusters](configuration.md#clusters), every tool takes `cluster` (the first when empty) and every answer
carries the `cluster` it describes; `teia_overview` lists them. A tool never mixes clusters: compare clusters by calling
it once per cluster.

Every tool that reads the map takes `slice`, as `?slice=` does on the API: `journey:checkout` or
`journey:checkout,tenant:acme` keeps only the calls that carried those values, with their own metrics (see
[journeys](journeys.md#slices-follow-the-calls)). The result carries `slice.matched`; an empty map with
`matched: false` means no call carried the selection.

In `teia_map`, `errorRate` is present, `0` included, only when failures were measured; otherwise it is absent and
`errorsUnknown` says why. The server's instructions remind the agent how to read the values: `errorsUnknown` is not zero errors,
`latencyApproximate` marks combined percentiles, `ghost` is observed but not declared, `stale` declared but not seen.

## Connect a client

Claude Code:

```sh
claude mcp add --transport http teia http://localhost:8480/mcp
# with a token
claude mcp add --transport http teia http://teia.teia.svc:8080/mcp --header "Authorization: Bearer $TEIA_TOKEN"
```

Any client that speaks streamable HTTP:

```json
{
  "mcpServers": {
    "teia": {
      "type": "http",
      "url": "http://localhost:8480/mcp",
      "headers": { "Authorization": "Bearer ${TEIA_TOKEN}" }
    }
  }
}
```

In Kubernetes, reach it through the chart's Service or Ingress (`/mcp` is served with the rest), or
`kubectl -n teia port-forward svc/teia 8480:8080` for a local agent.

Check it by hand:

```sh
curl -s http://localhost:8480/mcp -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"teia_overview","arguments":{}}}'
```
