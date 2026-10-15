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
func TestSampleTimesPreventAttributingOldCallsToRecycledAddresses(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, callerAddress := range []bool{false, true} {
		from, to := "orders", source.AddressID("10.0.0.7")
		if callerAddress {
			from, to = to, from
		}
		for _, age := range []time.Duration{time.Hour, time.Minute} {
			g := BuildAt([]Result{
				result("inventory", "docker", source.Observation{Nodes: []source.NodeObservation{{
					ID: "new-owner", Addresses: []string{"10.0.0.7"}, Since: now.Add(-30 * time.Minute),
				}}}),
				result("tempo", "tempo", source.Observation{Edges: []source.EdgeObservation{{
					From: from, To: to, Window: 5 * time.Minute, SampleCount: 2,
					ObservedFrom: now.Add(-age), ObservedAt: now.Add(-time.Second),
				}}}),
			}, nil, nil, now)
			want := 1
			if age == time.Hour {
				want = 0
			}
			if len(g.Edges) != want {
				t.Fatalf("callerAddress=%v age=%v: got %v; old sample must not belong to current IP owner", callerAddress, age, g.Edges)
			}
		}
	}
}

func TestAddressWindowDoesNotAdvanceDuringCollection(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                 string
		evaluated, collected time.Time
		want                 int
	}{
		{"evaluation precedes completion", now.Add(-30 * time.Second), now, 1},
		{"completion precedes assembly", time.Time{}, now.Add(-30 * time.Second), 1},
		{"undated fallback", time.Time{}, time.Time{}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			traffic := result("metrics", "prometheus", source.Observation{
				EvaluatedAt: tc.evaluated,
				Edges: []source.EdgeObservation{
					{From: source.AddressID("10.0.0.7"), To: "catalog", Window: 2 * time.Minute},
					{From: source.AddressID("10.0.0.8"), To: "catalog", Window: 2 * time.Minute},
				},
			})
			traffic.CollectedAt = tc.collected
			g := BuildAt([]Result{
				result("inventory", "docker", source.Observation{Nodes: []source.NodeObservation{
					{ID: "new-owner", Addresses: []string{"10.0.0.7"}, Since: now.Add(-130 * time.Second)},
					{ID: "old-owner", Addresses: []string{"10.0.0.8"}, Since: now.Add(-time.Hour)},
				}}), traffic,
			}, nil, nil, now)
			if len(g.Edges) != tc.want || g.Edges["old-owner->catalog"] == nil {
				t.Fatalf("collection delay must not move a query window past address reassignment: %v", g.SortedEdges())
			}
		})
	}
}

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
			h, reasons := DefaultHealth.Judge(Signals{RequestRate: c.rate, ErrorRate: c.errs, LatencyP95Ms: c.p95})
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
		t.Errorf("a remembered edge keeps its relationship, not the locks of ten seconds ago: %+v", e)
	}
}

func TestRetainedEvidenceKeepsTimeAndDoesNotRenewRetention(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	r := result("metrics", "prometheus", source.Observation{Edges: []source.EdgeObservation{{
		From: "web", To: "orders", Signal: source.SignalTraces, RequestRate: 10,
		ObservedAt: now.Add(-time.Hour), Window: 5 * time.Minute,
	}}})
	r.CollectedAt = now
	first := BuildAt([]Result{r}, nil, nil, now)
	ev := first.Edges["web->orders"].Evidence[0]
	if !ev.ObservedAt.Equal(now.Add(-time.Hour)) || ev.WindowSeconds != 300 || !ev.CollectedAt.Equal(now) {
		t.Fatalf("collection must not replace original sample time: %+v", ev)
	}
	m := &Memory{Retention: time.Minute}
	m.Retain(first, now)
	later := Build(nil, nil, nil)
	m.Retain(later, now.Add(50*time.Second))
	e := later.Edges["web->orders"]
	if e == nil || !e.Evidence[0].Retained || !e.Evidence[0].CollectedAt.Equal(now) || e.RequestRate != 0 {
		t.Fatalf("retained evidence = %+v", e)
	}
	// Reusing retained topology must not reset the last genuinely observed time.
	// A current capacity/backlog reading on that edge does not observe calls.
	e.Evidence = append(e.Evidence, model.Evidence{Source: "exporter", CollectedAt: now.Add(55 * time.Second), Metrics: []string{"backlog"}})
	m.Retain(later, now.Add(55*time.Second))
	expired := Build(nil, nil, nil)
	m.Retain(expired, now.Add(61*time.Second))
	if len(expired.Edges) != 0 {
		t.Fatal("retained topology renewed its own lifetime")
	}
}

func TestReadingsKeepTheirOwnSampleTimeAndOrigin(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	r := result("metrics", "prometheus", source.Observation{
		EvaluatedAt: now.Add(-time.Second),
		Nodes:       []source.NodeObservation{{ID: "cache"}},
		Edges:       []source.EdgeObservation{{From: "web", To: "cache", RequestRate: 5}},
		Readings: []source.Reading{
			{Candidates: []string{"cache"}, OldestSampleAt: now.Add(-time.Hour), Origin: "redis (instance cache:9121)",
				Measures: []model.Measure{{Cause: model.CauseSaturation, What: "memory", Value: 48, Max: 64}}},
			{Candidates: []string{"cache"}, OldestSampleAt: now.Add(-time.Minute), Origin: "redis (instance cache:9121)",
				Measures: []model.Measure{{Cause: model.CauseSaturation, What: "clients", Value: 10, Max: 100}}},
			{Candidates: []string{"web"}, To: []string{"cache"}, Window: 5 * time.Minute,
				Measures: []model.Measure{{Cause: model.CauseLag, What: "behind", Value: 20}}},
		},
	})
	r.CollectedAt = now
	g := BuildAt([]Result{r}, nil, nil, now)
	entries := g.Nodes["cache"].Evidence
	if len(entries) != 3 || !entries[0].OldestSampleAt.IsZero() || !entries[1].OldestSampleAt.Equal(now.Add(-time.Hour)) ||
		!entries[2].OldestSampleAt.Equal(now.Add(-time.Minute)) || entries[1].Origin != "redis (instance cache:9121)" {
		t.Fatalf("per-reading sample times/origin were lost: %+v", entries)
	}
	for _, ev := range entries {
		if !ev.EvaluatedAt.Equal(r.Observation.EvaluatedAt) || !ev.CollectedAt.Equal(now) || !ev.ObservedAt.IsZero() {
			t.Fatalf("sample, evaluation and collection times were mixed: %+v", ev)
		}
	}
	edge := g.Edges["web->cache"].Evidence
	if len(edge) != 2 || !edge[0].OldestSampleAt.IsZero() || !edge[1].OldestSampleAt.IsZero() || edge[1].WindowSeconds != 300 {
		t.Fatalf("a gauge timestamp must not date traffic or derived measures: %+v", edge)
	}
}

func TestReadingsGoToTheNodeTheyMeasure(t *testing.T) {
	g := Build([]Result{
		result("docker", "docker", source.Observation{Nodes: []source.NodeObservation{
			{ID: "catalog-cache", Kind: model.KindCache, Addresses: []string{"10.0.0.4"}},
			{ID: "catalog-cache-exporter", Kind: model.KindService},
			{ID: "noise", Kind: model.KindService, Ignore: true},
		}}),
		result("traces", "prometheus", source.Observation{Edges: []source.EdgeObservation{
			{From: "email-worker", To: "order-events", Signal: source.SignalTraces},
		}}),
		result("exporters", "prometheus", source.Observation{Readings: []source.Reading{
			{Candidates: []string{"catalog-cache", "catalog-cache-exporter"}, Measures: []model.Measure{{Cause: model.CauseSaturation, What: "memory", Value: 48, Max: 64}}},
			{Candidates: []string{"sessions", source.AddressID("10.0.0.4")}, Measures: []model.Measure{{Cause: model.CauseSaturation, What: "clients", Value: 10, Max: 100}}},
			{Candidates: []string{"email-worker"}, To: []string{"order-events"}, Measures: []model.Measure{{Cause: model.CauseBacklog, What: "messages waiting", Value: 45210}}},
			{Candidates: []string{"noise"}, Measures: []model.Measure{{Cause: model.CauseRestarts, What: "restarts", Value: 9}}, Origin: "restarts (pod noise-0)"},
			{Candidates: []string{"billing"}, To: []string{"invoices"}, Measures: []model.Measure{{Cause: model.CauseBacklog, What: "messages waiting", Value: 3}}, Origin: "kafka (consumergroup billing)"},
		}}),
	}, nil, nil)

	cache := g.Nodes["catalog-cache"]
	if len(cache.Measures) != 2 {
		t.Errorf("catalog-cache should get memory (by name) and clients (by address): %+v", cache.Measures)
	}
	if exp := g.Nodes["catalog-cache-exporter"]; len(exp.Measures) != 0 {
		t.Errorf("the exporter is not what it measures: %+v", exp.Measures)
	}
	if e := g.Edges["email-worker->order-events"]; len(e.Measures) != 1 || e.Measures[0].Value != 45210 {
		t.Errorf("consumer lag belongs on the line to its topic: %+v", e.Measures)
	}
	if _, ok := g.Nodes["billing"]; ok {
		t.Error("a reading never creates a node")
	}
	if len(g.Unmatched) != 2 {
		t.Fatalf("unmatched: %+v", g.Unmatched)
	}
}

func TestSaturationSummariesSpeakTheirUnit(t *testing.T) {
	_, memory := DefaultHealth.Judge(Signals{Measures: []model.Measure{{Cause: model.CauseSaturation, What: "memory", Value: 60 << 20, Max: 64 << 20, Unit: model.UnitBytes}}})
	if len(memory) != 1 || memory[0].Summary != "93.8% of memory used (60.0 MiB of 64.0 MiB), over 80.0%" {
		t.Errorf("memory: %+v", memory)
	}
	_, cpu := DefaultHealth.Judge(Signals{Measures: []model.Measure{{Cause: model.CauseSaturation, What: "CPU time throttled", Value: 0.97, Max: 1, Unit: model.UnitShare}}})
	if len(cpu) != 1 || cpu[0].Summary != "97.0% CPU time throttled, over 95.0%" || cpu[0].Health != model.HealthFailing {
		t.Errorf("cpu: %+v", cpu)
	}
	_, lag := DefaultHealth.Judge(Signals{Measures: []model.Measure{{Cause: model.CauseLag, What: "behind", Value: 720, Unit: model.UnitSeconds}}})
	if len(lag) != 1 || lag[0].Summary != "12 min behind, over 5 min" || lag[0].Health != model.HealthFailing {
		t.Errorf("lag: %+v", lag)
	}
}

func TestUnmeasuredFailuresAreNotHealthy(t *testing.T) {
	g := Build([]Result{
		result("custom", "prometheus-query", source.Observation{Edges: []source.EdgeObservation{
			{From: "app-a", To: "app-b", RequestRate: 10, NoErrors: true},
			{From: "web", To: "orders", RequestRate: 4, NoErrors: true},
		}}),
		result("traces", "prometheus", source.Observation{Edges: []source.EdgeObservation{
			{From: "web", To: "orders", RequestRate: 2, ErrorRate: 0.5},
		}}),
	}, nil, nil)
	ApplyHealth(g, DefaultHealth)

	ab := g.Edges["app-a->app-b"]
	if !ab.ErrorsUnknown || ab.Health != model.HealthUnknown {
		t.Errorf("traffic without failures measured is unknown, not healthy: %+v", ab)
	}
	if b := g.Nodes["app-b"]; !b.ErrorsUnknown || b.Health != model.HealthUnknown {
		t.Errorf("app-b: %s errorsUnknown=%v", b.Health, b.ErrorsUnknown)
	}
	wo := g.Edges["web->orders"]
	if wo.ErrorsUnknown || wo.RequestRate != 4 || wo.ErrorRate != 0.5 || wo.Health != model.HealthFailing {
		t.Errorf("the busiest rate, and failures from the source that measured them: %+v", wo)
	}
}
