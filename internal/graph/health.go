package graph

import (
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// HealthThresholds are error-rate limits (0..1).
type HealthThresholds struct {
	Degraded float64
	Failing  float64
}

// DefaultHealth: 1% errors is degraded, 5% is failing.
var DefaultHealth = HealthThresholds{Degraded: 0.01, Failing: 0.05}

// Classify turns a request and error rate into a health state.
func (t HealthThresholds) Classify(requestRate, errorRate float64) model.Health {
	switch {
	case requestRate <= 0:
		return model.HealthUnknown
	case errorRate >= t.Failing:
		return model.HealthFailing
	case errorRate >= t.Degraded:
		return model.HealthDegraded
	}
	return model.HealthHealthy
}

// ApplyHealth sets the health of every edge from its own rates, and of every
// node from the traffic it receives; a node nobody calls is judged by the
// calls it makes.
func ApplyHealth(g *Graph, t HealthThresholds) {
	type sum struct{ rate, failed float64 }
	in, out := map[string]*sum{}, map[string]*sum{}
	add := func(m map[string]*sum, id string, e *model.Edge) {
		s, ok := m[id]
		if !ok {
			s = &sum{}
			m[id] = s
		}
		s.rate += e.RequestRate
		s.failed += e.RequestRate * e.ErrorRate
	}
	for _, e := range g.Edges {
		e.Health = t.Classify(e.RequestRate, e.ErrorRate)
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
			n.Health = model.HealthUnknown
			continue
		}
		n.RequestRate = s.rate
		n.ErrorRate = s.failed / s.rate
		n.Health = t.Classify(n.RequestRate, n.ErrorRate)
	}
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
