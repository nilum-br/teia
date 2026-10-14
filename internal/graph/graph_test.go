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

func TestLatencyFromTheBusiestMeasurement(t *testing.T) {
	sampled := source.Observation{Edges: []source.EdgeObservation{
		{From: "web", To: "orders", Signal: source.SignalTraces, LatencyP50Ms: 50, LatencyP95Ms: 60},
	}}
	histogram := source.Observation{Edges: []source.EdgeObservation{
		{From: "web", To: "orders", Signal: source.SignalTraces, RequestRate: 5, LatencyP50Ms: 100, LatencyP95Ms: 200},
	}}
	for _, order := range [][]Result{
		{result("tempo", "tempo", sampled), result("prometheus", "prometheus", histogram)},
		{result("prometheus", "prometheus", histogram), result("tempo", "tempo", sampled)},
	} {
		e := Build(order, nil, nil).Edges["web->orders"]
		if e.LatencyP50Ms != 100 || e.LatencyP95Ms != 200 {
			t.Errorf("histograms (with a rate) should win over sampled spans, got p50 %v p95 %v", e.LatencyP50Ms, e.LatencyP95Ms)
		}
	}
}

func TestNodeLatencyIsWhatItsCallersWaitFor(t *testing.T) {
	g := Build([]Result{result("prometheus", "prometheus", source.Observation{Edges: []source.EdgeObservation{
		{From: "web", To: "orders", RequestRate: 3, LatencyP50Ms: 40, LatencyP95Ms: 100},
		{From: "batch", To: "orders", RequestRate: 1, LatencyP50Ms: 80, LatencyP95Ms: 300},
		{From: "orders", To: "payments", RequestRate: 2, LatencyP50Ms: 10, LatencyP95Ms: 20},
		{From: "orders", To: "audit", RequestRate: 1}, // no durations measured
	}})}, nil, nil)
	ApplyHealth(g, DefaultHealth)

	// (3*100 + 1*300) / 4 for the calls orders serves; its own calls do not count.
	if n := g.Nodes["orders"]; n.LatencyP95Ms != 150 || n.LatencyP50Ms != 50 {
		t.Errorf("orders p50/p95 = %v/%v, want 50/150", n.LatencyP50Ms, n.LatencyP95Ms)
	}
	if n := g.Nodes["web"]; n.LatencyP95Ms != 0 {
		t.Errorf("web serves nothing, so it has no latency, got %v", n.LatencyP95Ms)
	}
	if n := g.Nodes["audit"]; n.LatencyP95Ms != 0 {
		t.Errorf("nobody measured audit's calls, got %v", n.LatencyP95Ms)
	}
}

func TestJudgeSaysWhy(t *testing.T) {
	cases := []struct {
		name               string
		rate, errs, p95    float64
		health             model.Health
		causes             []model.Cause
		firstSummaryPrefix string
	}{
		{"nothing measured", 0, 0, 0, model.HealthUnknown, nil, ""},
		{"fine", 10, 0.001, 120, model.HealthHealthy, nil, ""},
		{"slow", 10, 0, 800, model.HealthDegraded, []model.Cause{model.CauseLatency}, "p95 800 ms, over 500 ms"},
		{"failing errors beat degraded latency", 10, 0.07, 900, model.HealthFailing, []model.Cause{model.CauseErrors, model.CauseLatency}, "7.0% errors, over 5.0%"},
		{"latency alone, from sampled spans without a rate", 0, 0, 2500, model.HealthFailing, []model.Cause{model.CauseLatency}, "p95 2.5 s, over 2.0 s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, reasons := DefaultHealth.Judge(c.rate, c.errs, c.p95, nil)
			if h != c.health {
				t.Errorf("health %s, want %s", h, c.health)
			}
			var causes []model.Cause
			for _, r := range reasons {
				causes = append(causes, r.Cause)
			}
			if len(causes) != len(c.causes) {
				t.Fatalf("causes %v, want %v", causes, c.causes)
			}
			for i := range causes {
				if causes[i] != c.causes[i] {
					t.Errorf("causes %v, want %v (worst first)", causes, c.causes)
				}
			}
			if c.firstSummaryPrefix != "" && reasons[0].Summary != c.firstSummaryPrefix {
				t.Errorf("summary %q, want %q", reasons[0].Summary, c.firstSummaryPrefix)
			}
		})
	}
}

func TestSlowCalleeIsDegradedWithItsReason(t *testing.T) {
	g := Build([]Result{result("prometheus", "prometheus", source.Observation{Edges: []source.EdgeObservation{
		{From: "recommendations", To: "inventory", RequestRate: 2, LatencyP50Ms: 40, LatencyP95Ms: 850},
	}})}, nil, nil)
	ApplyHealth(g, DefaultHealth)
	e, n := g.Edges["recommendations->inventory"], g.Nodes["inventory"]
	if e.Health != model.HealthDegraded || len(e.Reasons) != 1 || e.Reasons[0].Cause != model.CauseLatency {
		t.Errorf("edge %s %+v, want degraded by latency", e.Health, e.Reasons)
	}
	if n.Health != model.HealthDegraded || len(n.Reasons) != 1 || n.Reasons[0].Cause != model.CauseLatency {
		t.Errorf("node %s %+v, want degraded by latency", n.Health, n.Reasons)
	}
	if r := g.Nodes["recommendations"]; r.Health != model.HealthHealthy || len(r.Reasons) != 0 {
		t.Errorf("a caller is not slow because its callee is: %s %+v", r.Health, r.Reasons)
	}
}

func TestReasonsPointAtTheOrigin(t *testing.T) {
	g := Build([]Result{result("prometheus", "prometheus", source.Observation{Edges: []source.EdgeObservation{
		{From: "web", To: "recommendations", RequestRate: 4, LatencyP95Ms: 950},
		{From: "recommendations", To: "inventory", RequestRate: 4, LatencyP95Ms: 900},
		{From: "recommendations", To: "cache", RequestRate: 4, LatencyP95Ms: 3},
		{From: "web", To: "orders", RequestRate: 2, LatencyP95Ms: 40},
	}})}, nil, nil)
	ApplyHealth(g, DefaultHealth)

	rec := g.Nodes["recommendations"]
	if len(rec.Reasons) != 1 || rec.Reasons[0].Via != "inventory" || rec.Reasons[0].Summary != "p95 950 ms, over 500 ms, from inventory" {
		t.Errorf("recommendations should be slow because of inventory: %+v", rec.Reasons)
	}
	if inv := g.Nodes["inventory"]; len(inv.Reasons) != 1 || inv.Reasons[0].Via != "" {
		t.Errorf("inventory is the origin, it has no via: %+v", inv.Reasons)
	}
	if e := g.Edges["web->recommendations"]; len(e.Reasons) != 1 || e.Reasons[0].Via != "inventory" {
		t.Errorf("web waits on inventory through recommendations: %+v", e.Reasons)
	}
	if e := g.Edges["recommendations->inventory"]; len(e.Reasons) != 1 || e.Reasons[0].Via != "" {
		t.Errorf("the slow call itself names no further origin: %+v", e.Reasons)
	}
}

func TestMeasuresBecomeReasons(t *testing.T) {
	g := Build([]Result{
		result("docker", "docker", source.Observation{Nodes: []source.NodeObservation{
			{ID: "legacy-reports", Kind: model.KindService, Addresses: []string{"10.0.0.9"}},
		}}),
		result("postgres/orders-db", "postgres", source.Observation{
			Nodes: []source.NodeObservation{{ID: "orders-db", Kind: model.KindDatabase,
				Measures: []model.Measure{{Cause: model.CauseSaturation, What: "connections", Value: 92, Max: 97}}}},
			Edges: []source.EdgeObservation{{From: "orders", To: "orders-db", Signal: source.SignalDatabase,
				Measures: []model.Measure{{Cause: model.CauseLocks, What: "sessions waiting on locks", Value: 3, Via: source.AddressID("10.0.0.9")}}}},
		}),
	}, nil, nil)
	ApplyHealth(g, DefaultHealth)

	db := g.Nodes["orders-db"]
	if db.Health != model.HealthDegraded || len(db.Reasons) != 1 || db.Reasons[0].Cause != model.CauseSaturation {
		t.Fatalf("orders-db: %s %+v", db.Health, db.Reasons)
	}
	if got := db.Reasons[0].Summary; got != "94.8% of connections used (92 of 97), over 80.0%" {
		t.Errorf("summary %q", got)
	}
	e := g.Edges["orders->orders-db"]
	if e.Health != model.HealthDegraded || len(e.Reasons) != 1 || e.Reasons[0].Via != "legacy-reports" {
		t.Fatalf("orders->orders-db: %s %+v", e.Health, e.Reasons)
	}
	if got := e.Reasons[0].Summary; got != "3 sessions waiting on locks, over 1, held by legacy-reports" {
		t.Errorf("summary %q", got)
	}
}

func TestRememberedEdgesForgetMomentaryMeasures(t *testing.T) {
	m := &Memory{Retention: time.Minute}
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	first := Build([]Result{result("pg", "postgres", source.Observation{Edges: []source.EdgeObservation{
		{From: "orders", To: "orders-db", Measures: []model.Measure{{Cause: model.CauseLocks, What: "sessions waiting on locks", Value: 3}}},
	}})}, nil, nil)
	m.Retain(first, now)
	later := Build(nil, nil, nil)
	m.Retain(later, now.Add(10*time.Second))
	if e := later.Edges["orders->orders-db"]; e == nil || len(e.Measures) != 0 {
		t.Errorf("a remembered edge keeps its traffic, not the locks of ten seconds ago: %+v", e)
	}
}
