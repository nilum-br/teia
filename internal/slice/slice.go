// Package slice narrows a discovery round to the calls whose own telemetry
// carried a selection of business dimensions (journey, tenant...).
//
// A label journey follows service ownership: a service shared by two
// journeys brings every call it makes. A slice follows the calls instead: a
// trace span or a metric series says which journey and tenant it belongs to,
// so two journeys through the same services keep different connections and
// their own rates, errors and latencies. Sources that cannot split their
// edges by the selected dimensions are left out and named, never mixed in.
package slice

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

// Selector maps each selected dimension to its value. Every pair must hold.
type Selector map[string]string

// ValidName checks a dimension name or value: 1–128 bytes, without the
// selector's separators (",", ":", "=") or control characters.
func ValidName(s string) error {
	switch {
	case strings.TrimSpace(s) == "" || len(s) > 128:
		return fmt.Errorf("%q must be 1–128 bytes", s)
	case strings.ContainsAny(s, ",:="):
		return fmt.Errorf("%q must not contain commas, colons or equals signs", s)
	case strings.ContainsFunc(s, unicode.IsControl):
		return fmt.Errorf("%q must not contain control characters", s)
	}
	return nil
}

// Parse reads "journey:checkout,tenant:acme" (or "journey=checkout").
// An empty string is no selection: nil, nil.
func Parse(raw string) (Selector, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, fmt.Errorf("slice must be at most 2048 bytes")
	}
	sel := Selector{}
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		i := strings.IndexAny(part, ":=")
		if i <= 0 {
			return nil, fmt.Errorf("slice %q: expected dimension:value", part)
		}
		dim, value := strings.TrimSpace(part[:i]), strings.TrimSpace(part[i+1:])
		for _, s := range []string{dim, value} {
			if err := ValidName(s); err != nil {
				return nil, fmt.Errorf("slice: %w", err)
			}
		}
		if old, ok := sel[dim]; ok && old != value {
			return nil, fmt.Errorf("slice: dimension %q selected twice; one value per dimension", dim)
		}
		sel[dim] = value
	}
	return sel, nil
}

// String is the canonical form: dimensions sorted, "dim:value" by commas.
func (s Selector) String() string {
	parts := make([]string, 0, len(s))
	for _, k := range slices.Sorted(maps.Keys(s)) {
		parts = append(parts, k+":"+s[k])
	}
	return strings.Join(parts, ",")
}

func (s Selector) matches(dims map[string]string) bool {
	for k, v := range s {
		if dims[k] != v {
			return false
		}
	}
	return true
}

func (s Selector) supportedBy(dims []string) bool {
	for k := range s {
		if !slices.Contains(dims, k) {
			return false
		}
	}
	return true
}

// Results narrows a round's results to the selected calls. Each observed
// edge of a source that splits by every selected dimension keeps only its
// matching slices, measured as that subset; edges without a match are
// dropped. Observed edges of other sources are dropped and their source is
// reported as excluded. Sampled operations keep the selected traces and
// calls, or are marked excluded with the source. Inventory, declarations and
// readings stay as they are: Prune later removes what no selected call touches.
func Results(results []graph.Result, sel Selector) (out []graph.Result, contributed, excluded []string) {
	out = make([]graph.Result, 0, len(results))
	for _, r := range results {
		obs := r.Observation
		edges := make([]source.EdgeObservation, 0, len(obs.Edges))
		supported := sel.supportedBy(obs.Dimensions)
		dropped, kept := false, false
		for _, e := range obs.Edges {
			if e.Declared {
				edges = append(edges, e)
				continue
			}
			if !supported {
				dropped = true
				continue
			}
			var matched []source.Slice
			for _, sl := range e.Slices {
				if sel.matches(sl.Dims) {
					matched = append(matched, sl)
				}
			}
			if len(matched) == 0 {
				continue
			}
			edges = append(edges, narrow(e, matched))
			kept = true
		}
		if obs.Operations != nil {
			if supported {
				obs.Operations = operations(obs.Operations, sel)
			} else {
				obs.Operations = excludedOperations(obs.Operations)
			}
		}
		if r.Err == nil && dropped {
			excluded = append(excluded, r.Source.Name())
		}
		if r.Err == nil && kept {
			contributed = append(contributed, r.Source.Name())
		}
		obs.Edges = edges
		// Callers a signal cannot name are counted per callee, not per
		// journey: they cannot be attributed to the selection.
		obs.UnknownCallers = nil
		r.Observation = obs
		out = append(out, r)
	}
	sort.Strings(contributed)
	sort.Strings(excluded)
	return out, contributed, excluded
}

// narrow replaces an edge's measurements with those of its matched slices.
func narrow(e source.EdgeObservation, matched []source.Slice) source.EdgeObservation {
	agg := aggregate(matched)
	e.RequestRate, e.ErrorRate, e.NoErrors = agg.RequestRate, agg.ErrorRate, agg.NoErrors
	e.LatencyP50Ms, e.LatencyP95Ms, e.LatencyApproximate = agg.LatencyP50Ms, agg.LatencyP95Ms, agg.LatencyApproximate
	e.SampleCount, e.ObservedFrom, e.ObservedAt = agg.SampleCount, agg.ObservedFrom, agg.ObservedAt
	// Edge-wide readings (locks held by this caller...) are not per journey.
	e.Measures = nil
	e.Slices = matched
	return e
}

// operations narrows a source's sampled operations to the traces whose entry
// span carried the selection and, inside them, to the spans and calls that
// carried it too: the calls the slice's edges come from. Counts, root outcomes
// and durations are recomputed from the kept entry spans.
func operations(c *model.OperationContract, sel Selector) *model.OperationContract {
	out := *c
	out.Sources = slices.Clone(c.Sources)
	out.Scopes, out.Unattributed = []model.OperationScope{}, []model.OperationUnattributed{}
	for _, u := range c.Unattributed {
		if sel.matches(u.Dimensions) {
			out.Unattributed = append(out.Unattributed, u)
		}
	}
	for _, scope := range c.Scopes {
		if kept, ok := operationScope(scope, sel); ok {
			out.Scopes = append(out.Scopes, kept)
		}
	}
	return &out
}

// excludedOperations keeps the status of a source that cannot split its
// traces by the selection, marked excluded, and none of its operations.
func excludedOperations(c *model.OperationContract) *model.OperationContract {
	out := *c
	out.Sources = slices.Clone(c.Sources)
	for i := range out.Sources {
		out.Sources[i].Status = "excluded"
	}
	out.Scopes, out.Unattributed = []model.OperationScope{}, []model.OperationUnattributed{}
	return &out
}

// operationScope keeps one operation's selected traces, spans and calls; it
// reports false when no sampled entry carried the selection.
func operationScope(scope model.OperationScope, sel Selector) (model.OperationScope, bool) {
	kinds := map[string]model.NodeKind{}
	for _, n := range scope.Nodes {
		kinds[n.ID] = n.Kind
	}
	kindOf := func(id string) model.NodeKind {
		if kind, ok := kinds[id]; ok {
			return kind
		}
		return model.KindService
	}
	out := scope
	out.SampledRootCount, out.IncompleteTraceCount, out.RootOutcomes = 0, 0, model.OperationOutcomes{}
	out.SampleRootDurationsMs, out.Traces = []float64{}, []model.OperationTrace{}
	nodes := map[string]model.NodeKind{}
	kept := map[string]bool{} // trace ID and span ID of every selected span
	gaps := map[string]bool{}
	for _, t := range scope.Traces {
		if !sel.matches(t.Dimensions) {
			continue
		}
		trace := t
		trace.SpanIDs, trace.Spans = []string{}, []model.OperationSpan{}
		for _, sp := range t.Spans {
			if !sel.matches(sp.Dimensions) {
				continue
			}
			trace.SpanIDs = append(trace.SpanIDs, sp.SpanID)
			trace.Spans = append(trace.Spans, sp)
			kept[t.TraceID+"\x00"+sp.SpanID] = true
			if sp.NodeID != "" {
				nodes[sp.NodeID] = kindOf(sp.NodeID)
			}
		}
		out.Traces = append(out.Traces, trace)
		out.SampledRootCount++
		if t.RootDurationMs != nil {
			out.SampleRootDurationsMs = append(out.SampleRootDurationsMs, *t.RootDurationMs)
		}
		switch t.RootOutcome {
		case "ok":
			out.RootOutcomes.OK++
		case "error":
			out.RootOutcomes.Error++
		default:
			out.RootOutcomes.Unknown++
		}
		if t.StructuralStatus == "gaps-detected" && !gaps[t.TraceID] {
			gaps[t.TraceID] = true
			out.IncompleteTraceCount++
		}
	}
	if out.SampledRootCount == 0 {
		return out, false
	}
	out.Edges = []model.OperationEdge{}
	for _, e := range scope.Edges {
		edge := e
		edge.Calls, edge.TraceIDs = []model.OperationCall{}, []string{}
		for _, call := range e.Calls {
			if !kept[call.TraceID+"\x00"+call.SpanID] {
				continue
			}
			edge.Calls = append(edge.Calls, call)
			if !slices.Contains(edge.TraceIDs, call.TraceID) {
				edge.TraceIDs = append(edge.TraceIDs, call.TraceID)
			}
		}
		if len(edge.Calls) == 0 {
			continue
		}
		out.Edges = append(out.Edges, edge)
		for _, id := range []string{e.From, e.To} {
			if _, ok := nodes[id]; !ok {
				nodes[id] = kindOf(id)
			}
		}
	}
	out.Nodes = make([]model.OperationNode, 0, len(nodes))
	for id, kind := range nodes {
		out.Nodes = append(out.Nodes, model.OperationNode{ID: id, Kind: kind})
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	return out, true
}

// aggregate combines the slices a selection matched (for instance every
// tenant of one journey). Rates add up; the error share counts only slices
// whose failures were measured; percentiles of several slices are weighted
// by traffic (or sample count) and marked approximate, never summed.
func aggregate(matched []source.Slice) source.Slice {
	if len(matched) == 1 {
		return matched[0]
	}
	var out source.Slice
	var measured, failed, weight, p50, p95 float64
	timed := 0
	for i, sl := range matched {
		out.RequestRate += sl.RequestRate
		if !sl.NoErrors && sl.RequestRate > 0 {
			measured += sl.RequestRate
			failed += sl.ErrorRate * sl.RequestRate
		}
		out.SampleCount += sl.SampleCount
		if sl.LatencyP95Ms > 0 {
			w := sl.RequestRate
			if w <= 0 {
				w = math.Max(1, float64(sl.SampleCount))
			}
			weight += w
			p50 += sl.LatencyP50Ms * w
			p95 += sl.LatencyP95Ms * w
			timed++
		}
		switch {
		case i == 0:
			out.ObservedFrom, out.ObservedAt = sl.ObservedFrom, sl.ObservedAt
		case sl.ObservedFrom.IsZero() || sl.ObservedAt.IsZero() || out.ObservedFrom.IsZero() || out.ObservedAt.IsZero():
			// One undated sample makes the whole envelope unknown.
			out.ObservedFrom, out.ObservedAt = time.Time{}, time.Time{}
		default:
			if sl.ObservedFrom.Before(out.ObservedFrom) {
				out.ObservedFrom = sl.ObservedFrom
			}
			if sl.ObservedAt.After(out.ObservedAt) {
				out.ObservedAt = sl.ObservedAt
			}
		}
	}
	out.NoErrors = measured == 0
	if measured > 0 {
		out.ErrorRate = math.Min(1, failed/measured)
	}
	if weight > 0 {
		out.LatencyP50Ms, out.LatencyP95Ms = p50/weight, p95/weight
		out.LatencyApproximate = timed > 1 || slices.ContainsFunc(matched, func(s source.Slice) bool { return s.LatencyApproximate })
	}
	return out
}

// Prune keeps only what the selected calls touch: their observed edges (with
// the declaration they matched) and the nodes at either end. Declared flows
// the calls never made are left out, even between two kept nodes:
// declarations are not per journey, so one there would read as a stale
// dependency of the selection.
func Prune(g *graph.Graph) {
	touched := map[string]bool{}
	for key, e := range g.Edges {
		if !e.Observed {
			delete(g.Edges, key)
			continue
		}
		touched[e.From], touched[e.To] = true, true
	}
	for id := range g.Nodes {
		if !touched[id] {
			delete(g.Nodes, id)
		}
	}
	for id := range g.UnknownCallers {
		if !touched[id] {
			delete(g.UnknownCallers, id)
		}
	}
}

// Values collects every dimension value seen on the edges, sorted.
func Values(edges []model.Edge) map[string][]string {
	var out map[string][]string
	for _, e := range edges {
		for dim, values := range e.Dimensions {
			if out == nil {
				out = map[string][]string{}
			}
			for _, v := range values {
				if !slices.Contains(out[dim], v) {
					out[dim] = append(out[dim], v)
				}
			}
		}
	}
	for dim := range out {
		sort.Strings(out[dim])
	}
	return out
}
