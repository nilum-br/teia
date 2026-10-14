// Package graph merges what every source observed into one graph: identities,
// kinds, drift states, visibility and coverage.
package graph

import (
	"sort"
	"strings"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

// Result is what one source returned in a round.
type Result struct {
	Source      source.Source
	Observation source.Observation
	Err         error
}

// Graph is the merged view the advisor works on.
type Graph struct {
	Nodes map[string]*model.Node
	Edges map[string]*model.Edge
	// Ignored holds IDs dropped by config or by a teia.ignore label.
	Ignored map[string]bool
}

// SortedNodes returns nodes ordered by ID.
func (g *Graph) SortedNodes() []model.Node {
	out := make([]model.Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SortedEdges returns edges ordered by caller, then callee.
func (g *Graph) SortedEdges() []model.Edge {
	out := make([]model.Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// Callers returns the IDs of nodes with an observed edge into id.
func (g *Graph) Callers(id string) []string {
	var out []string
	for _, e := range g.Edges {
		if e.To == id && e.Observed {
			out = append(out, e.From)
		}
	}
	sort.Strings(out)
	return out
}

// kindPriority decides whose opinion about a node's kind wins: an operator
// label on a container beats a declaration, which beats a guess from traffic.
var kindPriority = map[string]int{
	"docker":       3,
	"declared":     2,
	"postgres":     2,
	"servicegraph": 1,
}

// Build merges every source's observations into one graph.
func Build(results []Result, aliases map[string]string, ignore []string) *Graph {
	ignored := map[string]bool{}
	for _, id := range ignore {
		ignored[id] = true
	}
	canonical := func(id string) string {
		id = strings.TrimSpace(id)
		if a, ok := aliases[id]; ok {
			return a
		}
		return id
	}

	// Inventory first: addresses and explicit ignores.
	byAddress := map[string]string{}
	for _, r := range results {
		for _, n := range r.Observation.Nodes {
			id := canonical(n.ID)
			if n.Ignore {
				ignored[id] = true
			}
			for _, addr := range n.Addresses {
				byAddress[addr] = id
			}
		}
	}
	resolve := func(id string) string {
		id = canonical(id)
		if source.IsAddressID(id) {
			if name, ok := byAddress[strings.TrimPrefix(id, source.AddressPrefix)]; ok {
				return name
			}
		}
		return id
	}

	g := &Graph{Nodes: map[string]*model.Node{}, Edges: map[string]*model.Edge{}, Ignored: ignored}
	kindRank := map[string]int{}
	selfReported := map[string]bool{}

	addNode := func(id string, kind model.NodeKind, srcName, srcType string, labels map[string]string) *model.Node {
		n, ok := g.Nodes[id]
		if !ok {
			n = &model.Node{ID: id, Kind: model.KindUnknown}
			g.Nodes[id] = n
		}
		if kind != "" && kind != model.KindUnknown && kindPriority[srcType] > kindRank[id] {
			n.Kind = kind
			kindRank[id] = kindPriority[srcType]
		}
		n.Sources = appendUnique(n.Sources, srcName)
		for k, v := range labels {
			if n.Labels == nil {
				n.Labels = map[string]string{}
			}
			if _, exists := n.Labels[k]; !exists {
				n.Labels[k] = v
			}
		}
		return n
	}

	for _, r := range results {
		if r.Err != nil {
			continue
		}
		srcName, srcType := r.Source.Name(), r.Source.Type()
		for _, n := range r.Observation.Nodes {
			id := resolve(n.ID)
			if ignored[id] {
				continue
			}
			node := addNode(id, n.Kind, srcName, srcType, n.Labels)
			for _, addr := range n.Addresses {
				node.Addresses = appendUnique(node.Addresses, addr)
			}
			if n.SelfReported {
				selfReported[id] = true
			}
		}
		for _, o := range r.Observation.Edges {
			from, to := resolve(o.From), resolve(o.To)
			if from == to || ignored[from] || ignored[to] {
				continue
			}
			addNode(from, "", srcName, srcType, nil)
			addNode(to, "", srcName, srcType, nil)
			e := mergeEdge(g, from, to)
			e.Sources = appendUnique(e.Sources, srcName)
			if e.Protocol == "" {
				e.Protocol = o.Protocol
			}
			if e.Port == 0 {
				e.Port = o.Port
			}
			if o.Declared {
				e.Declared = true
				if e.Reason == "" {
					e.Reason = o.Reason
				}
			} else {
				e.Observed = true
				if o.RequestRate > e.RequestRate {
					e.RequestRate = o.RequestRate
					e.ErrorRate = o.ErrorRate
				}
			}
		}
	}

	seen := map[string]bool{}
	for _, e := range g.Edges {
		switch {
		case e.Declared && e.Observed:
			e.State = model.EdgeOK
		case e.Observed:
			e.State = model.EdgeGhost
		default:
			e.State = model.EdgeStale
		}
		if e.Observed {
			seen[e.From], seen[e.To] = true, true
		}
	}
	for id, n := range g.Nodes {
		switch {
		case selfReported[id]:
			n.Visibility = model.VisibilityFull
		case seen[id]:
			n.Visibility = model.VisibilityPartial
		default:
			n.Visibility = model.VisibilityNone
		}
	}
	return g
}

func mergeEdge(g *Graph, from, to string) *model.Edge {
	key := from + "->" + to
	e, ok := g.Edges[key]
	if !ok {
		e = &model.Edge{From: from, To: to}
		g.Edges[key] = e
	}
	return e
}

// ComputeCoverage scores how complete the picture is.
func ComputeCoverage(g *Graph) model.Coverage {
	c := model.Coverage{Nodes: len(g.Nodes), Edges: len(g.Edges)}
	var internal, full int
	for _, n := range g.Nodes {
		if n.Kind == model.KindExternal {
			continue
		}
		internal++
		if n.Visibility == model.VisibilityFull {
			full++
		}
	}
	var observed, observedDeclared, declared, declaredObserved int
	for _, e := range g.Edges {
		if e.Observed {
			observed++
			if e.Declared {
				observedDeclared++
			}
		}
		if e.Declared {
			declared++
			if e.Observed {
				declaredObserved++
			}
		}
	}
	c.Visibility = ratio(full, internal)
	c.Declared = ratio(observedDeclared, observed)
	c.Observed = ratio(declaredObserved, declared)
	return c
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
