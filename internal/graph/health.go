package graph

import (
	"sort"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// HealthThresholds are the limits of each golden signal: Degraded and
// Failing are error rates (0..1), the latency limits apply to p95.
type HealthThresholds struct {
	Degraded, Failing                   float64
	LatencyDegradedMs, LatencyFailingMs float64
}

// DefaultHealth: 1% errors or a p95 of 500 ms is degraded; 5% errors or a
// p95 of 2 s is failing.
var DefaultHealth = HealthThresholds{Degraded: 0.01, Failing: 0.05, LatencyDegradedMs: 500, LatencyFailingMs: 2000}

// Judge returns the health of traffic with these signals, and a reason for
// every signal past its limit, worst first. Without a rate or a latency
// there is nothing to judge.
func (t HealthThresholds) Judge(requestRate, errorRate, latencyP95Ms float64) (model.Health, []model.Reason) {
	if requestRate <= 0 && latencyP95Ms <= 0 {
		return model.HealthUnknown, nil
	}
	var reasons []model.Reason
	if requestRate > 0 {
		if r, ok := limit(model.CauseErrors, errorRate, t.Degraded, t.Failing); ok {
			r.Summary = model.FormatPercent(errorRate) + " errors, over " + model.FormatPercent(r.Limit)
			reasons = append(reasons, r)
		}
	}
	if r, ok := limit(model.CauseLatency, latencyP95Ms, t.LatencyDegradedMs, t.LatencyFailingMs); ok {
		r.Summary = "p95 " + model.FormatMs(latencyP95Ms) + ", over " + model.FormatMs(r.Limit)
		reasons = append(reasons, r)
	}
	sortReasons(reasons)
	return model.Worst(reasons, model.HealthHealthy), reasons
}

// limit compares value with two limits; ok is false within both. A limit
// of zero or less is off.
func limit(cause model.Cause, value, degraded, failing float64) (model.Reason, bool) {
	switch {
	case failing > 0 && value >= failing:
		return model.Reason{Cause: cause, Health: model.HealthFailing, Value: value, Limit: failing}, true
	case degraded > 0 && value >= degraded:
		return model.Reason{Cause: cause, Health: model.HealthDegraded, Value: value, Limit: degraded}, true
	}
	return model.Reason{}, false
}

// sortReasons puts the worst first, then by cause for a stable order.
func sortReasons(reasons []model.Reason) {
	sort.SliceStable(reasons, func(i, j int) bool {
		if a, b := reasons[i].Health.Rank(), reasons[j].Health.Rank(); a != b {
			return a > b
		}
		return reasons[i].Cause < reasons[j].Cause
	})
}

// ApplyHealth sets the health of every edge from its own rates, and of every
// node from the traffic it receives; a node nobody calls is judged by the
// calls it makes.
func ApplyHealth(g *Graph, t HealthThresholds) {
	// timed is the traffic of the calls that measured latency, and their
	// latencies weighted by it.
	type sum struct{ rate, failed, timed, p50, p95 float64 }
	in, out := map[string]*sum{}, map[string]*sum{}
	add := func(m map[string]*sum, id string, e *model.Edge) {
		s, ok := m[id]
		if !ok {
			s = &sum{}
			m[id] = s
		}
		s.rate += e.RequestRate
		s.failed += e.RequestRate * e.ErrorRate
		if e.LatencyP95Ms > 0 {
			s.timed += e.RequestRate
			s.p50 += e.RequestRate * e.LatencyP50Ms
			s.p95 += e.RequestRate * e.LatencyP95Ms
		}
	}
	for _, e := range g.Edges {
		e.Health, e.Reasons = t.Judge(e.RequestRate, e.ErrorRate, e.LatencyP95Ms)
		if e.RequestRate > 0 {
			add(in, e.To, e)
			add(out, e.From, e)
		}
	}
	for id, n := range g.Nodes {
		s := in[id]
		if s == nil {
			s = out[id]
		}
		if s == nil || s.rate == 0 {
			n.Health, n.Reasons = model.HealthUnknown, nil
			continue
		}
		n.RequestRate = s.rate
		n.ErrorRate = s.failed / s.rate
		// A node's latency is how long it takes to answer, so only the calls
		// it serves count, never the ones it makes.
		if s := in[id]; s != nil && s.timed > 0 {
			n.LatencyP50Ms, n.LatencyP95Ms = s.p50/s.timed, s.p95/s.timed
		}
		n.Health, n.Reasons = t.Judge(n.RequestRate, n.ErrorRate, n.LatencyP95Ms)
	}
	explain(g)
}

// explain points latency and error reasons at the dependency they most
// likely come from. A caller waiting on a slow callee is slow too, and so is
// its own caller: naming the callee lets the eye go to the origin instead of
// three amber cards. The rule is deliberately plain: the outgoing call with
// the worst value, when it accounts for most of the node's (70% of its p95,
// half of its error rate).
func explain(g *Graph) {
	out := map[string][]*model.Edge{}
	for _, e := range g.Edges {
		out[e.From] = append(out[e.From], e)
	}
	for _, n := range g.Nodes {
		for i := range n.Reasons {
			r := &n.Reasons[i]
			var worst *model.Edge
			for _, e := range out[n.ID] {
				if v, w := causeValue(e, r.Cause), causeValue(worst, r.Cause); v > w || (v == w && worst != nil && e.To < worst.To) {
					worst = e
				}
			}
			if worst == nil {
				continue
			}
			share := 0.7
			if r.Cause == model.CauseErrors {
				share = 0.5
			}
			if causeValue(worst, r.Cause) >= share*r.Value {
				r.Via = worst.To
				r.Summary += ", from " + worst.To
			}
		}
	}
	// An edge into a node that waits on something else waits on it too.
	for _, e := range g.Edges {
		callee := g.Nodes[e.To]
		if callee == nil {
			continue
		}
		for i := range e.Reasons {
			r := &e.Reasons[i]
			for _, cr := range callee.Reasons {
				if cr.Cause == r.Cause && cr.Via != "" && cr.Via != e.From {
					r.Via = cr.Via
					r.Summary += ", from " + cr.Via
					break
				}
			}
		}
	}
}

// causeValue is the edge's value for a cause; zero for a nil edge or a
// cause edges do not measure.
func causeValue(e *model.Edge, c model.Cause) float64 {
	if e == nil {
		return 0
	}
	switch c {
	case model.CauseLatency:
		return e.LatencyP95Ms
	case model.CauseErrors:
		if e.RequestRate > 0 {
			return e.ErrorRate
		}
	}
	return 0
}

// Memory remembers observed edges, so a signal that samples (Tempo) or a
// rate window that just emptied does not make edges blink in and out.
type Memory struct {
	Retention time.Duration
	edges     map[string]rememberedEdge
}

type rememberedEdge struct {
	edge model.Edge
	seen time.Time
}

// Retain records this round's observed edges and puts back those seen within
// the retention period that this round missed. It runs before drift states
// and health are computed.
func (m *Memory) Retain(g *Graph, now time.Time) {
	if m.Retention <= 0 {
		return
	}
	if m.edges == nil {
		m.edges = map[string]rememberedEdge{}
	}
	for key, e := range g.Edges {
		if e.Observed {
			m.edges[key] = rememberedEdge{edge: *e, seen: now}
		}
	}
	for key, r := range m.edges {
		if now.Sub(r.seen) > m.Retention {
			delete(m.edges, key)
			continue
		}
		if g.Ignored[r.edge.From] || g.Ignored[r.edge.To] {
			continue
		}
		e, ok := g.Edges[key]
		if ok && e.Observed {
			continue
		}
		if !ok {
			kept := r.edge
			kept.Declared = false
			e = &kept
			g.Edges[key] = e
		} else {
			e.Observed = true
			e.RequestRate, e.ErrorRate = r.edge.RequestRate, r.edge.ErrorRate
			e.LatencyP50Ms, e.LatencyP95Ms = r.edge.LatencyP50Ms, r.edge.LatencyP95Ms
			for _, s := range r.edge.Sources {
				e.Sources = appendUnique(e.Sources, s)
			}
			for _, s := range r.edge.Signals {
				e.Signals = appendUnique(e.Signals, s)
			}
		}
		for _, id := range []string{e.From, e.To} {
			if _, ok := g.Nodes[id]; !ok {
				g.Nodes[id] = &model.Node{ID: id, Kind: model.KindUnknown, Sources: e.Sources}
			}
		}
	}
	RecomputeStates(g)
}

// RecomputeStates derives drift states and visibility after edges changed.
func RecomputeStates(g *Graph) {
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
		if n.Visibility == model.VisibilityFull {
			continue
		}
		if seen[id] {
			n.Visibility = model.VisibilityPartial
		} else {
			n.Visibility = model.VisibilityNone
		}
	}
}
