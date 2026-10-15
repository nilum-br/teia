package graph

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func TestFrequencyUsesOriginalTimeAcrossShortWindowsAndHistory(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	makeGraph := func() *Graph {
		return &Graph{Nodes: map[string]*model.Node{}, Edges: map[string]*model.Edge{"daily->archive": {From: "daily", To: "archive", Declared: true, ExpectedFrequencySeconds: 86400}}}
	}
	var memory FrequencyMemory
	calls := 0
	lookup := func(string) (model.Edge, time.Time, bool, error) {
		calls++
		return model.Edge{Observed: true, Evidence: []model.Evidence{{ObservedAt: now.Add(-time.Hour), Metrics: []string{"topology"}}}}, now, true, nil
	}
	g := makeGraph()
	memory.Apply(g, now, lookup)
	if status := g.Edges["daily->archive"].Frequency; status.Status != "not-due" || !status.LastObservedAt.Equal(now.Add(-time.Hour)) || !status.NextExpectedAt.Equal(now.Add(23*time.Hour)) {
		t.Fatalf("daily job judged using short round: %+v", status)
	}
	late := makeGraph()
	memory.Apply(late, now.Add(25*time.Hour), lookup)
	if calls != 1 || late.Edges["daily->archive"].Frequency.Status != "overdue" {
		t.Fatalf("history not cached or deadline lost: calls=%d %+v", calls, late.Edges)
	}
	// A late response from an older period does not advance the original time.
	late.Edges["daily->archive"].Observed = true
	late.Edges["daily->archive"].Evidence = []model.Evidence{{ObservedAt: now.Add(-2 * time.Hour), CollectedAt: now.Add(25 * time.Hour), Metrics: []string{"topology"}}}
	memory.Apply(late, now.Add(25*time.Hour), lookup)
	if !late.Edges["daily->archive"].Frequency.LastObservedAt.Equal(now.Add(-time.Hour)) {
		t.Fatal("late data replaced newer original evidence")
	}
	late.Edges["daily->archive"].Evidence[0].ObservedAt = now.Add(25 * time.Hour)
	memory.Apply(late, now.Add(25*time.Hour), lookup)
	if late.Edges["daily->archive"].Frequency.Status != "not-due" {
		t.Fatal("reappearance did not reset the original-event interval")
	}
}

func TestFrequencyUnknownIsNotACollectedTimestamp(t *testing.T) {
	now := time.Now()
	for _, ev := range []model.Evidence{
		{CollectedAt: now, EvaluatedAt: now, WindowSeconds: 300, Metrics: []string{"topology"}},
		{ObservedAt: now, Retained: true, Metrics: []string{"topology"}},
		{ObservedAt: now.Add(time.Hour), Metrics: []string{"topology"}},
		{ObservedAt: now, ObservedFrom: now.Add(time.Hour), Metrics: []string{"topology"}},
		{ObservedAt: now, Metrics: []string{"latency"}},
	} {
		var memory FrequencyMemory
		g := &Graph{Edges: map[string]*model.Edge{"a->b": {From: "a", To: "b", Declared: true, Observed: true, ExpectedFrequencySeconds: 86400, Evidence: []model.Evidence{ev}}}}
		memory.Apply(g, now, nil)
		if status := g.Edges["a->b"].Frequency; status.Status != "unknown" || !status.LastObservedAt.IsZero() || !status.NextExpectedAt.IsZero() {
			t.Fatalf("invalid/missing time became a last call: %+v", status)
		}
	}
}

func TestFrequencyFailureDoesNotInventHistoryAndRetries(t *testing.T) {
	var memory FrequencyMemory
	g := &Graph{Edges: map[string]*model.Edge{"a->b": {From: "a", To: "b", Declared: true, ExpectedFrequencySeconds: 86400}}}
	lookup := func(string) (model.Edge, time.Time, bool, error) {
		return model.Edge{}, time.Time{}, false, errors.New("cannot read history")
	}
	if errs := memory.Apply(g, time.Now(), lookup); len(errs) != 1 || g.Edges["a->b"].Frequency.Status != "unknown" {
		t.Fatalf("failed history created evidence: %v %+v", errs, g.Edges)
	}
	if errs := memory.Apply(g, time.Now(), lookup); len(errs) != 1 {
		t.Fatal("history read error was cached as absence")
	}
	g.Edges = map[string]*model.Edge{}
	memory.Apply(g, time.Now(), nil)
	if len(memory.edges) != 0 {
		t.Fatal("removed declarations kept unbounded memory")
	}
}

func TestFrequencyMaximumDurationDoesNotOverflow(t *testing.T) {
	now := time.Now()
	var memory FrequencyMemory
	g := &Graph{Edges: map[string]*model.Edge{"a->b": {From: "a", To: "b", Declared: true, Observed: true, ExpectedFrequencySeconds: time.Duration(math.MaxInt64).Seconds(), Evidence: []model.Evidence{{ObservedAt: now, Metrics: []string{"topology"}}}}}}
	memory.Apply(g, now, nil)
	if f := g.Edges["a->b"].Frequency; f.Status != "not-due" || !f.NextExpectedAt.Equal(now.Add(time.Duration(math.MaxInt64))) {
		t.Fatalf("maximum interval overflowed: %+v", f)
	}
}
