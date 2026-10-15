package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nilum-br/teia/internal/engine"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/review"
)

type historyInput struct {
	clusterInput
	From string `json:"from,omitempty" jsonschema:"RFC3339 start of the period; with to"`
	To   string `json:"to,omitempty" jsonschema:"RFC3339 end of the period; with from"`
}

type compareInput struct {
	clusterInput
	Before string `json:"before" jsonschema:"ID of the earlier stored snapshot, as teia_history lists it"`
	After  string `json:"after,omitempty" jsonschema:"ID of the later stored snapshot; the current round when empty"`
	Limit  int    `json:"limit,omitempty" jsonschema:"connections with the largest changes to return, 50 by default"`
}

type pathInput struct {
	sliceInput
	From     string `json:"from" jsonschema:"the component the calls start from"`
	To       string `json:"to" jsonschema:"the component they reach"`
	MaxPaths int    `json:"maxPaths,omitempty" jsonschema:"shortest paths to return, 3 by default, at most 10"`
	MaxDepth int    `json:"maxDepth,omitempty" jsonschema:"most connections in a path, 6 by default, at most 10"`
}

type impactInput struct {
	sliceInput
	ID        string `json:"id" jsonschema:"the component whose dependants or dependencies to follow"`
	Direction string `json:"direction,omitempty" jsonschema:"upstream: who calls it, directly or not (who is hit when it fails); downstream: what it calls. upstream by default"`
	Depth     int    `json:"depth,omitempty" jsonschema:"hops to follow, 3 by default, at most 8"`
}

type operationsInput struct {
	clusterInput
	Limit int `json:"limit,omitempty" jsonschema:"operation scopes to return, 20 by default"`
}

type proposeInput struct {
	clusterInput
	From      string `json:"from" jsonschema:"the caller of an observed but undeclared (ghost) connection"`
	To        string `json:"to" jsonschema:"its callee"`
	Rationale string `json:"rationale" jsonschema:"why this communication is intended, for the reviewers"`
}

// addInvestigationTools registers the tools that look across time and
// across the graph: clusters, history and comparison, paths and impact,
// sampled operations, and flows proposals. All of them only read.
func addInvestigationTools(s *mcp.Server, clusters []Cluster, pick func(string) (Cluster, error),
	snapshotFor func(cluster, slice string) (model.Snapshot, error)) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_clusters",
		Description: "The clusters this teia maps, each with its components, connections, health counts and how many of its sources worked. The first is read when a tool gets no cluster.",
		Annotations: readOnly("Clusters"),
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return nil, clusterList(clusters), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_history",
		Description: "Stored snapshots of a cluster (history.directory must be set), newest last, optionally within a period: their IDs feed teia_compare. Gaps between snapshots are unknown, not quiet.",
		Annotations: readOnly("History"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in historyInput) (*mcp.CallToolResult, any, error) {
		c, err := pick(in.Cluster)
		if err != nil {
			return nil, nil, err
		}
		var from, to time.Time
		for raw, target := range map[string]*time.Time{in.From: &from, in.To: &to} {
			if raw == "" {
				continue
			}
			if *target, err = time.Parse(time.RFC3339Nano, raw); err != nil {
				return nil, nil, fmt.Errorf("from and to are RFC3339 timestamps, such as 2026-10-15T12:00:00Z")
			}
		}
		if from.IsZero() != to.IsZero() || (!from.IsZero() && from.After(to)) {
			return nil, nil, errors.New("give both from and to, with from at or before to")
		}
		listing, err := c.History.List(from, to)
		if err != nil {
			return nil, nil, fmt.Errorf("history could not be read: %w", err)
		}
		return nil, map[string]any{"cluster": c.Name, "history": listing}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_compare",
		Description: "What changed in a cluster between a stored snapshot and a later one (or the current round): connections and components that appeared or disappeared, health changes, and the connections whose traffic, error share or p95 moved most. For questions such as what changed after a deploy.",
		Annotations: readOnly("Compare"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in compareInput) (*mcp.CallToolResult, any, error) {
		c, err := pick(in.Cluster)
		if err != nil {
			return nil, nil, err
		}
		if in.Before == "" {
			return nil, nil, errors.New("before is required: a snapshot ID from teia_history")
		}
		stored := func(id string) (model.Snapshot, error) {
			snap, err := c.History.Snapshot(id)
			if err != nil {
				return snap, fmt.Errorf("snapshot %q is not available (history disabled, expired or unknown): %w", id, err)
			}
			return snap, nil
		}
		before, err := stored(in.Before)
		if err != nil {
			return nil, nil, err
		}
		after := c.Engine.Snapshot()
		if in.After != "" {
			if after, err = stored(in.After); err != nil {
				return nil, nil, err
			}
		}
		limit := in.Limit
		if limit <= 0 || limit > 500 {
			limit = 50
		}
		changes := engine.Diff(before, after, after.GeneratedAt)
		if changes == nil {
			changes = []model.Change{}
		}
		return nil, map[string]any{
			"cluster": c.Name,
			"before":  map[string]any{"generatedAt": before.GeneratedAt, "round": before.Round, "components": len(before.Nodes), "connections": len(before.Edges)},
			"after":   map[string]any{"generatedAt": after.GeneratedAt, "round": after.Round, "components": len(after.Nodes), "connections": len(after.Edges)},
			"changes": changes,
			"moved":   movedEdges(before, after, limit),
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_path",
		Description: "The shortest call paths from one component to another, each connection with its health, rate, error share and p95: how a request travels, and where along the way it degrades.",
		Annotations: readOnly("Path"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in pathInput) (*mcp.CallToolResult, any, error) {
		snap, err := snapshotFor(in.Cluster, in.Slice)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range []string{in.From, in.To} {
			if !slices.ContainsFunc(snap.Nodes, func(n model.Node) bool { return n.ID == id }) {
				return nil, nil, fmt.Errorf("no component %q in this map; teia_map lists the IDs", id)
			}
		}
		paths := shortestPaths(snap.Edges, in.From, in.To, clamp(in.MaxPaths, 3, 10), clamp(in.MaxDepth, 6, 10))
		return nil, map[string]any{"cluster": snap.Cluster, "slice": snap.Slice, "from": in.From, "to": in.To, "paths": paths}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_impact",
		Description: "Who depends on a component (upstream: its callers, directly or not, which are hit when it fails) or what it depends on (downstream), hop by hop, with their health and the business dimensions (journeys, tenants) of the calls involved.",
		Annotations: readOnly("Impact"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in impactInput) (*mcp.CallToolResult, any, error) {
		snap, err := snapshotFor(in.Cluster, in.Slice)
		if err != nil {
			return nil, nil, err
		}
		direction := in.Direction
		if direction == "" {
			direction = "upstream"
		}
		if direction != "upstream" && direction != "downstream" {
			return nil, nil, errors.New("direction is upstream or downstream")
		}
		if !slices.ContainsFunc(snap.Nodes, func(n model.Node) bool { return n.ID == in.ID }) {
			return nil, nil, fmt.Errorf("no component %q in this map; teia_map lists the IDs", in.ID)
		}
		out := impact(snap, in.ID, direction == "upstream", clamp(in.Depth, 3, 8))
		out["cluster"], out["slice"], out["direction"] = snap.Cluster, snap.Slice, direction
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_operations",
		Description: "Sampled operations of a cluster: the request paths traces showed, with their sampling policy and what could not be attributed. Samples, not totals.",
		Annotations: readOnly("Operations"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in operationsInput) (*mcp.CallToolResult, any, error) {
		snap, err := snapshotFor(in.Cluster, "")
		if err != nil {
			return nil, nil, err
		}
		ops := snap.Operations
		limit := clamp(in.Limit, 20, 200)
		var truncated int
		if ops != nil && len(ops.Scopes) > limit {
			trimmed := *ops
			truncated = len(ops.Scopes) - limit
			trimmed.Scopes = ops.Scopes[:limit]
			ops = &trimmed
		}
		return nil, map[string]any{"cluster": snap.Cluster, "operations": ops, "scopesLeftOut": truncated}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "teia_propose_flow",
		Description: "Drafts the flows file fragment that would declare an observed but undeclared (ghost) connection, with the evidence behind it, for a pull request in the caller's repository. It writes nothing anywhere.",
		Annotations: readOnly("Propose a flow"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in proposeInput) (*mcp.CallToolResult, any, error) {
		snap, err := snapshotFor(in.Cluster, "")
		if err != nil {
			return nil, nil, err
		}
		proposal, err := review.Propose(snap, review.ProposalRequest{From: in.From, To: in.To, Rationale: in.Rationale, InputKind: "current-discovery"})
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"cluster": snap.Cluster, "proposal": proposal}, nil
	})
}

func clamp(v, def, most int) int {
	if v <= 0 {
		return def
	}
	return min(v, most)
}

// movedEdges are the connections present in both snapshots whose rate,
// error share or p95 changed most, by relative change.
func movedEdges(before, after model.Snapshot, limit int) []map[string]any {
	prev := map[string]model.Edge{}
	for _, e := range before.Edges {
		prev[e.Key()] = e
	}
	type moved struct {
		row   map[string]any
		score float64
	}
	var all []moved
	rel := func(a, b float64) float64 {
		if a == b {
			return 0
		}
		return math.Abs(b-a) / math.Max(math.Abs(a), 1e-9)
	}
	for _, e := range after.Edges {
		p, ok := prev[e.Key()]
		if !ok {
			continue
		}
		score := max(rel(p.RequestRate, e.RequestRate), rel(p.LatencyP95Ms, e.LatencyP95Ms), math.Abs(e.ErrorRate-p.ErrorRate)*10)
		if score == 0 && p.Health == e.Health {
			continue
		}
		all = append(all, moved{score: score, row: map[string]any{
			"from": e.From, "to": e.To,
			"requestRate":  map[string]float64{"before": p.RequestRate, "after": e.RequestRate},
			"errorRate":    map[string]any{"before": measured(p.ErrorRate, !p.ErrorsUnknown && p.RequestRate > 0), "after": measured(e.ErrorRate, !e.ErrorsUnknown && e.RequestRate > 0)},
			"latencyP95Ms": map[string]float64{"before": p.LatencyP95Ms, "after": e.LatencyP95Ms},
			"health":       map[string]model.Health{"before": p.Health, "after": e.Health},
		}})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	out := make([]map[string]any, 0, min(limit, len(all)))
	for _, m := range all[:min(limit, len(all))] {
		out = append(out, m.row)
	}
	return out
}

// shortestPaths finds up to most loop-free paths from one component to
// another, shortest first, by breadth-first search bounded in depth and in
// the work it may do.
func shortestPaths(edges []model.Edge, from, to string, most, depth int) [][]mapEdge {
	next := map[string][]model.Edge{}
	for _, e := range edges {
		next[e.From] = append(next[e.From], e)
	}
	type walk struct {
		at    string
		edges []model.Edge
	}
	queue := []walk{{at: from}}
	var out [][]mapEdge
	for steps := 0; len(queue) > 0 && len(out) < most && steps < 100_000; steps++ {
		w := queue[0]
		queue = queue[1:]
		if w.at == to && len(w.edges) > 0 {
			path := make([]mapEdge, len(w.edges))
			for i, e := range w.edges {
				path[i] = compactEdge(e)
			}
			out = append(out, path)
			continue
		}
		if len(w.edges) == depth {
			continue
		}
		for _, e := range next[w.at] {
			if e.To == from || slices.ContainsFunc(w.edges, func(x model.Edge) bool { return x.From == e.To }) {
				continue
			}
			queue = append(queue, walk{at: e.To, edges: append(slices.Clip(w.edges), e)})
		}
	}
	if out == nil {
		out = [][]mapEdge{}
	}
	return out
}

// impact follows callers (upstream) or callees (downstream) hop by hop.
func impact(snap model.Snapshot, id string, upstream bool, depth int) map[string]any {
	nodes := map[string]model.Node{}
	for _, n := range snap.Nodes {
		nodes[n.ID] = n
	}
	adjacent := map[string][]model.Edge{}
	for _, e := range snap.Edges {
		if upstream {
			adjacent[e.To] = append(adjacent[e.To], e)
		} else {
			adjacent[e.From] = append(adjacent[e.From], e)
		}
	}
	seen := map[string]bool{id: true}
	frontier := []string{id}
	var levels []map[string]any
	var traversed []mapEdge
	dims := map[string][]string{}
	for hop := 1; hop <= depth && len(frontier) > 0; hop++ {
		var reached []mapNode
		var nextFrontier []string
		for _, at := range frontier {
			for _, e := range adjacent[at] {
				traversed = append(traversed, compactEdge(e))
				for dim, values := range e.Dimensions {
					for _, v := range values {
						if !slices.Contains(dims[dim], v) {
							dims[dim] = append(dims[dim], v)
						}
					}
				}
				other := e.From
				if !upstream {
					other = e.To
				}
				if seen[other] {
					continue
				}
				seen[other] = true
				nextFrontier = append(nextFrontier, other)
				reached = append(reached, compactNode(nodes[other]))
			}
		}
		if len(reached) > 0 {
			levels = append(levels, map[string]any{"hop": hop, "components": reached})
		}
		frontier = nextFrontier
	}
	for dim := range dims {
		sort.Strings(dims[dim])
	}
	if levels == nil {
		levels = []map[string]any{}
	}
	if traversed == nil {
		traversed = []mapEdge{}
	}
	return map[string]any{"component": compactNode(nodes[id]), "levels": levels, "connections": traversed, "dimensions": dims}
}
