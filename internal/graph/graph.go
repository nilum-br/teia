// Package graph merges what every source observed into one graph: identities,
// kinds, drift states, visibility and coverage.
package graph

import (
	"slices"
	"sort"
	"strings"
	"time"

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
	// UnknownCallers maps a node, then a signal, to the traffic it receives
	// from callers that signal could not identify.
	UnknownCallers map[string]map[string]UnknownCallers
	// Unmatched are readings no node took: what they measured, and the
	// names that were tried.
	Unmatched []Unmatched

	canonical func(string) string
}

// Canonical returns the node ID the graph uses for id: normalised, then
// aliased. Anything that names nodes outside the graph (findings, explorer
// targets) goes through it before comparing.
func (g *Graph) Canonical(id string) string {
	if g.canonical == nil {
		return NormalizeID(id)
	}
	return g.canonical(id)
}

// Unmatched is a reading whose candidates name no node in the graph.
type Unmatched struct {
	Source     string
	Origin     string
	Candidates []string
	To         []string
}

// UnknownCallers is traffic into a node from callers one signal cannot name.
type UnknownCallers struct {
	Rate    float64
	Sources []string
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
// label or annotation (teia.kind, teia.nilum.io/kind) beats a declaration,
// which beats a guess from traffic. Inventories report a kind only when it is
// set explicitly; otherwise they say "unknown", which never wins.
var kindPriority = map[string]int{
	"docker":     3,
	"kubernetes": 3,
	"declared":   2,
	"postgres":   2,
}

// priorityOf returns how much a source type is trusted about kinds; traffic
// sources default to the lowest rank.
func priorityOf(sourceType string) int {
	if p, ok := kindPriority[sourceType]; ok {
		return p
	}
	return 1
}

// NormalizeID is the form every node ID takes in the graph: trimmed and
// lowercased, so OpenTelemetry's "OrderService" and a flows file's
// "orderservice" are one node whatever source names it. Unresolved addresses
// ("ip:...") are kept as they are.
func NormalizeID(id string) string {
	id = strings.TrimSpace(id)
	if source.IsAddressID(id) {
		return id
	}
	return strings.ToLower(id)
}

// Build merges every source's observations into one graph.
func Build(results []Result, aliases map[string]string, ignore []string) *Graph {
	return BuildAt(results, aliases, ignore, time.Now())
}

// BuildAt is Build with an explicit clock, for tests.
func BuildAt(results []Result, aliases map[string]string, ignore []string, now time.Time) *Graph {
	ignored := map[string]bool{}
	for _, id := range ignore {
		ignored[NormalizeID(id)] = true
	}
	aliasOf := make(map[string]string, len(aliases))
	for from, to := range aliases {
		aliasOf[NormalizeID(from)] = NormalizeID(to)
	}
	canonical := func(id string) string {
		id = NormalizeID(id)
		if a, ok := aliasOf[id]; ok {
			return a
		}
		return id
	}

	// Inventory first: addresses and explicit ignores.
	type owner struct {
		id    string
		since time.Time
	}
	byAddress := map[string]owner{}
	for _, r := range results {
		for _, n := range r.Observation.Nodes {
			id := canonical(n.ID)
			if n.Ignore {
				ignored[id] = true
			}
			for _, addr := range n.Addresses {
				byAddress[addr] = owner{id: id, since: n.Since}
			}
		}
	}
	resolve := func(id string) string {
		id = canonical(id)
		if source.IsAddressID(id) {
			if o, ok := byAddress[strings.TrimPrefix(id, source.AddressPrefix)]; ok {
				return o.id
			}
		}
		return id
	}
	// ambiguous reports whether an address seen over window may have belonged
	// to someone else: its current owner got it after the window started.
	ambiguous := func(id string, window time.Duration) bool {
		if window <= 0 || !source.IsAddressID(canonical(id)) {
			return false
		}
		o, ok := byAddress[strings.TrimPrefix(canonical(id), source.AddressPrefix)]
		return ok && !o.since.IsZero() && o.since.After(now.Add(-window))
	}

	g := &Graph{Nodes: map[string]*model.Node{}, Edges: map[string]*model.Edge{}, Ignored: ignored, UnknownCallers: map[string]map[string]UnknownCallers{}, canonical: canonical}
	kindRank := map[string]int{}
	selfReported := map[string]bool{}

	addNode := func(id string, kind model.NodeKind, srcName, srcType string, labels map[string]string) *model.Node {
		n, ok := g.Nodes[id]
		if !ok {
			n = &model.Node{ID: id, Kind: model.KindUnknown}
			g.Nodes[id] = n
		}
		if kind != "" && kind != model.KindUnknown && priorityOf(srcType) > kindRank[id] {
			n.Kind = kind
			kindRank[id] = priorityOf(srcType)
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

	// latencyRate and errorsRate are the request rates of the observations
	// each edge's latency and error rate came from.
	latencyRate := map[string]float64{}
	errorsRate := map[string]float64{}
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
			node.Measures = mergeMeasures(node.Measures, n.Measures, resolve)
			if n.SelfReported {
				selfReported[id] = true
			}
		}
		for _, uc := range r.Observation.UnknownCallers {
			id := resolve(uc.Callee)
			if ignored[id] {
				continue
			}
			addNode(id, "", srcName, srcType, nil)
			if g.UnknownCallers[id] == nil {
				g.UnknownCallers[id] = map[string]UnknownCallers{}
			}
			u := g.UnknownCallers[id][uc.Signal]
			if uc.Rate > u.Rate {
				u.Rate = uc.Rate
			}
			u.Sources = appendUnique(u.Sources, srcName)
			g.UnknownCallers[id][uc.Signal] = u
		}
		for _, o := range r.Observation.Edges {
			if ambiguous(o.From, o.Window) || ambiguous(o.To, o.Window) {
				continue // attribute it once the window no longer straddles the change
			}
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
				e.Measures = mergeMeasures(e.Measures, o.Measures, resolve)
				if o.Signal != "" {
					e.Signals = appendUnique(e.Signals, o.Signal)
				}
				key := e.Key()
				if o.RequestRate > e.RequestRate {
					e.RequestRate = o.RequestRate
				}
				// Failures come from the busiest observation that measured
				// them: a query that counts requests only says nothing.
				if o.RequestRate > 0 && !o.NoErrors && o.RequestRate > errorsRate[key] {
					e.ErrorRate = o.ErrorRate
					errorsRate[key] = o.RequestRate
				}
				// Latency comes from the busiest observation that measured
				// it, so histograms (with a rate) win over sampled spans.
				if o.LatencyP95Ms > 0 && (e.LatencyP95Ms == 0 || o.RequestRate > latencyRate[key]) {
					e.LatencyP50Ms, e.LatencyP95Ms = o.LatencyP50Ms, o.LatencyP95Ms
					latencyRate[key] = o.RequestRate
				}
			}
		}
	}

	for key, e := range g.Edges {
		e.ErrorsUnknown = e.RequestRate > 0 && errorsRate[key] == 0
	}
	markOutside(g, results, canonical, selfReported)
	attachReadings(g, results, resolve)

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

// markOutside: when an inventory lists what runs on the platform, a node of
// unknown kind that no inventory lists and that only its callers report is
// outside the platform (a SaaS, a partner API): external.
//
// Only a complete list supports that conclusion. An inventory that failed or
// saw less than it could (missing RBAC, no socket) may have missed workloads
// that are very much inside, and one that lists nothing proves nothing, so
// in either case kinds are left unknown rather than called external.
func markOutside(g *Graph, results []Result, canonical func(string) string, selfReported map[string]bool) {
	inventoried := map[string]bool{}
	listed := false
	for _, r := range results {
		if t := r.Source.Type(); t != "docker" && t != "kubernetes" {
			continue
		}
		if r.Err != nil || r.Observation.Degraded {
			return
		}
		for _, n := range r.Observation.Nodes {
			inventoried[canonical(n.ID)] = true
			listed = true
		}
	}
	if !listed {
		return
	}
	for id, n := range g.Nodes {
		if n.Kind == model.KindUnknown && !inventoried[id] && !selfReported[id] && !source.IsAddressID(id) {
			n.Kind = model.KindExternal
		}
	}
}

// mergeMeasures adds measures to have, one per cause and what: when two
// sources read the same thing, the higher value wins. Via is resolved like
// any node ID (an address becomes the node that held it).
func mergeMeasures(have, add []model.Measure, resolve func(string) string) []model.Measure {
	for _, m := range add {
		if m.Via != "" {
			m.Via = resolve(m.Via)
		}
		i := slices.IndexFunc(have, func(h model.Measure) bool { return h.Cause == m.Cause && h.What == m.What })
		switch {
		case i < 0:
			have = append(have, m)
		case m.Value > have[i].Value:
			have[i] = m
		}
	}
	return have
}

// attachReadings gives each reading's measures to the first candidate the
// graph knows (or to its edge towards the first known To), and records the
// readings nobody took, so the advisor can ask for an alias.
func attachReadings(g *Graph, results []Result, resolve func(string) string) {
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		for _, rd := range r.Observation.Readings {
			if !attach(g, rd, resolve) {
				g.Unmatched = append(g.Unmatched, Unmatched{Source: r.Source.Name(), Origin: rd.Origin, Candidates: rd.Candidates, To: rd.To})
			}
		}
	}
}

// attach gives one reading's measures to its node or edge, and says whether
// one took them.
func attach(g *Graph, rd source.Reading, resolve func(string) string) bool {
	from := ""
	for _, c := range rd.Candidates {
		if id := resolve(c); g.Nodes[id] != nil && !g.Ignored[id] {
			from = id
			break
		}
	}
	if from == "" {
		return false
	}
	if len(rd.To) == 0 {
		n := g.Nodes[from]
		n.Measures = mergeMeasures(n.Measures, rd.Measures, resolve)
		return true
	}
	for _, t := range rd.To {
		if e := g.Edges[from+"->"+resolve(t)]; e != nil {
			e.Measures = mergeMeasures(e.Measures, rd.Measures, resolve)
			return true
		}
	}
	return false
}
