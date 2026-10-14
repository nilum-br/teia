package graph

import (
	"context"
	"errors"
	"testing"
	"time"

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

// An inventory knows a workload runs, not what it is: without an explicit
// kind it must not outrank flows files, explorers or the image.
func TestInventoryKindIsOnlyAnOpinionWhenExplicit(t *testing.T) {
	results := []Result{
		result("kubernetes", "kubernetes", source.Observation{Nodes: []source.NodeObservation{
			{ID: "orders-db", Kind: model.KindUnknown, Labels: map[string]string{"image": "postgres:18"}},
			{ID: "billing-db", Kind: model.KindUnknown, Labels: map[string]string{"image": "bitnami/postgresql:17"}},
			{ID: "sessions", Kind: model.KindUnknown, Labels: map[string]string{"image": "redis:8"}},
			{ID: "orders", Kind: model.KindUnknown, Labels: map[string]string{"image": "ghcr.io/acme/orders:1.2"}},
			{ID: "ledger", Kind: model.KindUnknown, Labels: map[string]string{"image": "ghcr.io/acme/ledger:3"}},
			{ID: "partner-mock", Kind: model.KindExternal}, // teia.nilum.io/kind annotation
		}}),
		result("declared", "declared", source.Observation{Nodes: []source.NodeObservation{
			{ID: "ledger", Kind: model.KindDatabase},
			{ID: "partner-mock", Kind: model.KindService},
		}}),
		result("servicegraph", "servicegraph", source.Observation{Nodes: []source.NodeObservation{
			{ID: "orders-db", Kind: model.KindDatabase},
		}}),
	}
	g := Build(results, nil, nil)
	ApplyTech(g)
	want := map[string]model.NodeKind{
		"orders-db":    model.KindDatabase, // traffic says so, nobody says otherwise
		"billing-db":   model.KindDatabase, // from the image
		"sessions":     model.KindCache,    // from the image
		"orders":       model.KindUnknown,  // still a workload for the advisor
		"ledger":       model.KindDatabase, // the flows file decides
		"partner-mock": model.KindExternal, // an explicit annotation beats a flows file
	}
	for id, w := range want {
		if got := g.Nodes[id].Kind; got != w {
			t.Errorf("%s: kind %s, want %s", id, got, w)
		}
	}
}

func TestMarkOutsideTrustsOnlyCompleteInventories(t *testing.T) {
	traffic := result("servicegraph", "servicegraph", source.Observation{
		Nodes: []source.NodeObservation{{ID: "orders", SelfReported: true}},
		Edges: []source.EdgeObservation{
			{From: "orders", To: "stripe"},
			{From: "orders", To: "orders-db"},
		},
	})
	inventory := func(obs source.Observation, err error) Result {
		r := result("kubernetes", "kubernetes", obs)
		r.Err = err
		return r
	}
	listed := source.Observation{Nodes: []source.NodeObservation{{ID: "orders"}, {ID: "pg-main"}}}
	cases := []struct {
		name      string
		inventory Result
		external  bool // whether stripe becomes external
	}{
		{"complete", inventory(listed, nil), true},
		{"failed", inventory(source.Observation{}, errors.New("connection refused")), false},
		{"degraded", inventory(source.Observation{Degraded: true}, nil), false},
		{"empty", inventory(source.Observation{}, nil), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := Build([]Result{traffic, c.inventory}, map[string]string{"pg-main": "orders-db"}, nil)
			if got := g.Nodes["stripe"].Kind == model.KindExternal; got != c.external {
				t.Errorf("stripe external = %v, want %v", got, c.external)
			}
			if g.Nodes["orders-db"].Kind == model.KindExternal {
				t.Error("an inventoried node known under an alias is not external")
			}
		})
	}
}

func TestIDsAreCaseInsensitive(t *testing.T) {
	results := []Result{
		result("declared", "declared", source.Observation{Edges: []source.EdgeObservation{
			{From: "orderservice", To: "payments", Declared: true},
		}}),
		result("servicegraph", "servicegraph", source.Observation{
			Nodes: []source.NodeObservation{{ID: "OrderService", SelfReported: true}},
			Edges: []source.EdgeObservation{
				{From: "OrderService", To: "Payments"},
				{From: "OrderService", To: "PG-Main"},
				{From: "OrderService", To: "LoadGen"},
				{From: source.AddressID("FE80::1"), To: "OrderService"},
			},
		}),
	}
	g := Build(results, map[string]string{"pg-main": "Orders-DB"}, []string{"LOADGEN"})
	want := []string{"orderservice->payments", "orderservice->orders-db", "ip:FE80::1->orderservice"}
	if len(g.Edges) != len(want) {
		t.Fatalf("got edges %v, want %v", g.SortedEdges(), want)
	}
	for _, key := range want {
		if _, ok := g.Edges[key]; !ok {
			t.Errorf("missing edge %s in %v", key, g.SortedEdges())
		}
	}
	if e := g.Edges["orderservice->payments"]; e != nil && e.State != model.EdgeOK {
		t.Errorf("declared and observed edge should be ok, got %s", e.State)
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

func TestRecycledAddressesAreNotMisattributed(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	results := []Result{
		result("docker", "docker", source.Observation{Nodes: []source.NodeObservation{
			{ID: "old-timer", Addresses: []string{"10.0.0.7"}, Since: now.Add(-time.Hour)},
			{ID: "newcomer", Addresses: []string{"10.0.0.8"}, Since: now.Add(-30 * time.Second)},
		}}),
		result("loki/access-logs", "loki-query", source.Observation{Edges: []source.EdgeObservation{
			{From: source.AddressID("10.0.0.7"), To: "catalog", Window: 2 * time.Minute},
			{From: source.AddressID("10.0.0.8"), To: "catalog", Window: 2 * time.Minute},
		}}),
		result("postgres/db", "postgres", source.Observation{Edges: []source.EdgeObservation{
			{From: source.AddressID("10.0.0.8"), To: "db"}, // current sessions: no window
		}}),
	}
	g := BuildAt(results, nil, nil, now)
	if _, ok := g.Edges["old-timer->catalog"]; !ok {
		t.Error("an address held for the whole window should resolve")
	}
	if _, ok := g.Edges["newcomer->catalog"]; ok {
		t.Error("an address that changed hands inside the window must not be attributed")
	}
	if _, ok := g.Edges["newcomer->db"]; !ok {
		t.Error("a point-in-time observation resolves to the current owner")
	}
}

// Inventories report one observation per pod, so a rollout only makes the
// new pod's address ambiguous, not every address of the workload.
func TestAddressesAreTimedPerObservation(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	results := []Result{
		result("kubernetes", "kubernetes", source.Observation{Nodes: []source.NodeObservation{
			{ID: "orders", Addresses: []string{"10.0.0.7"}, Since: now.Add(-time.Hour)},
			{ID: "orders", Addresses: []string{"10.0.0.8"}, Since: now.Add(-30 * time.Second)},
			{ID: "catalog", Addresses: []string{"10.0.0.9"}, Since: now.Add(-time.Hour)},
		}}),
		result("loki/access-logs", "loki-query", source.Observation{Edges: []source.EdgeObservation{
			{From: source.AddressID("10.0.0.7"), To: source.AddressID("10.0.0.9"), Window: 2 * time.Minute},
		}}),
	}
	g := BuildAt(results, nil, nil, now)
	if _, ok := g.Edges["orders->catalog"]; !ok {
		t.Errorf("the older pod held its address for the whole window: %v", g.SortedEdges())
	}
	if got := g.Nodes["orders"].Addresses; len(got) != 2 {
		t.Errorf("orders addresses = %v, want both pods'", got)
	}
}
