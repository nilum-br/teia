package engine

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/slice"
	"github.com/nilum-br/teia/internal/source"
)

type sliceSource struct {
	name, typ string
	obs       source.Observation
}

func (s sliceSource) Name() string { return s.name }
func (s sliceSource) Type() string { return s.typ }
func (s sliceSource) Discover(context.Context) (source.Observation, error) {
	return s.obs, nil
}

func dims(journey, tenant string) map[string]string {
	return map[string]string{"journey": journey, "tenant": tenant}
}

// The PoC's acceptance criteria: whatsapp and notification share the same
// app-a and app-b instances; only whatsapp calls redis.
func sharedJourneys(t *testing.T) *Engine {
	t.Helper()
	inventory := sliceSource{"kubernetes", "kubernetes", source.Observation{Nodes: []source.NodeObservation{
		{ID: "app-a", Kind: model.KindService, Labels: map[string]string{"journey.whatsapp": "true", "journey.notification": "true"}},
		{ID: "app-b", Kind: model.KindService, Labels: map[string]string{"journey.whatsapp": "true", "journey.notification": "true"}},
		{ID: "redis", Kind: model.KindCache},
		{ID: "idle", Kind: model.KindService},
	}}}
	declared := sliceSource{"declared", "declared", source.Observation{Edges: []source.EdgeObservation{
		{From: "app-a", To: "app-b", Declared: true}, {From: "app-b", To: "redis", Declared: true},
	}}}
	metrics := sliceSource{"prometheus/calls", "prometheus-query", source.Observation{Dimensions: []string{"journey", "tenant"}, Edges: []source.EdgeObservation{
		{From: "app-a", To: "app-b", Signal: source.SignalMetrics, RequestRate: 6, ErrorRate: 1.0 / 12, LatencyP95Ms: 200, Slices: []source.Slice{
			{Dims: dims("notification", "a"), RequestRate: 1, ErrorRate: 0.5, LatencyP95Ms: 240},
			{Dims: dims("whatsapp", "a"), RequestRate: 2, LatencyP95Ms: 50},
			{Dims: dims("whatsapp", "b"), RequestRate: 3, LatencyP95Ms: 45},
		}},
		{From: "app-b", To: "redis", Signal: source.SignalMetrics, RequestRate: 5, Slices: []source.Slice{
			{Dims: dims("whatsapp", "a"), RequestRate: 2, LatencyP95Ms: 2},
			{Dims: dims("whatsapp", "b"), RequestRate: 3, LatencyP95Ms: 3},
		}},
	}}}
	traces := sliceSource{"tempo", "tempo", source.Observation{Dimensions: []string{"journey", "tenant"}, Nodes: []source.NodeObservation{
		{ID: "app-a", Kind: model.KindService, SelfReported: true}, {ID: "app-b", Kind: model.KindService, SelfReported: true},
	}, Edges: []source.EdgeObservation{
		{From: "app-a", To: "app-b", Signal: source.SignalTraces, SampleCount: 3, NoErrors: true, LatencyP95Ms: 170, Slices: []source.Slice{
			{Dims: dims("notification", "a"), SampleCount: 1, NoErrors: true, LatencyP95Ms: 165},
			{Dims: dims("whatsapp", "a"), SampleCount: 2, NoErrors: true, LatencyP95Ms: 35},
		}},
		{From: "app-b", To: "redis", Signal: source.SignalTraces, SampleCount: 2, NoErrors: true, Slices: []source.Slice{
			{Dims: dims("whatsapp", "a"), SampleCount: 2, NoErrors: true, LatencyP95Ms: 2},
		}},
	}}}
	// A mesh preset without business dimensions cannot be split by journey.
	mesh := sliceSource{"prometheus/mesh", "prometheus", source.Observation{Edges: []source.EdgeObservation{
		{From: "app-a", To: "app-b", Signal: source.SignalMesh, RequestRate: 6},
		{From: "idle", To: "redis", Signal: source.SignalMesh, RequestRate: 1},
	}}}
	eng := New([]source.Source{inventory, declared, metrics, traces, mesh}, Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	eng.Refresh(context.Background())
	return eng
}

func ids(s model.Snapshot) (nodes, edges []string) {
	for _, n := range s.Nodes {
		nodes = append(nodes, n.ID)
	}
	for _, e := range s.Edges {
		edges = append(edges, e.Key())
	}
	return nodes, edges
}

func edgeOf(s model.Snapshot, key string) model.Edge {
	for _, e := range s.Edges {
		if e.Key() == key {
			return e
		}
	}
	return model.Edge{}
}

func mustParse(t *testing.T, raw string) slice.Selector {
	t.Helper()
	sel, err := slice.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return sel
}

func TestSlicesFollowCallsThroughSharedServices(t *testing.T) {
	eng := sharedJourneys(t)
	whole := eng.Snapshot()
	if got := whole.Dimensions["journey"]; !slices.Equal(got, []string{"notification", "whatsapp"}) {
		t.Fatalf("whole snapshot dimensions = %v", whole.Dimensions)
	}
	if whole.Slice != nil || edgeOf(whole, "app-a->app-b").RequestRate != 6 {
		t.Fatal("the overview must remain the whole platform with global metrics")
	}

	for _, tc := range []struct {
		selector     string
		nodes, edges []string
		rate         float64
		errorRate    float64
		health       model.Health
		traced       bool
	}{
		{"journey:whatsapp", []string{"app-a", "app-b", "redis"}, []string{"app-a->app-b", "app-b->redis"}, 5, 0, model.HealthHealthy, true},
		{"journey:notification", []string{"app-a", "app-b"}, []string{"app-a->app-b"}, 1, 0.5, model.HealthFailing, true},
		{"journey:whatsapp,tenant:b", []string{"app-a", "app-b", "redis"}, []string{"app-a->app-b", "app-b->redis"}, 3, 0, model.HealthHealthy, false},
		{"tenant=a,journey=notification", []string{"app-a", "app-b"}, []string{"app-a->app-b"}, 1, 0.5, model.HealthFailing, true},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			s := eng.Sliced(mustParse(t, tc.selector))
			nodes, edges := ids(s)
			if !slices.Equal(nodes, tc.nodes) || !slices.Equal(edges, tc.edges) {
				t.Fatalf("nodes %v edges %v, want %v %v", nodes, edges, tc.nodes, tc.edges)
			}
			ab := edgeOf(s, "app-a->app-b")
			if ab.RequestRate != tc.rate || ab.ErrorRate != tc.errorRate || ab.ErrorsUnknown || ab.Health != tc.health {
				t.Fatalf("app-a->app-b = rate %v errors %v unknown %v health %v", ab.RequestRate, ab.ErrorRate, ab.ErrorsUnknown, ab.Health)
			}
			// The trace sample never saw tenant b: it must not be credited.
			if ab.State != model.EdgeOK || slices.Contains(ab.Signals, source.SignalTraces) != tc.traced || !slices.Contains(ab.Signals, source.SignalMetrics) {
				t.Fatalf("declared, metrics and sampled traces inside the slice: %+v", ab.Signals)
			}
			if slices.Contains(ab.Signals, source.SignalMesh) {
				t.Fatal("a source that cannot split by journey must not contribute to the slice")
			}
			if s.Slice == nil || !s.Slice.Matched || s.Slice.Metrics != "scoped" ||
				(tc.traced && !slices.Equal(s.Slice.Sources, []string{"prometheus/calls", "tempo"})) || !slices.Equal(s.Slice.Excluded, []string{"prometheus/mesh"}) {
				t.Fatalf("slice scope = %+v", s.Slice)
			}
			for _, scope := range s.Journeys.Scopes {
				for _, port := range scope.BoundaryPorts {
					if port.To == "redis" && !slices.Contains(tc.nodes, "redis") {
						t.Fatalf("redis must not appear as a port of %s", tc.selector)
					}
				}
			}
		})
	}
}

func TestSliceWithoutMatchIsEmpty(t *testing.T) {
	eng := sharedJourneys(t)
	for _, sel := range []string{"journey:missing", "journey:notification,tenant:b", "region:eu"} {
		s := eng.Sliced(mustParse(t, sel))
		if len(s.Nodes) != 0 || len(s.Edges) != 0 || s.Slice == nil || s.Slice.Matched {
			t.Fatalf("%s must be empty, never the whole map: %d nodes, %+v", sel, len(s.Nodes), s.Slice)
		}
	}
	if s := eng.Sliced(mustParse(t, "region:eu")); !slices.Equal(s.Slice.Excluded, []string{"prometheus/calls", "prometheus/mesh", "tempo"}) {
		t.Fatalf("no source splits by region: %+v", s.Slice)
	}
}

func TestSliceLatencyIsTheSelectionsOwn(t *testing.T) {
	eng := sharedJourneys(t)
	ab := edgeOf(eng.Sliced(mustParse(t, "journey:notification")), "app-a->app-b")
	// Histograms (with a rate) win over sampled spans, as for whole edges.
	if ab.LatencyP95Ms != 240 || ab.LatencyApproximate {
		t.Fatalf("notification p95 = %v approx %v", ab.LatencyP95Ms, ab.LatencyApproximate)
	}
	ab = edgeOf(eng.Sliced(mustParse(t, "journey:whatsapp")), "app-a->app-b")
	if ab.LatencyP95Ms <= 45 || ab.LatencyP95Ms >= 50 || !ab.LatencyApproximate {
		t.Fatalf("two tenants' p95 combine by traffic and are marked approximate: %v %v", ab.LatencyP95Ms, ab.LatencyApproximate)
	}
}

func TestSlicedSnapshotIsCachedPerRound(t *testing.T) {
	eng := sharedJourneys(t)
	a := eng.Sliced(mustParse(t, "journey:whatsapp,tenant:a"))
	b := eng.Sliced(mustParse(t, "tenant:a,journey:whatsapp"))
	if a.Round != b.Round || len(eng.sliced) != 1 {
		t.Fatalf("equivalent selectors must share one cached slice: %d cached", len(eng.sliced))
	}
	eng.Refresh(context.Background())
	if len(eng.sliced) != 0 {
		t.Fatal("a new round must drop the old slices")
	}
}

// Declarations are not per journey: a declared flow between two services a
// slice reaches is not one of its calls. Checkout reaches app-a and app-b
// through app-c, so the declared app-a->app-b must not show as stale there.
func TestSliceLeavesOutDeclaredFlowsItsCallsNeverMade(t *testing.T) {
	declared := sliceSource{"declared", "declared", source.Observation{Edges: []source.EdgeObservation{
		{From: "app-a", To: "app-b", Declared: true}, {From: "app-b", To: "redis", Declared: true},
		{From: "app-a", To: "app-c", Declared: true}, {From: "app-c", To: "app-b", Declared: true},
		{From: "app-c", To: "redis", Declared: true},
	}}}
	call := func(from, to, journey string) source.EdgeObservation {
		return source.EdgeObservation{From: from, To: to, Signal: source.SignalMetrics, RequestRate: 1, Slices: []source.Slice{
			{Dims: dims(journey, "b"), RequestRate: 1, LatencyP95Ms: 10},
		}}
	}
	metrics := sliceSource{"prometheus/calls", "prometheus-query", source.Observation{Dimensions: []string{"journey", "tenant"}, Edges: []source.EdgeObservation{
		call("app-a", "app-b", "whatsapp"), call("app-b", "redis", "whatsapp"),
		call("app-a", "app-c", "checkout"), call("app-c", "app-b", "checkout"), call("app-c", "redis", "checkout"),
	}}}
	eng := New([]source.Source{declared, metrics}, Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	eng.Refresh(context.Background())

	s := eng.Sliced(mustParse(t, "journey:checkout"))
	nodes, edges := ids(s)
	if want := []string{"app-a->app-c", "app-c->app-b", "app-c->redis"}; !slices.Equal(edges, want) {
		t.Fatalf("checkout edges %v, want %v (nodes %v)", edges, want, nodes)
	}
	for _, e := range s.Edges {
		if !e.Declared || e.State != model.EdgeOK {
			t.Fatalf("checkout calls keep their declaration: %s declared %v state %v", e.Key(), e.Declared, e.State)
		}
	}
	for _, f := range s.Findings {
		if strings.Contains(f.Subject, "app-a->app-b") || strings.Contains(f.Subject, "app-b->redis") {
			t.Fatalf("no finding about flows the slice never made: %+v", f)
		}
	}
	if _, edges := ids(eng.Snapshot()); len(edges) != 5 {
		t.Fatalf("the whole map keeps every declared flow: %v", edges)
	}
}

// A cluster's engine says which cluster every snapshot, change and slice
// describes.
func TestClusterIsStampedOnEverything(t *testing.T) {
	metrics := &growingSource{sliceSource: sliceSource{"prometheus/calls", "prometheus-query", source.Observation{Dimensions: []string{"journey", "tenant"}, Edges: []source.EdgeObservation{
		{From: "app-a", To: "app-b", Signal: source.SignalMetrics, RequestRate: 1, Slices: []source.Slice{{Dims: dims("checkout", "b"), RequestRate: 1}}},
	}}}}
	eng := New([]source.Source{metrics}, Options{Cluster: "east"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	eng.Refresh(context.Background())
	snap := eng.Refresh(context.Background())
	if snap.Cluster != "east" || len(snap.Changes) == 0 {
		t.Fatalf("snapshot cluster %q, %d changes", snap.Cluster, len(snap.Changes))
	}
	for _, c := range eng.Changes() {
		if c.Cluster != "east" {
			t.Fatalf("change without its cluster: %+v", c)
		}
	}
	if s := eng.Sliced(mustParse(t, "journey:checkout")); s.Cluster != "east" {
		t.Fatalf("slice cluster %q", s.Cluster)
	}
	if New(nil, Options{}, slog.New(slog.NewTextHandler(io.Discard, nil))).Refresh(context.Background()).Cluster != "" {
		t.Fatal("a single scope has no cluster")
	}
}

// growingSource sees nothing in its first round and its calls afterwards,
// so the second round has changes.
type growingSource struct {
	sliceSource
	rounds int
}

func (g *growingSource) Discover(context.Context) (source.Observation, error) {
	g.rounds++
	if g.rounds == 1 {
		return source.Observation{Dimensions: g.obs.Dimensions}, nil
	}
	return g.obs, nil
}

// Run waits for its phase before the first round.
func TestRunStartsAtItsPhase(t *testing.T) {
	eng := New(nil, Options{Interval: time.Hour, Phase: 300 * time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	if r := eng.Snapshot().Round; r != 0 {
		t.Fatalf("round %d before the phase", r)
	}
	deadline := time.Now().Add(3 * time.Second)
	for eng.Snapshot().Round == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no round after the phase")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
