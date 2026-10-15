package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/slice"
	"github.com/nilum-br/teia/internal/source"
)

// A slice keeps an operation's traces whose entry carried the selection and,
// in them, the calls that did; counts are recomputed, and a trace source that
// cannot split its traces is marked excluded instead of mixed in.
func TestSlicesNarrowSampledOperations(t *testing.T) {
	journey := func(j string) map[string]string {
		if j == "" {
			return nil
		}
		return map[string]string{"journey": j}
	}
	opSpan := func(id, parent, node, kind, j string) model.OperationSpan {
		return model.OperationSpan{SpanID: id, ParentSpanID: parent, NodeID: node, Kind: kind, Links: []model.OperationSpanLink{}, Dimensions: journey(j)}
	}
	opTrace := func(id, j, outcome string, ms float64, spans ...model.OperationSpan) model.OperationTrace {
		trace := model.OperationTrace{TraceID: id, RootSpanID: spans[0].SpanID, RootDurationMs: &ms, RootOutcome: outcome, Spans: spans, StructuralStatus: "linked-sample", Dimensions: journey(j)}
		for _, sp := range spans {
			trace.SpanIDs = append(trace.SpanIDs, sp.SpanID)
		}
		return trace
	}
	opScope := func(source, operation string, edges []model.OperationEdge, traces ...model.OperationTrace) model.OperationScope {
		scope := model.OperationScope{ID: model.OperationID(source, "web", operation), Source: source, EntryService: "web", Operation: operation,
			Nodes: []model.OperationNode{{ID: "web", Kind: model.KindService}}, Edges: edges, Traces: traces, SampledRootCount: len(traces)}
		for _, e := range edges {
			scope.Nodes = append(scope.Nodes, model.OperationNode{ID: e.To, Kind: model.KindService})
		}
		return scope
	}
	call := func(trace, span string) []model.OperationCall {
		return []model.OperationCall{{TraceID: trace, SpanID: span, Attribution: "parent-span"}}
	}
	// Checkout calls orders, which notifies the mailer for another journey.
	checkout := opScope("tempo", "POST /checkout", []model.OperationEdge{
		{From: "web", To: "orders", TraceIDs: []string{"t1"}, Calls: call("t1", "orders-in")},
		{From: "orders", To: "mailer", TraceIDs: []string{"t1"}, Calls: call("t1", "mail-out")},
	}, opTrace("t1", "checkout", "ok", 30, opSpan("web-in", "", "web", "server", "checkout"),
		opSpan("orders-in", "web-in", "orders", "server", "checkout"), opSpan("mail-out", "orders-in", "orders", "client", "notification")))
	// The home page served one checkout request and one browse request.
	home := opScope("tempo", "GET /home", nil, opTrace("t2", "checkout", "ok", 10, opSpan("home-1", "", "web", "server", "checkout")),
		opTrace("t3", "browse", "error", 50, opSpan("home-2", "", "web", "server", "browse")))
	browse := opScope("tempo", "GET /browse", nil, opTrace("t4", "browse", "ok", 20, opSpan("browse-in", "", "web", "server", "browse")))
	contract := model.EmptyOperations()
	contract.Sources = []model.OperationSource{{Source: "tempo", Status: "sampled"}}
	contract.Scopes = []model.OperationScope{checkout, home, browse}
	contract.Unattributed = []model.OperationUnattributed{
		{Source: "tempo", TraceID: "t1", SpanID: "lost", NodeID: "worker", Reason: "missing-parent", Dimensions: journey("checkout")},
		{Source: "tempo", TraceID: "t4", SpanID: "gone", NodeID: "worker", Reason: "missing-parent", Dimensions: journey("browse")},
	}
	edge := func(from, to, j string) source.EdgeObservation {
		return source.EdgeObservation{From: from, To: to, Signal: source.SignalTraces, SampleCount: 1, NoErrors: true,
			Slices: []source.Slice{{Dims: journey(j), SampleCount: 1, NoErrors: true}}}
	}
	traces := sliceSource{"tempo", "tempo", source.Observation{Dimensions: []string{"journey"}, Operations: contract,
		Edges: []source.EdgeObservation{edge("web", "orders", "checkout"), edge("web", "catalog", "browse")}}}
	// A second trace source without dimensions cannot split its traces.
	legacy := model.EmptyOperations()
	legacy.Sources = []model.OperationSource{{Source: "tempo-legacy", Status: "sampled"}}
	legacy.Scopes = []model.OperationScope{opScope("tempo-legacy", "GET /report", nil, opTrace("t5", "", "ok", 5, opSpan("report", "", "web", "server", "")))}
	plain := sliceSource{"tempo-legacy", "tempo", source.Observation{Operations: legacy}}

	eng := New([]source.Source{traces, plain}, Options{}, quiet())
	if whole := eng.Refresh(context.Background()); len(whole.Operations.Scopes) != 4 {
		t.Fatalf("whole round scopes = %+v", whole.Operations.Scopes)
	}
	ops := eng.Sliced(slice.Selector{"journey": "checkout"}).Operations
	if ops == nil {
		t.Fatal("a sliced round carries its own operations")
	}
	scopes := map[string]model.OperationScope{}
	for _, s := range ops.Scopes {
		scopes[s.Operation] = s
	}
	if len(ops.Scopes) != 2 || scopes["POST /checkout"].ID == "" || scopes["GET /home"].ID == "" {
		t.Fatalf("checkout slice scopes = %v", scopes)
	}
	co := scopes["POST /checkout"]
	var nodes []string
	for _, n := range co.Nodes {
		nodes = append(nodes, n.ID)
	}
	if len(co.Edges) != 1 || co.Edges[0].To != "orders" || len(co.Traces[0].Spans) != 2 || !reflect.DeepEqual(nodes, []string{"orders", "web"}) {
		t.Fatalf("the notification call stayed in the checkout slice: %+v", co)
	}
	h := scopes["GET /home"]
	if h.SampledRootCount != 1 || !reflect.DeepEqual(h.SampleRootDurationsMs, []float64{10}) || h.RootOutcomes != (model.OperationOutcomes{OK: 1}) {
		t.Fatalf("home counts the browse request: %+v", h)
	}
	statuses := map[string]string{}
	for _, s := range ops.Sources {
		statuses[s.Source] = s.Status
	}
	if statuses["tempo"] != "sampled" || statuses["tempo-legacy"] != "excluded" {
		t.Fatalf("source statuses = %v", statuses)
	}
	if len(ops.Unattributed) != 1 || ops.Unattributed[0].SpanID != "lost" {
		t.Fatalf("unattributed = %+v", ops.Unattributed)
	}
	for _, s := range eng.Snapshot().Operations.Scopes {
		if s.Operation == "POST /checkout" && (len(s.Edges) != 2 || len(s.Traces[0].Spans) != 3) {
			t.Fatalf("slicing changed the whole round: %+v", s)
		}
	}
}

func TestOperationSamplesAreCanonicalAndNeverRetainedAsCurrent(t *testing.T) {
	at := time.Now().UTC()
	contract := model.EmptyOperations()
	contract.Sources = []model.OperationSource{{Source: "tempo", Status: "sampled", QueryPeriod: &model.OperationPeriod{From: at.Add(-time.Minute), To: at}}}
	for i, name := range []string{"web-alias", "web"} {
		id := []string{"a", "b"}[i]
		contract.Scopes = append(contract.Scopes, model.OperationScope{ID: model.OperationID("tempo", name, "POST /checkout"), Source: "tempo", EntryService: name, Operation: "POST /checkout", SampledRootCount: 1,
			Nodes:  []model.OperationNode{{ID: name, Kind: model.KindService}},
			Traces: []model.OperationTrace{{TraceID: id, RootSpanID: "root", RootOutcome: "unknown", SpanIDs: []string{"root"}, Spans: []model.OperationSpan{{SpanID: "root", NodeID: name}}, StructuralStatus: "linked-sample"}}})
	}
	obs := source.Observation{Operations: contract, Nodes: []source.NodeObservation{{ID: "web-alias", Kind: model.KindService, SelfReported: true}, {ID: "web", Kind: model.KindService}}}
	src := &scripted{name: "tempo", typ: "tempo", steps: []func(context.Context) (source.Observation, error){returns(obs), fails(errors.New("down"))}}
	engine := New([]source.Source{src}, Options{Aliases: map[string]string{"web-alias": "web"}}, quiet())
	engine.now = func() time.Time { return at }
	first := engine.Refresh(context.Background())
	if len(first.Operations.Scopes) != 1 || first.Operations.Scopes[0].EntryService != "web" || first.Operations.Scopes[0].SampledRootCount != 2 || len(first.Operations.Scopes[0].Nodes) != 1 {
		t.Fatalf("aliases duplicated/lost operation scopes: %+v", first.Operations)
	}
	if contract.Scopes[0].EntryService != "web-alias" || contract.Scopes[0].Traces[0].Spans[0].NodeID != "web-alias" {
		t.Fatal("engine mutated source observation")
	}
	engine.now = func() time.Time { return at.Add(time.Second) }
	second := engine.Refresh(context.Background())
	if len(second.Nodes) == 0 || len(second.Operations.Scopes) != 0 || second.Operations.Sources[0].Status != "unavailable" {
		t.Fatalf("retained topology fabricated current operation samples: %+v", second.Operations)
	}
	if !reflect.DeepEqual(first.Operations.Scopes[0].Traces[0].SpanIDs, []string{"root"}) {
		t.Fatal("refresh mutated older snapshot")
	}
}
