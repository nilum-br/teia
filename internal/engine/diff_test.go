package engine

import (
	"reflect"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

var round = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// summary keeps the fields a test cares about.
type summary struct {
	Type        model.ChangeType
	Subject, ID string
	From, To    string
}

func summarise(changes []model.Change) []summary {
	var out []summary
	for _, c := range changes {
		out = append(out, summary{c.Type, c.Subject, c.ID, c.From, c.To})
	}
	return out
}

func node(id string, h model.Health) model.Node {
	return model.Node{ID: id, Kind: model.KindService, Visibility: model.VisibilityFull, Health: h}
}

func edge(from, to string, s model.EdgeState, h model.Health) model.Edge {
	return model.Edge{From: from, To: to, State: s, Health: h}
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name       string
		prev, next model.Snapshot
		want       []summary
	}{
		{
			name: "first round reports nothing",
			next: model.Snapshot{GeneratedAt: round, Nodes: []model.Node{node("a", model.HealthHealthy)}},
		},
		{
			name: "removed nodes and edges come in a stable order",
			prev: model.Snapshot{GeneratedAt: round,
				Nodes: []model.Node{node("a", ""), node("c", ""), node("d", ""), node("e", "")},
				Edges: []model.Edge{edge("a", "c", model.EdgeOK, ""), edge("a", "d", model.EdgeGhost, ""), edge("c", "d", model.EdgeStale, "")}},
			next: model.Snapshot{GeneratedAt: round.Add(time.Minute), Nodes: []model.Node{node("a", ""), node("b", "")}},
			want: []summary{
				{Type: model.ChangeNodeAdded, Subject: "b", To: "service"},
				{Type: model.ChangeNodeRemoved, Subject: "c"},
				{Type: model.ChangeNodeRemoved, Subject: "d"},
				{Type: model.ChangeNodeRemoved, Subject: "e"},
				{Type: model.ChangeEdgeRemoved, Subject: "a->c", From: "ok"},
				{Type: model.ChangeEdgeRemoved, Subject: "a->d", From: "ghost"},
				{Type: model.ChangeEdgeRemoved, Subject: "c->d", From: "stale"},
			},
		},
		{
			name: "findings sharing a subject are told apart by ID",
			prev: model.Snapshot{GeneratedAt: round, Findings: []model.Finding{
				{ID: "stale/orders", Subject: "orders", Severity: model.SeverityWarning, Title: "stale"},
				{ID: "blind/orders", Subject: "orders", Severity: model.SeverityInfo, Title: "blind"},
			}},
			next: model.Snapshot{GeneratedAt: round.Add(time.Minute), Findings: []model.Finding{
				{ID: "blind/orders", Subject: "orders", Severity: model.SeverityInfo, Title: "blind"},
				{ID: "ghost/orders", Subject: "orders", Severity: model.SeverityWarning, Title: "ghost"},
			}},
			want: []summary{
				{Type: model.ChangeFindingOpened, Subject: "orders", ID: "ghost/orders", To: "warning"},
				{Type: model.ChangeFindingResolved, Subject: "orders", ID: "stale/orders", From: "warning"},
			},
		},
		{
			name: "edge state and health",
			prev: model.Snapshot{GeneratedAt: round, Edges: []model.Edge{edge("a", "b", model.EdgeOK, model.HealthHealthy)}},
			next: model.Snapshot{GeneratedAt: round.Add(time.Minute), Edges: []model.Edge{edge("a", "b", model.EdgeGhost, model.HealthFailing)}},
			want: []summary{
				{Type: model.ChangeEdgeState, Subject: "a->b", From: "ok", To: "ghost"},
				{Type: model.ChangeEdgeHealth, Subject: "a->b", From: "healthy", To: "failing"},
			},
		},
		{
			name: "a move to unknown is not reported",
			prev: model.Snapshot{GeneratedAt: round, Nodes: []model.Node{node("a", model.HealthFailing)}},
			next: model.Snapshot{GeneratedAt: round.Add(time.Minute), Nodes: []model.Node{node("a", model.HealthUnknown)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := Diff(tt.prev, tt.next, round)
			if got := summarise(changes); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("changes:\n got %+v\nwant %+v", got, tt.want)
			}
			for _, c := range changes {
				if !c.At.Equal(round) || c.Message == "" {
					t.Errorf("change without time or message: %+v", c)
				}
			}
		})
	}
}

// A brief drop to unknown neither alerts nor makes the next known state look new.
func TestDifferRemembersLastKnownHealth(t *testing.T) {
	healths := []model.Health{
		model.HealthHealthy, // first round: nothing
		model.HealthFailing, // healthy -> failing
		model.HealthUnknown, // silent
		model.HealthFailing, // same as before the gap: silent
		model.HealthUnknown, // silent
		model.HealthHealthy, // failing -> healthy
	}
	want := [][]summary{
		nil,
		{{Type: model.ChangeNodeHealth, Subject: "a", From: "healthy", To: "failing"}, {Type: model.ChangeEdgeHealth, Subject: "a->b", From: "healthy", To: "failing"}},
		nil,
		nil,
		nil,
		{{Type: model.ChangeNodeHealth, Subject: "a", From: "failing", To: "healthy"}, {Type: model.ChangeEdgeHealth, Subject: "a->b", From: "failing", To: "healthy"}},
	}

	var d Differ
	var prev model.Snapshot
	for i, h := range healths {
		next := model.Snapshot{
			GeneratedAt: round.Add(time.Duration(i) * time.Minute),
			Nodes:       []model.Node{node("a", h), node("b", model.HealthUnknown)},
			Edges:       []model.Edge{edge("a", "b", model.EdgeOK, h)},
		}
		if got := summarise(d.Diff(prev, next, next.GeneratedAt)); !reflect.DeepEqual(got, want[i]) {
			t.Errorf("round %d (%s):\n got %+v\nwant %+v", i+1, h, got, want[i])
		}
		prev = next
	}
}

func TestDifferForgetsRemovedSubjects(t *testing.T) {
	var d Differ
	a := model.Snapshot{GeneratedAt: round, Nodes: []model.Node{node("a", model.HealthFailing)}}
	d.Diff(model.Snapshot{}, a, round)
	d.Diff(a, model.Snapshot{GeneratedAt: round.Add(time.Minute)}, round)
	if len(d.known) != 0 {
		t.Errorf("known = %v, want it empty once the node is gone", d.known)
	}
}
