package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
	"github.com/nilum-br/teia/internal/source/tempo"
)

// scripted is a source that plays one step per round and repeats the last.
type scripted struct {
	name, typ string
	calls     atomic.Int32
	steps     []func(context.Context) (source.Observation, error)
}

func (s *scripted) Name() string { return s.name }
func (s *scripted) Type() string { return s.typ }
func (s *scripted) Discover(ctx context.Context) (source.Observation, error) {
	i := int(s.calls.Add(1)) - 1
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	return s.steps[i](ctx)
}

func returns(obs source.Observation) func(context.Context) (source.Observation, error) {
	return func(context.Context) (source.Observation, error) { return obs, nil }
}

func fails(err error) func(context.Context) (source.Observation, error) {
	return func(context.Context) (source.Observation, error) { return source.Observation{}, err }
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func statusOf(t *testing.T, s model.Snapshot, name string) model.SourceStatus {
	t.Helper()
	for _, st := range s.Sources {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("no status for %s in %+v", name, s.Sources)
	return model.SourceStatus{}
}

func edgeState(s model.Snapshot, key string) model.EdgeState {
	for _, e := range s.Edges {
		if e.Key() == key {
			return e.State
		}
	}
	return ""
}

func TestRefreshReusesLastGoodObservation(t *testing.T) {
	intent := source.Observation{Edges: []source.EdgeObservation{{From: "orders", To: "payments", Declared: true}}}
	traffic := source.Observation{Edges: []source.EdgeObservation{{From: "orders", To: "payments", Signal: source.SignalTraces, RequestRate: 1}}}
	declared := &scripted{name: "declared", typ: "declared", steps: []func(context.Context) (source.Observation, error){
		returns(intent), fails(errors.New("bad flows file")), fails(errors.New("bad flows file")), returns(intent),
	}}
	observed := &scripted{name: "traces", typ: "tempo", steps: []func(context.Context) (source.Observation, error){returns(traffic)}}

	e := New([]source.Source{declared, observed}, Options{Interval: 10 * time.Second, Retention: 30 * time.Second}, quiet())
	clock := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	e.now = func() time.Time { return clock }
	ctx := context.Background()

	tests := []struct {
		name    string
		advance time.Duration
		state   model.EdgeState
		source  model.SourceState
		message string
		// noEdgeChanges: a failed source must not flood with edge changes.
		noEdgeChanges bool
	}{
		{name: "both sources work", state: model.EdgeOK, source: model.SourceOK},
		{name: "intent fails within the grace period", advance: 10 * time.Second, state: model.EdgeOK, source: model.SourceError,
			message: "failed: bad flows file; using the last good observation from 10s ago", noEdgeChanges: true},
		{name: "intent still fails after the grace period", advance: 40 * time.Second, state: model.EdgeGhost, source: model.SourceError,
			message: "bad flows file"},
		{name: "intent recovers", advance: 10 * time.Second, state: model.EdgeOK, source: model.SourceOK},
	}
	for _, tt := range tests {
		clock = clock.Add(tt.advance)
		snap := e.Refresh(ctx)
		if got := edgeState(snap, "orders->payments"); got != tt.state {
			t.Errorf("%s: edge state = %q, want %q", tt.name, got, tt.state)
		}
		st := statusOf(t, snap, "declared")
		if st.State != tt.source {
			t.Errorf("%s: source state = %q, want %q", tt.name, st.State, tt.source)
		}
		if tt.message != "" && st.Message != tt.message {
			t.Errorf("%s: message = %q, want %q", tt.name, st.Message, tt.message)
		}
		for _, c := range snap.Changes {
			if tt.noEdgeChanges && strings.HasPrefix(string(c.Type), "edge.") {
				t.Errorf("%s: a failed source should not change edges, got %+v", tt.name, c)
			}
		}
	}
}

func TestFailedTelemetryRetainsTopologyWithoutOldHealth(t *testing.T) {
	reading := []model.Measure{{Cause: model.CauseSaturation, What: "memory", Value: 10, Max: 100}}
	observed := source.Observation{
		Nodes:    []source.NodeObservation{{ID: "cache", Kind: model.KindCache, Measures: reading}},
		Edges:    []source.EdgeObservation{{From: "web", To: "cache", RequestRate: 10, LatencyP95Ms: 20}},
		Readings: []source.Reading{{Candidates: []string{"cache"}, Measures: reading}},
	}
	src := &scripted{name: "metrics", typ: "prometheus", steps: []func(context.Context) (source.Observation, error){
		returns(observed), fails(errors.New("unavailable")), returns(observed),
	}}
	e := New([]source.Source{src}, Options{Retention: time.Minute}, quiet())
	clock := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	e.now = func() time.Time { return clock }
	first := e.Refresh(context.Background())
	if first.TimeMode != "current" || first.Edges[0].Evidence[0].CollectedAt != clock || !first.Edges[0].Evidence[0].ObservedAt.IsZero() {
		t.Fatal("collection time must not invent the original observation time")
	}
	if len(first.Edges) != 1 || first.Edges[0].Health != model.HealthHealthy {
		t.Fatalf("expected measured healthy traffic: %+v", first.Edges)
	}
	clock = clock.Add(10 * time.Second)
	failed := e.Refresh(context.Background())
	if !failed.Edges[0].Evidence[0].Retained || failed.Edges[0].Evidence[0].CollectedAt != first.GeneratedAt {
		t.Fatal("retention must keep the old evidence time, not the new refresh time")
	}
	if statusOf(t, failed, "metrics").LastSuccessAt != first.GeneratedAt {
		t.Fatal("source must expose the last successful collection")
	}
	if len(failed.Edges) != 1 || !failed.Edges[0].Observed {
		t.Fatal("keep topology during a temporary failure")
	}
	for _, edge := range failed.Edges {
		if edge.Health != model.HealthUnknown || edge.RequestRate != 0 || edge.LatencyP95Ms != 0 || !edge.ErrorsUnknown {
			t.Errorf("old traffic must not look current: %+v", edge)
		}
	}
	for _, node := range failed.Nodes {
		if node.Health != model.HealthUnknown || len(node.Measures) != 0 {
			t.Errorf("old readings must not make a node healthy: %+v", node)
		}
	}
	if statusOf(t, failed, "metrics").State != model.SourceError {
		t.Fatal("report the source failure")
	}
	clock = clock.Add(10 * time.Second)
	recovered := e.Refresh(context.Background())
	if recovered.Edges[0].Evidence[0].Retained || recovered.Edges[0].Evidence[0].CollectedAt != clock {
		t.Fatal("recovery must replace the retained evidence")
	}
	if recovered.Edges[0].Health != model.HealthHealthy || recovered.Edges[0].RequestRate != 10 {
		t.Fatal("recovery must restore measurements, without mutating the source's observation")
	}
	if first.Edges[0].RequestRate != 10 {
		t.Fatal("retention must not mutate a previous snapshot")
	}
}

func TestSourceCollectionTimeDoesNotWaitForSlowerSources(t *testing.T) {
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(base.UnixNano())
	collected, release := make(chan struct{}), make(chan struct{})
	var observedOnce, releaseOnce sync.Once
	releaseSlow := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseSlow()
	observation := func(from string) source.Observation {
		return source.Observation{EvaluatedAt: base.Add(-time.Second), Edges: []source.EdgeObservation{{From: from, To: "cache"}}}
	}
	fast := &scripted{name: "fast", typ: "prometheus", steps: []func(context.Context) (source.Observation, error){
		returns(observation("fast")), fails(errors.New("offline")),
	}}
	slow := &scripted{name: "slow", typ: "prometheus", steps: []func(context.Context) (source.Observation, error){
		func(ctx context.Context) (source.Observation, error) {
			select {
			case <-release:
				return observation("slow"), nil
			case <-ctx.Done():
				return source.Observation{}, ctx.Err()
			}
		},
	}}
	e := New([]source.Source{fast, slow}, Options{Retention: time.Minute}, quiet())
	e.now = func() time.Time {
		at := time.Unix(0, clock.Load()).UTC()
		observedOnce.Do(func() { close(collected) })
		return at
	}
	answer := make(chan model.Snapshot, 1)
	go func() { answer <- e.Refresh(context.Background()) }()
	select {
	case <-collected:
	case <-time.After(2 * time.Second):
		t.Fatal("the fast source never recorded its collection time")
	}
	clock.Store(base.Add(30 * time.Second).UnixNano())
	releaseSlow()
	first := <-answer
	if !first.GeneratedAt.Equal(base.Add(30*time.Second)) || !statusOf(t, first, "fast").LastSuccessAt.Equal(base) ||
		!statusOf(t, first, "slow").LastSuccessAt.Equal(first.GeneratedAt) {
		t.Fatalf("source completion was replaced by snapshot assembly: %+v", first.Sources)
	}
	if ev := first.Edges[0].Evidence[0]; !ev.CollectedAt.Equal(base) || !ev.EvaluatedAt.Equal(base.Add(-time.Second)) {
		t.Fatalf("query and collection times must stay separate: %+v", ev)
	}
	clock.Store(base.Add(40 * time.Second).UnixNano())
	failed := e.Refresh(context.Background())
	if ev := failed.Edges[0].Evidence[0]; !ev.Retained || !ev.CollectedAt.Equal(base) || !ev.EvaluatedAt.Equal(base.Add(-time.Second)) {
		t.Fatalf("failure renewed the old query or collection time: %+v", ev)
	}
}

func TestTempoSampleAgeSurvivesRefreshFailureAndRecovery(t *testing.T) {
	clock := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	var end atomic.Int64
	end.Store(clock.Add(-time.Hour).UnixNano())
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/api/v2/search/tag/resource.service.name/values":
			fmt.Fprint(w, `{"tagValues":[{"value":"web"}]}`)
		case "/api/search":
			fmt.Fprint(w, `{"traces":[{"traceID":"old"}]}`)
		case "/api/v2/traces/old":
			at := end.Load()
			fmt.Fprintf(w, `{"trace":{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"web"}}]},
				"scopeSpans":[{"spans":[{"spanId":"A","kind":"SPAN_KIND_CLIENT","startTimeUnixNano":"%d","endTimeUnixNano":"%d",
				"attributes":[{"key":"peer.service","value":{"stringValue":"orders"}}]}]}]}]}}`, at-int64(20*time.Millisecond), at)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	e := New([]source.Source{tempo.New("tempo", tempo.Config{URL: srv.URL})}, Options{Retention: time.Minute}, quiet())
	e.now = func() time.Time { return clock }
	check := func(s model.Snapshot, collected time.Time, retained bool) {
		t.Helper()
		if len(s.Edges) != 1 || len(s.Edges[0].Evidence) != 1 {
			t.Fatalf("expected one edge with evidence: %+v", s.Edges)
		}
		ev := s.Edges[0].Evidence[0]
		if !ev.CollectedAt.Equal(collected) || ev.ObservedAt.UnixNano() != end.Load() ||
			ev.ObservedFrom.UnixNano() != end.Load()-int64(20*time.Millisecond) || ev.SampleCount != 1 || ev.Retained != retained {
			t.Fatalf("sample provenance changed: %+v", ev)
		}
		if s.Edges[0].RequestRate != 0 {
			t.Fatal("sample count must not become a request rate")
		}
	}
	first := e.Refresh(context.Background())
	check(first, clock, false)
	clock = clock.Add(10 * time.Second)
	second := e.Refresh(context.Background())
	check(second, clock, false)
	if second.Edges[0].LatencyP95Ms != 20 {
		t.Fatal("sample timestamps must not change the measured duration")
	}
	fail.Store(true)
	clock = clock.Add(10 * time.Second)
	failed := e.Refresh(context.Background())
	check(failed, second.GeneratedAt, true)
	if failed.Edges[0].LatencyP95Ms != 0 {
		t.Fatal("failed collection must retain topology without live latency")
	}
	fail.Store(false)
	clock = clock.Add(10 * time.Second)
	end.Store(clock.Add(-time.Second).UnixNano())
	check(e.Refresh(context.Background()), clock, false)
	if !first.Edges[0].Evidence[0].ObservedAt.Equal(first.GeneratedAt.Add(-time.Hour)) {
		t.Fatal("recovery mutated an earlier snapshot")
	}
}

func TestFreshTelemetryStillJudgesAnEdgeDuringAnotherSourceOutage(t *testing.T) {
	old := source.Observation{Edges: []source.EdgeObservation{{From: "web", To: "orders", RequestRate: 100, LatencyP95Ms: 3000}}}
	fresh := source.Observation{Edges: []source.EdgeObservation{{From: "web", To: "orders", RequestRate: 2, LatencyP95Ms: 30}}}
	flaky := &scripted{name: "flaky", typ: "prometheus", steps: []func(context.Context) (source.Observation, error){
		returns(old), fails(errors.New("unavailable")),
	}}
	working := &scripted{name: "working", typ: "prometheus", steps: []func(context.Context) (source.Observation, error){returns(fresh)}}
	e := New([]source.Source{flaky, working}, Options{Retention: time.Minute}, quiet())
	e.Refresh(context.Background())
	next := e.Refresh(context.Background())
	if len(next.Edges) != 1 {
		t.Fatalf("want one merged edge, got %d", len(next.Edges))
	}
	edge := next.Edges[0]
	if edge.RequestRate != 2 || edge.LatencyP95Ms != 30 || edge.Health != model.HealthHealthy || edge.ErrorsUnknown {
		t.Fatalf("current telemetry must win over retained topology: %+v", edge)
	}
}

func TestRefreshKeepsInventoryNamesWhileItFails(t *testing.T) {
	inventory := source.Observation{Nodes: []source.NodeObservation{{ID: "orders", Kind: model.KindService, Addresses: []string{"10.0.0.2"}}}}
	sessions := source.Observation{Edges: []source.EdgeObservation{{From: source.AddressID("10.0.0.2"), To: "orders-db", Signal: source.SignalDatabase}}}
	docker := &scripted{name: "docker", typ: "docker", steps: []func(context.Context) (source.Observation, error){
		returns(inventory), fails(errors.New("socket gone")),
	}}
	pg := &scripted{name: "postgres/orders-db", typ: "postgres", steps: []func(context.Context) (source.Observation, error){returns(sessions)}}

	e := New([]source.Source{docker, pg}, Options{Interval: time.Second, Retention: time.Minute}, quiet())
	e.Refresh(context.Background())
	snap := e.Refresh(context.Background())
	for _, n := range snap.Nodes {
		if source.IsAddressID(n.ID) {
			t.Errorf("the address should still resolve to orders while the inventory fails, got node %s", n.ID)
		}
	}
	if edgeState(snap, "orders->orders-db") == "" {
		t.Errorf("edge orders->orders-db is missing: %+v", snap.Edges)
	}
}

func TestRefreshSurvivesPanickingSource(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	bad := &scripted{name: "bad", typ: "query", steps: []func(context.Context) (source.Observation, error){
		func(context.Context) (source.Observation, error) { panic("kaboom") },
	}}
	good := &scripted{name: "good", typ: "docker", steps: []func(context.Context) (source.Observation, error){
		returns(source.Observation{Nodes: []source.NodeObservation{{ID: "orders", Kind: model.KindService}}}),
	}}

	snap := New([]source.Source{bad, good}, Options{}, log).Refresh(context.Background())
	st := statusOf(t, snap, "bad")
	if st.State != model.SourceError || st.Message != "panicked: kaboom" {
		t.Errorf("status = %+v, want an error with the panic value only", st)
	}
	if statusOf(t, snap, "good").State != model.SourceOK || len(snap.Nodes) != 1 {
		t.Errorf("the other source should still be in the round: %+v", snap)
	}
	if !strings.Contains(logs.String(), "source panicked") || !strings.Contains(logs.String(), "goroutine") {
		t.Errorf("the stack should be logged, got:\n%s", logs.String())
	}
}

func TestRefreshGivesUpOnSourceIgnoringItsTimeout(t *testing.T) {
	release := make(chan struct{})
	stuck := &scripted{name: "stuck", typ: "query", steps: []func(context.Context) (source.Observation, error){
		func(context.Context) (source.Observation, error) {
			<-release // ignores ctx on purpose
			return source.Observation{Nodes: []source.NodeObservation{{ID: "late"}}}, nil
		},
		returns(source.Observation{Nodes: []source.NodeObservation{{ID: "fresh"}}}),
	}}
	good := &scripted{name: "good", typ: "docker", steps: []func(context.Context) (source.Observation, error){
		returns(source.Observation{Nodes: []source.NodeObservation{{ID: "orders", Kind: model.KindService}}}),
	}}
	e := New([]source.Source{stuck, good}, Options{Timeout: 50 * time.Millisecond}, quiet())
	e.margin = 50 * time.Millisecond

	start := time.Now()
	snap := e.Refresh(context.Background())
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the round waited %s for a stuck source", took)
	}
	if st := statusOf(t, snap, "stuck"); st.State != model.SourceError || !strings.Contains(st.Message, "timed out") {
		t.Errorf("status = %+v, want timed out", st)
	}
	if len(snap.Nodes) != 1 || snap.Nodes[0].ID != "orders" {
		t.Errorf("nodes = %+v, want only the good source's", snap.Nodes)
	}

	snap = e.Refresh(context.Background())
	if st := statusOf(t, snap, "stuck"); !strings.Contains(st.Message, "still running") {
		t.Errorf("a source still busy should be skipped, got %+v", st)
	}

	close(release)
	for deadline := time.Now().Add(5 * time.Second); e.busy[0].Load(); {
		if time.Now().After(deadline) {
			t.Fatal("the stuck source never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	snap = e.Refresh(context.Background())
	if st := statusOf(t, snap, "stuck"); st.State != model.SourceOK {
		t.Errorf("status after recovery = %+v", st)
	}
	for _, n := range snap.Nodes {
		if n.ID == "late" {
			t.Error("the late answer of an abandoned round must be discarded")
		}
	}
}

func TestSnapshotJSONIsSharedWithSubscribers(t *testing.T) {
	src := &scripted{name: "docker", typ: "docker", steps: []func(context.Context) (source.Observation, error){
		returns(source.Observation{Nodes: []source.NodeObservation{{ID: "orders", Kind: model.KindService}}}),
	}}
	e := New([]source.Source{src}, Options{}, quiet())
	if e.SnapshotJSON() != nil {
		t.Fatal("no JSON before the first round")
	}
	updates, stop := e.Subscribe()
	defer stop()

	e.Refresh(context.Background())
	var got struct{ Round int64 }
	if err := json.Unmarshal(e.SnapshotJSON(), &got); err != nil || got.Round != 1 {
		t.Fatalf("snapshot JSON = %s (%v)", e.SnapshotJSON(), err)
	}
	select {
	case data := <-updates:
		if !bytes.Equal(data, e.SnapshotJSON()) {
			t.Error("subscribers should get the same encoded snapshot")
		}
	default:
		t.Fatal("subscriber got nothing")
	}
}

func TestNotifiersGetChanges(t *testing.T) {
	src := &scripted{name: "docker", typ: "docker", steps: []func(context.Context) (source.Observation, error){
		returns(source.Observation{}),
		returns(source.Observation{Nodes: []source.NodeObservation{{ID: "orders", Kind: model.KindService}}}),
	}}
	n := &recorder{got: make(chan []model.Change, 1)}
	e := New([]source.Source{src}, Options{Notifiers: []Notifier{n}}, quiet())
	e.Refresh(context.Background())
	e.Refresh(context.Background())
	select {
	case changes := <-n.got:
		if len(changes) == 0 || changes[0].Type != model.ChangeNodeAdded {
			t.Errorf("changes = %+v", changes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notifier was not called")
	}
}

type recorder struct {
	once sync.Once
	got  chan []model.Change
}

func (r *recorder) Notify(_ context.Context, changes []model.Change) error {
	r.once.Do(func() { r.got <- changes })
	return nil
}

func TestRefreshMatchesExplorersAndIgnoresWhateverTheirSpelling(t *testing.T) {
	traffic := source.Observation{
		Nodes: []source.NodeObservation{{ID: "orders-db", Kind: model.KindDatabase}},
		Edges: []source.EdgeObservation{{From: "web", To: "orders-db", Signal: source.SignalTraces}},
		Findings: []model.Finding{
			{Rule: "own", Severity: model.SeverityWarning, Subject: "Legacy-Batch", Title: "about an ignored node"},
		},
	}
	traces := &scripted{name: "traces", typ: "prometheus", steps: []func(context.Context) (source.Observation, error){returns(traffic)}}
	pg := &scripted{name: "postgres/orders", typ: "postgres", steps: []func(context.Context) (source.Observation, error){
		returns(source.Observation{Explores: []string{"Orders"}}),
	}}

	e := New([]source.Source{traces, pg}, Options{
		Interval: time.Second, Retention: time.Minute,
		Aliases: map[string]string{"orders": "orders-db"},
		Ignore:  []string{"legacy-batch"},
	}, quiet())
	snap := e.Refresh(context.Background())

	if got := statusOf(t, snap, "postgres/orders").Explores; len(got) != 1 || got[0] != "orders-db" {
		t.Errorf("explores = %v, want [orders-db]", got)
	}
	for _, f := range snap.Findings {
		if f.Subject == "orders-db" && strings.Contains(f.Title, "cannot look inside") {
			t.Errorf("orders-db is explored, yet: %s", f.Title)
		}
		if f.Rule == "own" {
			t.Errorf("a finding about an ignored node survived: %+v", f)
		}
	}
}

// A source that answered must be free for the very next round: its slot is
// released before its answer is handed over, not after.
func TestAnsweredSourceIsNeverSkipped(t *testing.T) {
	fast := &scripted{name: "fast", typ: "tempo", steps: []func(context.Context) (source.Observation, error){returns(source.Observation{})}}
	e := New([]source.Source{fast}, Options{Interval: time.Second, Retention: time.Minute}, quiet())
	for i := range 2000 {
		if st := statusOf(t, e.Refresh(context.Background()), "fast"); st.State != model.SourceOK {
			t.Fatalf("round %d: %s: %s", i, st.State, st.Message)
		}
	}
}
