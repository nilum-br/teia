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
// reported as excluded. Inventory, declarations and readings stay as they are:
// Prune later removes what no selected call touches.
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
