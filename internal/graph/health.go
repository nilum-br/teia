package graph

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// HealthThresholds are the limits of each golden signal, as degraded and
// failing pairs: error rates (0..1), p95 latency, the used share of a
// capacity (0..1), sessions waiting on locks, and items waiting in a queue.
type HealthThresholds struct {
	Degraded, Failing                     float64
	LatencyDegradedMs, LatencyFailingMs   float64
	SaturationDegraded, SaturationFailing float64
	LocksDegraded, LocksFailing           float64
	BacklogDegraded, BacklogFailing       float64
	LagDegradedSeconds, LagFailingSeconds float64
	RestartsDegraded, RestartsFailing     float64
	EvictionsDegraded, EvictionsFailing   float64
	RejectionsDegraded, RejectionsFailing float64
}

// DefaultHealth is amber from 1% errors, a p95 of 500 ms, 80% of a capacity,
// one session waiting on a lock, a thousand items waiting, a queue a minute
// behind or one restart; red from 5%, 2 s, 95%, five sessions, ten thousand
// items, five minutes or three restarts.
var DefaultHealth = HealthThresholds{
	Degraded: 0.01, Failing: 0.05,
	LatencyDegradedMs: 500, LatencyFailingMs: 2000,
	SaturationDegraded: 0.8, SaturationFailing: 0.95,
	LocksDegraded: 1, LocksFailing: 5,
	BacklogDegraded: 1000, BacklogFailing: 10000,
	LagDegradedSeconds: 60, LagFailingSeconds: 300,
	RestartsDegraded: 1, RestartsFailing: 3,
	EvictionsDegraded: 1, EvictionsFailing: 100,
	RejectionsDegraded: 1, RejectionsFailing: 10,
}

// Signals are what a node or edge is judged on.
type Signals struct {
	RequestRate, ErrorRate, LatencyP95Ms float64
	// ErrorsUnknown: failures were not measured, so ErrorRate is not judged.
	ErrorsUnknown      bool
	LatencyApproximate bool
	Measures           []model.Measure
}

// Judge returns the health of these signals, and a reason for every one past
// its limit, worst first. Healthy needs something measured within its
// limits: traffic whose failures nobody measured is unknown, not healthy.
func (t HealthThresholds) Judge(s Signals) (model.Health, []model.Reason) {
	errorsJudged := s.RequestRate > 0 && !s.ErrorsUnknown
	reasons, measuresJudged := t.judgeMeasures(s.Measures)
	if !errorsJudged && s.LatencyP95Ms <= 0 && !measuresJudged {
		return model.HealthUnknown, nil
	}
	if errorsJudged {
		if r, ok := limit(model.CauseErrors, s.ErrorRate, t.Degraded, t.Failing); ok {
			r.Summary = model.FormatPercent(s.ErrorRate) + " errors, over " + model.FormatPercent(r.Limit)
			reasons = append(reasons, r)
		}
	}
	if r, ok := limit(model.CauseLatency, s.LatencyP95Ms, t.LatencyDegradedMs, t.LatencyFailingMs); ok {
		r.Summary = "p95 " + model.FormatMs(s.LatencyP95Ms) + ", over " + model.FormatMs(r.Limit)
		if s.LatencyApproximate {
			r.Approximate = true
			r.Summary = "p95 approximation " + model.FormatMs(s.LatencyP95Ms) + ", over " + model.FormatMs(r.Limit)
		}
		reasons = append(reasons, r)
	}
	sortReasons(reasons)
	return model.Worst(reasons, model.HealthHealthy), reasons
}

// judgeMeasures turns measures past their limits into reasons.
func (t HealthThresholds) judgeMeasures(measures []model.Measure) ([]model.Reason, bool) {
	var reasons []model.Reason
	judged := false
	for _, m := range measures {
		var r model.Reason
		var ok bool
		switch m.Cause {
		case model.CauseSaturation:
			if m.Max <= 0 {
				continue
			}
			share := m.Value / m.Max
			if r, ok = limit(m.Cause, share, t.SaturationDegraded, t.SaturationFailing); ok {
				if m.Unit == model.UnitShare {
					r.Summary = fmt.Sprintf("%s %s, over %s", model.FormatPercent(share), m.What, model.FormatPercent(r.Limit))
				} else {
					r.Summary = fmt.Sprintf("%s of %s used (%s of %s), over %s", model.FormatPercent(share), m.What,
						model.FormatValue(m.Value, m.Unit), model.FormatValue(m.Max, m.Unit), model.FormatPercent(r.Limit))
				}
			}
		case model.CauseLocks:
			if r, ok = limit(m.Cause, m.Value, t.LocksDegraded, t.LocksFailing); ok {
				r.Summary = fmt.Sprintf("%s %s, over %s", model.FormatCount(m.Value), m.What, model.FormatCount(r.Limit))
				if m.Via != "" {
					r.Summary += ", held by " + m.Via
				}
			}
		case model.CauseBacklog:
			if r, ok = limit(m.Cause, m.Value, t.BacklogDegraded, t.BacklogFailing); ok {
				r.Summary = fmt.Sprintf("%s %s, over %s", model.FormatCount(m.Value), m.What, model.FormatCount(r.Limit))
			}
		case model.CauseLag:
			if r, ok = limit(m.Cause, m.Value, t.LagDegradedSeconds, t.LagFailingSeconds); ok {
				r.Summary = fmt.Sprintf("%s %s, over %s", model.FormatSeconds(m.Value), m.What, model.FormatSeconds(r.Limit))
			}
		case model.CauseRestarts:
			if r, ok = limit(m.Cause, m.Value, t.RestartsDegraded, t.RestartsFailing); ok {
				r.Summary = fmt.Sprintf("%s %s, over %s", model.FormatCount(m.Value), m.What, model.FormatCount(r.Limit))
			}
		case model.CauseEvictions, model.CauseRejections:
			d, f, _ := t.LimitsFor(m.Cause)
			if r, ok = limit(m.Cause, m.Value, d, f); ok {
				r.Summary = fmt.Sprintf("%s %s, over %s", model.FormatCount(m.Value), m.What, model.FormatCount(r.Limit))
				if m.Cause == model.CauseEvictions {
					r.Summary += "; eviction may be normal for this cache policy, not proof of request failure"
				}
			}
		default:
			continue
		}
		judged = true
		if ok {
			r.What, r.Via = m.What, m.Via
			reasons = append(reasons, r)
		}
	}
	return reasons, judged
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
func ApplyHealth(g *Graph, t HealthThresholds, overrides ...HealthOverride) {
	// counted is the traffic of the calls whose failures were measured;
	// timed that of the calls that measured latency, and their latencies
	// weighted by it.
	type sum struct {
		rate, counted, failed, timed, timed50, p50, p95 float64
		latencies                                       int
		approximate                                     bool
		evidence                                        []model.Evidence
	}
	in, out := map[string]*sum{}, map[string]*sum{}
	shared := globalRules(t)
	add := func(m map[string]*sum, id string, e *model.Edge) {
		s, ok := m[id]
		if !ok {
			s = &sum{}
			m[id] = s
		}
		s.rate += e.RequestRate
		s.evidence = append(s.evidence, e.Evidence...)
		if !e.ErrorsUnknown {
			s.counted += e.RequestRate
			s.failed += e.RequestRate * e.ErrorRate
		}
		if e.LatencyP95Ms > 0 {
			s.timed += e.RequestRate
			s.p95 += e.RequestRate * e.LatencyP95Ms
			s.latencies++
			s.approximate = s.approximate || e.LatencyApproximate
		}
		if e.LatencyP50Ms > 0 {
			s.timed50 += e.RequestRate
			s.p50 += e.RequestRate * e.LatencyP50Ms
		}
	}
	for _, e := range g.Edges {
		limits, rules := resolveHealth(g, t, overrides, shared, nil, e)
		e.HealthRules = rules
		e.Health, e.Reasons = limits.Judge(Signals{RequestRate: e.RequestRate, ErrorRate: e.ErrorRate, ErrorsUnknown: e.ErrorsUnknown,
			LatencyP95Ms: e.LatencyP95Ms, LatencyApproximate: e.LatencyApproximate, Measures: e.Measures})
		annotateHealth(e.Reasons, rules, e.Evidence)
		if e.RequestRate > 0 {
			add(in, e.To, e)
			add(out, e.From, e)
		}
	}
	for id, n := range g.Nodes {
		limits, rules := resolveHealth(g, t, overrides, shared, n, nil)
		n.HealthRules = rules
		s := in[id]
		n.TrafficDirection = "incoming"
		if s == nil {
			s = out[id]
			n.TrafficDirection = "outgoing"
		}
		if s == nil || s.rate == 0 {
			n.TrafficDirection = ""
			// No traffic to judge, but a measure may still say something.
			n.Health, n.Reasons = limits.Judge(Signals{Measures: n.Measures})
			annotateHealth(n.Reasons, rules, n.Evidence)
			continue
		}
		n.RequestRate = s.rate
		n.Evidence = append(n.Evidence, s.evidence...)
		n.ErrorRate, n.ErrorsUnknown = 0, s.counted == 0
		n.ErrorCoverage = s.counted / s.rate
		if s.counted > 0 {
			n.ErrorRate = s.failed / s.counted
		}
		// A node's latency is how long it takes to answer, so only the calls
		// it serves count, never the ones it makes.
		if s := in[id]; s != nil && s.timed > 0 {
			n.LatencyP95Ms = s.p95 / s.timed
			n.LatencyCoverage = s.timed / s.rate
			n.LatencyApproximate = s.latencies > 1 || s.approximate
			if s.timed50 > 0 {
				n.LatencyP50Ms = s.p50 / s.timed50
			}
		}
		n.Health, n.Reasons = limits.Judge(Signals{RequestRate: n.RequestRate, ErrorRate: n.ErrorRate, ErrorsUnknown: n.ErrorsUnknown,
			LatencyP95Ms: n.LatencyP95Ms, LatencyApproximate: n.LatencyApproximate, Measures: n.Measures})
		annotateHealth(n.Reasons, rules, n.Evidence)
		for i := range n.Reasons {
			r := &n.Reasons[i]
			if r.Cause == model.CauseErrors && n.ErrorCoverage > 0 && n.ErrorCoverage < 1 {
				r.Coverage = n.ErrorCoverage
				r.Summary += ", measured on " + model.FormatPercent(n.ErrorCoverage) + " of traffic"
			}
		}
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
			if r.Via != "" {
				continue // the source named it (the session holding a lock)
			}
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
			if r.Via != "" {
				continue
			}
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
		if e.RequestRate > 0 && !e.ErrorsUnknown {
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
		current := len(e.Evidence) == 0
		for _, ev := range e.Evidence {
			current = current || (!ev.Retained && slices.Contains(ev.Metrics, "topology"))
		}
		if e.Observed && current {
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
			kept.Measures = nil // momentary: locks now, not when last seen
			e = &kept
			g.Edges[key] = e
		} else {
			e.Observed = true
			for _, s := range r.edge.Sources {
				e.Sources = appendUnique(e.Sources, s)
			}
			for _, s := range r.edge.Signals {
				e.Signals = appendUnique(e.Signals, s)
			}
		}
		// Retain the relationship, never replay old rates or latency as live.
		e.Evidence = append([]model.Evidence(nil), r.edge.Evidence...)
		for i := range e.Evidence {
			e.Evidence[i].Retained = true
		}
		if r.edge.Identity != nil {
			kept := &model.ConnectionIdentity{From: retainIdentity(r.edge.Identity.From), To: retainIdentity(r.edge.Identity.To)}
			if ok && e.Identity != nil {
				for _, entry := range e.Identity.From {
					kept.From = appendIdentity(kept.From, entry)
				}
				for _, entry := range e.Identity.To {
					kept.To = appendIdentity(kept.To, entry)
				}
			}
			e.Identity = kept
		}
		e.RequestRate, e.ErrorRate, e.ErrorsUnknown = 0, 0, true
		e.LatencyP50Ms, e.LatencyP95Ms = 0, 0
		e.LatencyApproximate = false
		e.Health, e.Reasons = model.HealthUnknown, nil
		for _, id := range []string{e.From, e.To} {
			if _, ok := g.Nodes[id]; !ok {
				g.Nodes[id] = &model.Node{ID: id, Kind: model.KindUnknown, Sources: e.Sources}
			}
		}
		if e.Identity != nil {
			for _, entry := range e.Identity.From {
				g.Nodes[e.From].Identity = appendIdentity(g.Nodes[e.From].Identity, entry)
			}
			for _, entry := range e.Identity.To {
				g.Nodes[e.To].Identity = appendIdentity(g.Nodes[e.To].Identity, entry)
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
