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

// A slice keeps the calls that carried its dimensions; an operation follows
// whole traces, so a slice must not present every journey's operations as
// its own.
func TestSlicesLeaveOutSampledOperations(t *testing.T) {
	contract := model.EmptyOperations()
	contract.Sources = []model.OperationSource{{Source: "tempo", Status: "sampled"}}
	contract.Scopes = []model.OperationScope{{ID: model.OperationID("tempo", "web", "GET /browse"), Source: "tempo", EntryService: "web", Operation: "GET /browse", SampledRootCount: 1,
		Nodes: []model.OperationNode{{ID: "web", Kind: model.KindService}, {ID: "catalog", Kind: model.KindService}},
		Edges: []model.OperationEdge{{From: "web", To: "catalog", TraceIDs: []string{"a"}}}}}
	traces := sliceSource{"tempo", "tempo", source.Observation{Dimensions: []string{"journey"}, Operations: contract, Edges: []source.EdgeObservation{
		{From: "web", To: "orders", Signal: source.SignalTraces, SampleCount: 1, NoErrors: true, Slices: []source.Slice{{Dims: map[string]string{"journey": "checkout"}, SampleCount: 1, NoErrors: true}}},
		{From: "web", To: "catalog", Signal: source.SignalTraces, SampleCount: 1, NoErrors: true, Slices: []source.Slice{{Dims: map[string]string{"journey": "browse"}, SampleCount: 1, NoErrors: true}}},
	}}}
	eng := New([]source.Source{traces}, Options{}, quiet())
	if whole := eng.Refresh(context.Background()); whole.Operations == nil || len(whole.Operations.Scopes) != 1 {
		t.Fatalf("whole round operations = %+v", whole.Operations)
	}
	sliced := eng.Sliced(slice.Selector{"journey": "checkout"})
	if sliced.Slice == nil || !sliced.Slice.Matched || len(sliced.Edges) != 1 {
		t.Fatalf("slice = %+v, edges = %+v", sliced.Slice, sliced.Edges)
	}
	if sliced.Operations != nil {
		t.Fatalf("the checkout slice carried the browse operation: %+v", sliced.Operations)
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
