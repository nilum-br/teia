package tempo

import (
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/nilum-br/teia/internal/model"
)

// Bounds are separate from the trace-search limits. A large trace can still
// contribute topology without making the operation contract unbounded.
const maxOperationSpans = 20000
const maxOperationScopes = 200
const maxOperationRoots = 100
const maxOperationGapIDs = 128
const maxOperationLinks = 32

type sampledTrace struct {
	id    string
	spans []span
}

// operationSamples attributes the sampled spans to entry operations. With
// dimensions configured, every trace, span and unattributed span records the
// business dimension values it carried, so a slice can keep its own.
func operationSamples(source string, window url.Values, traces []sampledTrace, dims map[string]string, requested, failed, searched, available int, limited bool) *model.OperationContract {
	contract := model.EmptyOperations()
	reader := &pairing{dims: dims}
	from, _ := strconv.ParseInt(window.Get("start"), 10, 64)
	to, _ := strconv.ParseInt(window.Get("end"), 10, 64)
	period := model.OperationPeriod{From: time.Unix(from, 0).UTC(), To: time.Unix(to, 0).UTC()}
	status := model.OperationSource{Source: source, QueryPeriod: &period, RequestedTraces: requested, FetchedTraces: len(traces), FailedTraces: failed, QueriedServices: searched, AvailableServices: available, Truncated: limited, Status: "sampled"}
	if failed > 0 || limited {
		status.Status = "partial"
	}
	scopes := map[string]*model.OperationScope{}
	remaining := maxOperationSpans
	emittedRemaining := maxOperationSpans
	for _, trace := range traces {
		if remaining <= 0 {
			status.Truncated, status.Status = true, "partial"
			break
		}
		spans := trace.spans
		truncated := len(spans) > remaining
		if truncated {
			spans = spans[:remaining]
			status.Truncated, status.Status = true, "partial"
		}
		remaining -= len(spans)
		byID := map[string]span{}
		duplicates := map[string]bool{}
		for _, sp := range spans {
			if _, exists := byID[sp.id]; exists || sp.id == "" {
				duplicates[sp.id] = true
			}
			byID[sp.id] = sp
		}
		for id := range duplicates {
			delete(byID, id)
		}
		cyclic := hasParentCycle(byID)
		dimsOf := func(sp span) map[string]string { return reader.dimsOf(sp, byID) }
		children := map[string][]string{}
		var roots, missing []string
		for id, sp := range byID {
			if (sp.kind == "server" || sp.kind == "consumer") && sp.service != "" && normalizedOperation(sp) != "" {
				roots = append(roots, id)
			}
			if sp.parent != "" {
				if _, exists := byID[sp.parent]; !exists {
					missing = append(missing, sp.parent)
				} else {
					children[sp.parent] = append(children[sp.parent], id)
				}
			}
		}
		sort.Strings(roots)
		sort.Strings(missing)
		missing = slices.Compact(missing)
		attributed := map[string]bool{}
		// Mark the union once. Materializing every nested root's descendants
		// before applying the output budget would require quadratic memory.
		queue := slices.Clone(roots)
		for _, root := range roots {
			attributed[root] = true
		}
		for i := 0; i < len(queue); i++ {
			for _, child := range children[queue[i]] {
				if !attributed[child] {
					attributed[child] = true
					queue = append(queue, child)
				}
			}
		}
		var unattributed []string
		for _, sp := range spans {
			if attributed[sp.id] && !duplicates[sp.id] {
				continue
			}
			reason := "no-matched-entry-ancestor"
			if duplicates[sp.id] {
				reason = "missing-or-duplicate-span-id"
			} else if sp.parent != "" {
				if _, exists := byID[sp.parent]; !exists {
					reason = "missing-parent"
				}
			}
			contract.Unattributed = append(contract.Unattributed, model.OperationUnattributed{Source: source, TraceID: trace.id, SpanID: sp.id, NodeID: sp.service, Reason: reason, Dimensions: dimsOf(sp)})
			unattributed = append(unattributed, sp.id)
		}
		sort.Strings(unattributed)
		unattributed = slices.Compact(unattributed)
		for _, rootID := range roots {
			root := byID[rootID]
			operation := normalizedOperation(root)
			key := model.OperationID(source, root.service, operation)
			scope := scopes[key]
			if emittedRemaining == 0 {
				status.Truncated, status.Status = true, "partial"
				if scope != nil {
					scope.Truncated = true
				}
				continue
			}
			if scope == nil {
				if len(scopes) >= maxOperationScopes {
					status.Truncated, status.Status = true, "partial"
					continue
				}
				scope = &model.OperationScope{ID: key, Source: source, EntryService: root.service, Operation: operation, QueryPeriod: period,
					SampleRootDurationsMs: []float64{}, Nodes: []model.OperationNode{}, Edges: []model.OperationEdge{}, Traces: []model.OperationTrace{}}
				scopes[key] = scope
			}
			if len(scope.Traces) >= maxOperationRoots {
				scope.Truncated, status.Truncated, status.Status = true, true, "partial"
				continue
			}
			ids, cut := descendants(rootID, children, emittedRemaining)
			if cut {
				status.Truncated, status.Status = true, "partial"
			}
			emittedRemaining -= len(ids)
			addOperationTrace(scope, trace.id, root, ids, byID, missing, unattributed, truncated || cut || len(duplicates) > 0, cyclic, dimsOf)
		}
	}
	for _, scope := range scopes {
		contract.Scopes = append(contract.Scopes, *scope)
		if scope.Truncated {
			status.Truncated, status.Status = true, "partial"
		}
	}
	sort.Slice(contract.Scopes, func(i, j int) bool { return contract.Scopes[i].ID < contract.Scopes[j].ID })
	contract.Sources = append(contract.Sources, status)
	return contract
}

func descendants(root string, children map[string][]string, limit int) ([]string, bool) {
	seen := map[string]bool{root: true}
	queue := []string{root}
	for i := 0; i < len(queue); i++ {
		ordered := slices.Clone(children[queue[i]])
		sort.Strings(ordered)
		for _, child := range ordered {
			if !seen[child] {
				if len(queue) == limit {
					return queue, true
				}
				seen[child] = true
				queue = append(queue, child)
			}
		}
	}
	return queue, false
}

func hasParentCycle(spans map[string]span) bool {
	done := map[string]bool{}
	for id := range spans {
		path := map[string]bool{}
		for id != "" && !done[id] {
			if path[id] {
				return true
			}
			path[id] = true
			id = spans[id].parent
		}
		for visited := range path {
			done[visited] = true
		}
	}
	return false
}

func addOperationTrace(scope *model.OperationScope, traceID string, root span, ids []string, spans map[string]span, missing, unattributed []string, truncated, cyclic bool, dimsOf func(span) map[string]string) {
	if len(missing) > maxOperationGapIDs || len(unattributed) > maxOperationGapIDs {
		truncated = true
	}
	missing = missing[:min(len(missing), maxOperationGapIDs)]
	unattributed = unattributed[:min(len(unattributed), maxOperationGapIDs)]
	trace := model.OperationTrace{TraceID: traceID, RootSpanID: root.id, RootDurationMs: durationMs(root), RootOutcome: operationOutcome(root),
		SpanIDs: ids, MissingParentSpanIDs: append([]string{}, missing...), UnattributedSpanIDs: append([]string{}, unattributed...), Spans: []model.OperationSpan{}, StructuralStatus: "linked-sample", Truncated: truncated,
		Dimensions: dimsOf(root)}
	nodes := map[string]model.NodeKind{}
	for _, node := range scope.Nodes {
		nodes[node.ID] = node.Kind
	}
	edges := map[string]*model.OperationEdge{}
	for i := range scope.Edges {
		edge := &scope.Edges[i]
		edges[edge.From+"\x00"+edge.To] = edge
	}
	inside := map[string]bool{}
	for _, id := range ids {
		inside[id] = true
	}
	remoteParent := map[string]bool{}
	add := func(from, to, attribution string, call span) {
		if from == "" || to == "" || from == to {
			return
		}
		key := from + "\x00" + to
		edge := edges[key]
		if edge == nil {
			edge = &model.OperationEdge{From: from, To: to, TraceIDs: []string{}, Calls: []model.OperationCall{}}
			edges[key] = edge
		}
		if !slices.Contains(edge.TraceIDs, traceID) {
			edge.TraceIDs = append(edge.TraceIDs, traceID)
		}
		if !slices.ContainsFunc(edge.Calls, func(c model.OperationCall) bool {
			return c.TraceID == traceID && c.SpanID == call.id && c.Attribution == attribution
		}) {
			edge.Calls = append(edge.Calls, model.OperationCall{TraceID: traceID, SpanID: call.id, ParentSpanID: call.parent, Attribution: attribution, StartedAt: call.startedAt, EndedAt: call.endedAt, DurationMs: durationMs(call)})
		}
	}
	for _, id := range ids {
		sp := spans[id]
		if sp.service != "" {
			nodes[sp.service] = model.KindService
		}
		links := sp.links[:min(len(sp.links), maxOperationLinks)]
		if len(sp.links) > maxOperationLinks {
			trace.Truncated, truncated = true, true
		}
		trace.Spans = append(trace.Spans, model.OperationSpan{SpanID: id, ParentSpanID: sp.parent, NodeID: sp.service, Kind: sp.kind, StartedAt: sp.startedAt, EndedAt: sp.endedAt, Links: append([]model.OperationSpanLink{}, links...), Dimensions: dimsOf(sp)})
		if len(sp.links) > 0 {
			trace.StructuralStatus = "gaps-detected" // links are preserved, never traversed as synchronous ancestry
		}
		parent, exists := spans[sp.parent]
		if !exists || !inside[sp.parent] || parent.service == sp.service || (sp.kind != "server" && sp.kind != "consumer") {
			continue
		}
		remoteParent[parent.id] = true
		add(parent.service, sp.service, "parent-span", sp)
	}
	for _, id := range ids {
		sp := spans[id]
		if remoteParent[id] || (sp.kind != "client" && sp.kind != "producer") {
			continue
		}
		target, kind := peerOf(sp.attrs)
		if target != "" {
			if _, exists := nodes[target]; !exists {
				nodes[target] = kind
			}
			add(sp.service, target, "client-peer", sp)
		}
	}
	if len(missing) > 0 || truncated || cyclic {
		trace.StructuralStatus = "gaps-detected"
	}
	if trace.StructuralStatus == "gaps-detected" && !slices.ContainsFunc(scope.Traces, func(t model.OperationTrace) bool {
		return t.TraceID == traceID && t.StructuralStatus == "gaps-detected"
	}) {
		scope.IncompleteTraceCount++
	}
	scope.SampledRootCount++
	if trace.RootDurationMs != nil {
		scope.SampleRootDurationsMs = append(scope.SampleRootDurationsMs, *trace.RootDurationMs)
	}
	switch trace.RootOutcome {
	case "ok":
		scope.RootOutcomes.OK++
	case "error":
		scope.RootOutcomes.Error++
	default:
		scope.RootOutcomes.Unknown++
	}
	scope.Truncated = scope.Truncated || truncated
	scope.Traces = append(scope.Traces, trace)
	scope.Nodes = nil
	for id, kind := range nodes {
		scope.Nodes = append(scope.Nodes, model.OperationNode{ID: id, Kind: kind})
	}
	sort.Slice(scope.Nodes, func(i, j int) bool { return scope.Nodes[i].ID < scope.Nodes[j].ID })
	scope.Edges = []model.OperationEdge{}
	for _, edge := range edges {
		scope.Edges = append(scope.Edges, *edge)
	}
	sort.Slice(scope.Edges, func(i, j int) bool {
		return scope.Edges[i].From+"\x00"+scope.Edges[i].To < scope.Edges[j].From+"\x00"+scope.Edges[j].To
	})
}

func durationMs(sp span) *float64 {
	if sp.startedAt.IsZero() || sp.endedAt.Before(sp.startedAt) {
		return nil
	}
	ms := float64(sp.endedAt.Sub(sp.startedAt)) / float64(time.Millisecond)
	return &ms
}

func normalizedOperation(sp span) string {
	safe := func(value string) bool {
		return value != "" && len(value) <= 200 && !strings.ContainsAny(value, "?#") && !strings.Contains(value, "://") && !strings.ContainsFunc(value, unicode.IsControl)
	}
	if explicit := strings.TrimSpace(sp.attrs["teia.operation"]); safe(explicit) {
		return explicit
	}
	if route := strings.TrimSpace(sp.attrs["http.route"]); safe(route) {
		method := strings.ToUpper(first(sp.attrs, "http.request.method", "http.method"))
		if method == "" || len(method) > 16 || strings.ContainsFunc(method, func(r rune) bool { return (r < 'A' || r > 'Z') && r != '_' }) {
			return ""
		}
		return method + " " + route
	}
	service, method := sp.attrs["rpc.service"], sp.attrs["rpc.method"]
	if safe(service) && safe(method) {
		return service + "/" + method
	}
	return "" // span names and raw URLs are not normalized route contracts
}

func operationOutcome(sp span) string {
	if sp.status == "2" || sp.status == "STATUS_CODE_ERROR" || sp.attrs["error.type"] != "" {
		return "error"
	}
	status, _ := strconv.Atoi(first(sp.attrs, "http.response.status_code", "http.status_code"))
	if status >= 500 && status <= 599 {
		return "error"
	}
	if sp.status == "1" || sp.status == "STATUS_CODE_OK" || status >= 100 && status < 500 {
		return "ok"
	}
	return "unknown"
}
