package engine

import (
	"slices"
	"sort"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
)

// operationContract preserves each source's query interval and sampling scope.
// Failed sources are explicit, and retained topology never refreshes traces.
func operationContract(results []graph.Result, g *graph.Graph) *model.OperationContract {
	out := model.EmptyOperations()
	for _, result := range results {
		if result.Source.Type() != "tempo" && result.Observation.Operations == nil {
			continue
		}
		contract := result.Observation.Operations
		if result.Err != nil || result.Retained || contract == nil {
			out.Sources = append(out.Sources, model.OperationSource{Source: result.Source.Name(), Status: "unavailable"})
			continue
		}
		for _, status := range contract.Sources {
			status.CollectedAt = result.CollectedAt
			out.Sources = append(out.Sources, status)
		}
		for _, original := range contract.Scopes {
			if g.Ignored[g.Canonical(original.EntryService)] {
				continue
			}
			scope := original
			scope.EntryService = g.Canonical(scope.EntryService)
			scope.ID = model.OperationID(scope.Source, scope.EntryService, scope.Operation)
			scope.SampleRootDurationsMs = slices.Clone(original.SampleRootDurationsMs)
			nodes := map[string]model.OperationNode{}
			for _, node := range original.Nodes {
				node.ID = g.Canonical(node.ID)
				if g.Ignored[node.ID] {
					scope.Truncated = true
					continue
				}
				if known := g.Nodes[node.ID]; known != nil {
					node.Kind = known.Kind // identity metadata only, never global health or metrics
				}
				nodes[node.ID] = node
			}
			scope.Nodes = []model.OperationNode{}
			for _, node := range nodes {
				scope.Nodes = append(scope.Nodes, node)
			}
			sort.Slice(scope.Nodes, func(i, j int) bool { return scope.Nodes[i].ID < scope.Nodes[j].ID })
			edges := map[string]*model.OperationEdge{}
			for _, edge := range original.Edges {
				edge.From, edge.To = g.Canonical(edge.From), g.Canonical(edge.To)
				if g.Ignored[edge.From] || g.Ignored[edge.To] || edge.From == edge.To {
					continue
				}
				key := edge.From + "\x00" + edge.To
				merged := edges[key]
				if merged == nil {
					merged = &model.OperationEdge{From: edge.From, To: edge.To, TraceIDs: []string{}, Calls: []model.OperationCall{}}
					edges[key] = merged
				}
				merged.Calls = append(merged.Calls, edge.Calls...)
				for _, id := range edge.TraceIDs {
					if !slices.Contains(merged.TraceIDs, id) {
						merged.TraceIDs = append(merged.TraceIDs, id)
					}
				}
			}
			scope.Edges = []model.OperationEdge{}
			for _, edge := range edges {
				scope.Edges = append(scope.Edges, *edge)
			}
			sort.Slice(scope.Edges, func(i, j int) bool {
				return scope.Edges[i].From+"\x00"+scope.Edges[i].To < scope.Edges[j].From+"\x00"+scope.Edges[j].To
			})
			scope.Traces = slices.Clone(original.Traces)
			for i := range scope.Traces {
				trace := &scope.Traces[i]
				trace.Spans = nil
				trace.SpanIDs = nil
				for _, span := range original.Traces[i].Spans {
					span.NodeID = g.Canonical(span.NodeID)
					if g.Ignored[span.NodeID] {
						trace.Truncated, trace.StructuralStatus = true, "gaps-detected"
						continue
					}
					span.Links = slices.Clone(span.Links)
					trace.Spans = append(trace.Spans, span)
					trace.SpanIDs = append(trace.SpanIDs, span.SpanID)
				}
			}
			out.Scopes = append(out.Scopes, scope)
		}
		for _, orphan := range contract.Unattributed {
			orphan.NodeID = g.Canonical(orphan.NodeID)
			if !g.Ignored[orphan.NodeID] {
				out.Unattributed = append(out.Unattributed, orphan)
			}
		}
	}
	out.Scopes = coalesceOperations(out.Scopes)
	return out
}

// Aliases can make two raw entry-service names one identity. Keep a unique
// selector ID and count root spans/calls once instead of dropping either scope.
func coalesceOperations(input []model.OperationScope) []model.OperationScope {
	groups := map[string][]model.OperationScope{}
	for _, scope := range input {
		groups[scope.ID] = append(groups[scope.ID], scope)
	}
	out := make([]model.OperationScope, 0, len(groups))
	for _, group := range groups {
		merged := group[0]
		merged.Nodes, merged.Edges, merged.Traces = []model.OperationNode{}, []model.OperationEdge{}, []model.OperationTrace{}
		merged.SampleRootDurationsMs = []float64{}
		merged.SampledRootCount, merged.IncompleteTraceCount, merged.RootOutcomes = 0, 0, model.OperationOutcomes{}
		nodes, edges, roots, incomplete := map[string]bool{}, map[string]int{}, map[string]bool{}, map[string]bool{}
		for _, scope := range group {
			merged.Truncated = merged.Truncated || scope.Truncated
			for _, node := range scope.Nodes {
				if !nodes[node.ID] {
					merged.Nodes = append(merged.Nodes, node)
					nodes[node.ID] = true
				}
			}
			for _, edge := range scope.Edges {
				key := edge.From + "\x00" + edge.To
				i, found := edges[key]
				if !found {
					i = len(merged.Edges)
					edges[key] = i
					merged.Edges = append(merged.Edges, model.OperationEdge{From: edge.From, To: edge.To, TraceIDs: []string{}, Calls: []model.OperationCall{}})
				}
				target := &merged.Edges[i]
				for _, id := range edge.TraceIDs {
					if !slices.Contains(target.TraceIDs, id) {
						target.TraceIDs = append(target.TraceIDs, id)
					}
				}
				for _, call := range edge.Calls {
					if !slices.ContainsFunc(target.Calls, func(c model.OperationCall) bool {
						return c.TraceID == call.TraceID && c.SpanID == call.SpanID && c.Attribution == call.Attribution
					}) {
						target.Calls = append(target.Calls, call)
					}
				}
			}
			for _, trace := range scope.Traces {
				key := trace.TraceID + "\x00" + trace.RootSpanID
				if roots[key] {
					continue
				}
				roots[key] = true
				merged.Traces = append(merged.Traces, trace)
				merged.SampledRootCount++
				if trace.RootDurationMs != nil {
					merged.SampleRootDurationsMs = append(merged.SampleRootDurationsMs, *trace.RootDurationMs)
				}
				if trace.StructuralStatus == "gaps-detected" {
					incomplete[trace.TraceID] = true
				}
				switch trace.RootOutcome {
				case "ok":
					merged.RootOutcomes.OK++
				case "error":
					merged.RootOutcomes.Error++
				default:
					merged.RootOutcomes.Unknown++
				}
			}
		}
		merged.IncompleteTraceCount = len(incomplete)
		sort.Slice(merged.Nodes, func(i, j int) bool { return merged.Nodes[i].ID < merged.Nodes[j].ID })
		sort.Slice(merged.Edges, func(i, j int) bool {
			return merged.Edges[i].From+"\x00"+merged.Edges[i].To < merged.Edges[j].From+"\x00"+merged.Edges[j].To
		})
		out = append(out, merged)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
