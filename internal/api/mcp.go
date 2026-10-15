package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nilum-br/teia/internal/journey"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/slice"
)

// mcpInstructions tells an agent what the tools answer and how to read them.
const mcpInstructions = `teia maps how services communicate from the telemetry already in place (traces, metrics,
logs, Kubernetes inventory) and compares it with the flows teams declared.

Start with teia_overview, then teia_map for the topology, and teia_component or teia_connection
for the evidence behind one service or one call. Every tool is read-only.

How to read the values:
- requestRate is requests per second; errorRate is the failed share (0..1); latencies are milliseconds.
- errorsUnknown=true means no source measured failures: it is not zero errors. In teia_map, errorRate
  is present (0 included) only when failures were measured. In the full objects of teia_component and
  teia_connection, a measured zero is omitted: there, read a missing errorRate with errorsUnknown false
  and some requestRate as 0.
- latencyApproximate=true means percentiles of several series were combined by traffic.
- state "ghost" is observed but not declared; "stale" is declared but not observed.
- slice (such as "journey:checkout,tenant:acme") keeps only the calls that carried those values,
  with their own metrics. teia_dimensions lists the values seen. An empty result with
  slice.matched=false means no call carried the selection, not that the whole map is empty.`

// readOnly marks a tool that reads the last round and changes nothing.
func readOnly(title string) *mcp.ToolAnnotations {
	closed := false
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}
}

type sliceInput struct {
	Slice string `json:"slice,omitempty" jsonschema:"only the calls carrying these business dimensions, such as journey:checkout or journey:checkout,tenant:acme"`
}

type mapInput struct {
	sliceInput
	Node string `json:"node,omitempty" jsonschema:"only this component and the connections from or to it"`
}

type componentInput struct {
	sliceInput
	ID string `json:"id" jsonschema:"the component's node ID, as teia_map lists it"`
}

type connectionInput struct {
	sliceInput
	From string `json:"from" jsonschema:"the caller's node ID"`
	To   string `json:"to" jsonschema:"the callee's node ID"`
}

type findingsInput struct {
	sliceInput
	Severity string `json:"severity,omitempty" jsonschema:"only this severity: critical, warning or info"`
}

type changesInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"the most recent changes to return, 50 by default, at most 500"`
}

type journeysInput struct {
	sliceInput
	Key string `json:"key,omitempty" jsonschema:"the label that names journeys, when not the configured one"`
}

// mapNode and mapEdge are the topology without evidence, links or rules:
// teia_component and teia_connection carry those for one subject.
type mapNode struct {
	ID            string            `json:"id"`
	Kind          model.NodeKind    `json:"kind"`
	Tech          string            `json:"tech,omitempty"`
	Health        model.Health      `json:"health"`
	Visibility    model.Visibility  `json:"visibility"`
	RequestRate   float64           `json:"requestRate,omitempty"`
	ErrorRate     *float64          `json:"errorRate,omitempty"`
	ErrorsUnknown bool              `json:"errorsUnknown,omitempty"`
	LatencyP95Ms  float64           `json:"latencyP95Ms,omitempty"`
	Approximate   bool              `json:"latencyApproximate,omitempty"`
	Journeys      []string          `json:"journeys,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Reasons       []string          `json:"reasons,omitempty"`
}

type mapEdge struct {
	From          string              `json:"from"`
	To            string              `json:"to"`
	Protocol      string              `json:"protocol,omitempty"`
	Port          int                 `json:"port,omitempty"`
	State         model.EdgeState     `json:"state"`
	Health        model.Health        `json:"health"`
	Signals       []string            `json:"signals,omitempty"`
	RequestRate   float64             `json:"requestRate,omitempty"`
	ErrorRate     *float64            `json:"errorRate,omitempty"`
	ErrorsUnknown bool                `json:"errorsUnknown,omitempty"`
	LatencyP95Ms  float64             `json:"latencyP95Ms,omitempty"`
	Approximate   bool                `json:"latencyApproximate,omitempty"`
	Dimensions    map[string][]string `json:"dimensions,omitempty"`
	Reasons       []string            `json:"reasons,omitempty"`
}

func reasonText(reasons []model.Reason) []string {
	var out []string
	for _, r := range reasons {
		out = append(out, r.Summary)
	}
	return out
}

// measured is the error share when failures were measured, including a
// measured zero, and nil when nothing was: an agent must not read a missing
// value as zero, nor zero as missing.
func measured(rate float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return &rate
}

func compactNode(n model.Node) mapNode {
	return mapNode{
		ID: n.ID, Kind: n.Kind, Tech: n.Tech, Health: n.Health, Visibility: n.Visibility,
		RequestRate: n.RequestRate, ErrorRate: measured(n.ErrorRate, !n.ErrorsUnknown && n.ErrorCoverage > 0), ErrorsUnknown: n.ErrorsUnknown,
		LatencyP95Ms: n.LatencyP95Ms, Approximate: n.LatencyApproximate,
		Journeys: n.Journeys, Labels: n.Labels, Reasons: reasonText(n.Reasons),
	}
}

func compactEdge(e model.Edge) mapEdge {
	return mapEdge{
		From: e.From, To: e.To, Protocol: e.Protocol, Port: e.Port, State: e.State, Health: e.Health,
		Signals: e.Signals, RequestRate: e.RequestRate, ErrorRate: measured(e.ErrorRate, !e.ErrorsUnknown && e.RequestRate > 0), ErrorsUnknown: e.ErrorsUnknown,
		LatencyP95Ms: e.LatencyP95Ms, Approximate: e.LatencyApproximate, Dimensions: e.Dimensions,
		Reasons: reasonText(e.Reasons),
	}
}

// count tallies values, such as edges per health.
func count[T any](items []T, key func(T) string) map[string]int {
	out := map[string]int{}
	for _, it := range items {
		out[key(it)]++
	}
	return out
}

// newMCPServer exposes the last round to agents over the Model Context
// Protocol: the same snapshots, slices and token as the HTTP API.
func newMCPServer(eng Snapshotter, version string) *mcp.Server {
	if version == "" {
		version = "dev"
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "teia", Title: "teia service communication map", Version: version},
		&mcp.ServerOptions{Instructions: mcpInstructions})

	// snapshotFor is the whole round, or only the calls a slice selects.
	snapshotFor := func(raw string) (model.Snapshot, error) {
		sel, err := slice.Parse(raw)
		if err != nil {
			return model.Snapshot{}, err
		}
		if sel == nil {
			return eng.Snapshot(), nil
		}
		slicer, ok := eng.(Slicer)
		if !ok {
			return model.Snapshot{}, errors.New("this server cannot slice snapshots by dimension")
		}
		return slicer.Sliced(sel), nil
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_overview",
		Description: "Summary of the last discovery round: when it ran, how many components and connections per health and state, coverage, the state of every source, the business dimensions seen and findings per severity.",
		Annotations: readOnly("Overview"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in sliceInput) (*mcp.CallToolResult, any, error) {
		snap, err := snapshotFor(in.Slice)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{
			"round":            snap.Round,
			"generatedAt":      snap.GeneratedAt.Format(time.RFC3339),
			"timeMode":         snap.TimeMode,
			"slice":            snap.Slice,
			"components":       len(snap.Nodes),
			"connections":      len(snap.Edges),
			"componentHealth":  count(snap.Nodes, func(n model.Node) string { return string(n.Health) }),
			"connectionHealth": count(snap.Edges, func(e model.Edge) string { return string(e.Health) }),
			"connectionState":  count(snap.Edges, func(e model.Edge) string { return string(e.State) }),
			"findings":         count(snap.Findings, func(f model.Finding) string { return string(f.Severity) }),
			"coverage":         snap.Coverage,
			"sources":          snap.Sources,
			"dimensions":       snap.Dimensions,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_map",
		Description: "The topology: every component and connection with health, state, signals, rate, error share and p95, without evidence. Optionally only one component's neighbourhood, or only the calls of a slice.",
		Annotations: readOnly("Map"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in mapInput) (*mcp.CallToolResult, any, error) {
		snap, err := snapshotFor(in.Slice)
		if err != nil {
			return nil, nil, err
		}
		keep := map[string]bool{}
		edges := []mapEdge{}
		for _, e := range snap.Edges {
			if in.Node != "" && e.From != in.Node && e.To != in.Node {
				continue
			}
			keep[e.From], keep[e.To] = true, true
			edges = append(edges, compactEdge(e))
		}
		nodes := []mapNode{}
		for _, n := range snap.Nodes {
			if in.Node == "" || n.ID == in.Node || keep[n.ID] {
				nodes = append(nodes, compactNode(n))
			}
		}
		if in.Node != "" && !slices.ContainsFunc(nodes, func(n mapNode) bool { return n.ID == in.Node }) {
			return nil, nil, fmt.Errorf("no component %q in this map", in.Node)
		}
		return nil, map[string]any{"generatedAt": snap.GeneratedAt.Format(time.RFC3339), "slice": snap.Slice, "nodes": nodes, "edges": edges}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_component",
		Description: "One component in full (health reasons, evidence per source, links to the telemetry, labels, measures) with the connections it makes and receives.",
		Annotations: readOnly("Component"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in componentInput) (*mcp.CallToolResult, any, error) {
		snap, err := snapshotFor(in.Slice)
		if err != nil {
			return nil, nil, err
		}
		i := slices.IndexFunc(snap.Nodes, func(n model.Node) bool { return n.ID == in.ID })
		if i < 0 {
			return nil, nil, fmt.Errorf("no component %q in this map; teia_map lists the IDs", in.ID)
		}
		outgoing, incoming := []mapEdge{}, []mapEdge{}
		for _, e := range snap.Edges {
			switch in.ID {
			case e.From:
				outgoing = append(outgoing, compactEdge(e))
			case e.To:
				incoming = append(incoming, compactEdge(e))
			}
		}
		var findings []model.Finding
		for _, f := range snap.Findings {
			if f.Subject == in.ID || strings.HasPrefix(f.Subject, in.ID+"->") || strings.HasSuffix(f.Subject, "->"+in.ID) {
				findings = append(findings, f)
			}
		}
		return nil, map[string]any{"slice": snap.Slice, "component": snap.Nodes[i], "outgoing": outgoing, "incoming": incoming, "findings": findings}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_connection",
		Description: "The connections from one component to another in full: declared and observed state, health reasons, evidence per source with its evaluation time, links to the telemetry, and the business dimensions seen on it.",
		Annotations: readOnly("Connection"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in connectionInput) (*mcp.CallToolResult, any, error) {
		snap, err := snapshotFor(in.Slice)
		if err != nil {
			return nil, nil, err
		}
		var found []model.Edge
		for _, e := range snap.Edges {
			if e.From == in.From && e.To == in.To {
				found = append(found, e)
			}
		}
		if len(found) == 0 {
			return nil, nil, fmt.Errorf("no connection from %q to %q in this map", in.From, in.To)
		}
		return nil, map[string]any{"slice": snap.Slice, "connections": found}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_findings",
		Description: "What teia found and the next step for each: undeclared or missing flows, failing or unmeasured connections, blind spots and how to see them.",
		Annotations: readOnly("Findings"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in findingsInput) (*mcp.CallToolResult, any, error) {
		switch model.Severity(in.Severity) {
		case "", model.SeverityCritical, model.SeverityWarning, model.SeverityInfo:
		default:
			return nil, nil, fmt.Errorf("severity must be critical, warning or info, not %q", in.Severity)
		}
		snap, err := snapshotFor(in.Slice)
		if err != nil {
			return nil, nil, err
		}
		findings := []model.Finding{}
		for _, f := range snap.Findings {
			if in.Severity == "" || string(f.Severity) == in.Severity {
				findings = append(findings, f)
			}
		}
		return nil, map[string]any{"slice": snap.Slice, "findings": findings}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_changes",
		Description: "Recent changes between rounds, newest first: connections that appeared or disappeared, health that changed, sources that failed or recovered.",
		Annotations: readOnly("Changes"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in changesInput) (*mcp.CallToolResult, any, error) {
		limit := in.Limit
		switch {
		case limit <= 0:
			limit = 50
		case limit > 500:
			limit = 500
		}
		changes := slices.Clone(eng.Changes())
		slices.Reverse(changes)
		if len(changes) > limit {
			changes = changes[:limit]
		}
		if changes == nil {
			changes = []model.Change{}
		}
		return nil, map[string]any{"changes": changes}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_journeys",
		Description: "Journeys by ownership label: which components belong to each, their frames and the ports to components outside them. For the calls a journey actually makes, use a slice instead.",
		Annotations: readOnly("Journeys"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in journeysInput) (*mcp.CallToolResult, any, error) {
		snap, err := snapshotFor(in.Slice)
		if err != nil {
			return nil, nil, err
		}
		contract := snap.Journeys
		if contract == nil || (in.Key != "" && in.Key != contract.Key) {
			contract = journey.Build(snap, in.Key)
		}
		return nil, map[string]any{"slice": snap.Slice, "journeys": contract}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_dimensions",
		Description: "The business dimensions (journey, tenant...) and the values the calls carried, for use as a slice.",
		Annotations: readOnly("Dimensions"),
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		dims := eng.Snapshot().Dimensions
		if dims == nil {
			dims = map[string][]string{}
		}
		return nil, map[string]any{"dimensions": dims}, nil
	})
	return s
}

// mcpHandler serves the MCP server over streamable HTTP. It is stateless and
// answers in JSON: every call reads the current round, so there is no
// session to keep.
func mcpHandler(eng Snapshotter, version string, log *slog.Logger) http.Handler {
	server := newMCPServer(eng, version)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: log})
}
