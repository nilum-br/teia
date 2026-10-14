package graph

import (
	"context"
	"testing"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

type fake struct{ name, typ string }

func (f fake) Name() string { return f.name }
func (f fake) Type() string { return f.typ }
func (f fake) Discover(context.Context) (source.Observation, error) {
	return source.Observation{}, nil
}

func result(name, typ string, obs source.Observation) Result {
	return Result{Source: fake{name, typ}, Observation: obs}
}

func TestBuildMergesIntentTrafficAndInventory(t *testing.T) {
	results := []Result{
		result("declared", "declared", source.Observation{
			Nodes: []source.NodeObservation{{ID: "orders", Kind: model.KindService}},
			Edges: []source.EdgeObservation{
				{From: "orders", To: "payments", Declared: true, Reason: "charge"},
				{From: "orders", To: "notifications", Declared: true},
			},
		}),
		result("servicegraph", "servicegraph", source.Observation{
			Nodes: []source.NodeObservation{
				{ID: "orders", SelfReported: true},
				{ID: "payments", SelfReported: true},
				{ID: "orders-db", Kind: model.KindDatabase},
			},
			Edges: []source.EdgeObservation{
				{From: "orders", To: "payments", RequestRate: 2, ErrorRate: 0.1},
				{From: "orders", To: "orders-db", Protocol: "postgresql", RequestRate: 2},
			},
		}),
		result("docker", "docker", source.Observation{
			Nodes: []source.NodeObservation{
				{ID: "legacy", Addresses: []string{"10.0.0.9"}},
				{ID: "orders-db", Kind: model.KindDatabase, Addresses: []string{"10.0.0.5"}},
				{ID: "prometheus", Ignore: true},
			},
		}),
		result("postgres/orders-db", "postgres", source.Observation{
			Edges: []source.EdgeObservation{{From: source.AddressID("10.0.0.9"), To: "orders-db", Protocol: "postgresql"}},
		}),
	}

	g := Build(results, nil, nil)

	wantState := map[string]model.EdgeState{
		"orders->payments":      model.EdgeOK,
		"orders->notifications": model.EdgeStale,
		"orders->orders-db":     model.EdgeGhost,
		"legacy->orders-db":     model.EdgeGhost,
	}
	if len(g.Edges) != len(wantState) {
		t.Fatalf("got %d edges, want %d: %v", len(g.Edges), len(wantState), g.SortedEdges())
	}
	for key, want := range wantState {
		e, ok := g.Edges[key]
		if !ok {
			t.Fatalf("missing edge %s", key)
		}
		if e.State != want {
			t.Errorf("%s: state %s, want %s", key, e.State, want)
		}
	}
	if e := g.Edges["orders->payments"]; e.Reason != "charge" || e.RequestRate != 2 || e.ErrorRate != 0.1 {
		t.Errorf("orders->payments lost data on merge: %+v", e)
	}

	wantVis := map[string]model.Visibility{
		"orders":        model.VisibilityFull,
		"payments":      model.VisibilityFull,
		"orders-db":     model.VisibilityPartial,
		"legacy":        model.VisibilityPartial,
		"notifications": model.VisibilityNone,
	}
	for id, want := range wantVis {
		if got := g.Nodes[id].Visibility; got != want {
			t.Errorf("%s: visibility %s, want %s", id, got, want)
		}
	}
	if _, ok := g.Nodes["prometheus"]; ok {
		t.Error("ignored node made it into the graph")
	}
	if _, ok := g.Nodes[source.AddressID("10.0.0.9")]; ok {
		t.Error("address was not resolved through the inventory")
	}
}

func TestBuildKindPriorityAndAliases(t *testing.T) {
	results := []Result{
		result("servicegraph", "servicegraph", source.Observation{
			Nodes: []source.NodeObservation{{ID: "pg-main", Kind: model.KindDatabase}},
		}),
		result("docker", "docker", source.Observation{
			Nodes: []source.NodeObservation{{ID: "gateway", Kind: model.KindExternal}},
		}),
		result("servicegraph2", "servicegraph", source.Observation{
			Nodes: []source.NodeObservation{{ID: "gateway", Kind: model.KindService}},
		}),
	}
	g := Build(results, map[string]string{"pg-main": "orders-db"}, []string{"loadgen"})
	if _, ok := g.Nodes["orders-db"]; !ok {
		t.Fatal("alias was not applied")
	}
	if got := g.Nodes["gateway"].Kind; got != model.KindExternal {
		t.Errorf("inventory label should win over traffic guess, got %s", got)
	}
}

func TestCoverage(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*model.Node{
			"a":   {ID: "a", Kind: model.KindService, Visibility: model.VisibilityFull},
			"b":   {ID: "b", Kind: model.KindService, Visibility: model.VisibilityPartial},
			"ext": {ID: "ext", Kind: model.KindExternal, Visibility: model.VisibilityPartial},
		},
		Edges: map[string]*model.Edge{
			"a->b":   {From: "a", To: "b", Declared: true, Observed: true},
			"a->ext": {From: "a", To: "ext", Observed: true},
			"b->a":   {From: "b", To: "a", Declared: true},
		},
	}
	c := ComputeCoverage(g)
	if c.Visibility != 0.5 {
		t.Errorf("visibility %v, want 0.5 (externals excluded)", c.Visibility)
	}
	if c.Declared != 0.5 || c.Observed != 0.5 {
		t.Errorf("declared %v observed %v, want 0.5 and 0.5", c.Declared, c.Observed)
	}
}
