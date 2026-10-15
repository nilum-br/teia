package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

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
