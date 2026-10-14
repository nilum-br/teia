package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
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
