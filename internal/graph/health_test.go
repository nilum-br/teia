package graph

import (
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

func TestHealthNeedsAnInterpretableMeasure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		measures []model.Measure
		want     model.Health
	}{
		{"memory without a capacity", []model.Measure{{Cause: model.CauseSaturation, Value: 100}}, model.HealthUnknown},
		{"unsupported reading", []model.Measure{{Cause: "temperature", Value: 30}}, model.HealthUnknown},
		{"empty queue is measured", []model.Measure{{Cause: model.CauseBacklog, Value: 0}}, model.HealthHealthy},
		{"known capacity", []model.Measure{{Cause: model.CauseSaturation, Value: 10, Max: 100}}, model.HealthHealthy},
		{"missing capacity does not hide backlog", []model.Measure{{Cause: model.CauseSaturation, Value: 100}, {Cause: model.CauseBacklog, Value: 15000}}, model.HealthFailing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := DefaultHealth.Judge(Signals{Measures: tc.measures})
			if got != tc.want {
				t.Fatalf("health = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestNodeErrorCoverageDescribesMeasuredTraffic(t *testing.T) {
	g := Build([]Result{result("metrics", "prometheus", source.Observation{Edges: []source.EdgeObservation{
		{From: "a", To: "c", RequestRate: 100, ErrorRate: 0.5},
		{From: "b", To: "c", RequestRate: 900, NoErrors: true},
	}})}, nil, nil)
	ApplyHealth(g, DefaultHealth)
	n := g.Nodes["c"]
	if n.ErrorRate != 0.5 || n.ErrorCoverage != 0.1 || n.ErrorsUnknown || n.Health != model.HealthFailing {
		t.Fatalf("preserve measured error rate and its coverage: %+v", n)
	}
}

func TestLatencySummaryKeepsCoverageAndApproximation(t *testing.T) {
	g := Build([]Result{result("metrics", "prometheus", source.Observation{Edges: []source.EdgeObservation{
		{From: "a", To: "c", RequestRate: 100, LatencyP50Ms: 10, LatencyP95Ms: 1000},
		{From: "b", To: "c", RequestRate: 300, LatencyP95Ms: 3000},
		{From: "d", To: "c", RequestRate: 600},
	}})}, nil, nil)
	ApplyHealth(g, DefaultHealth)
	n := g.Nodes["c"]
	if n.LatencyP95Ms != 2500 || n.LatencyP50Ms != 10 || !n.LatencyApproximate || n.LatencyCoverage != 0.4 || n.TrafficDirection != "incoming" {
		t.Fatalf("do not dilute missing p50/p95 or present a combined percentile: %+v", n)
	}
	if len(n.Reasons) != 1 || !n.Reasons[0].Approximate {
		t.Fatalf("latency reason must carry the qualification: %+v", n.Reasons)
	}
	if g.Nodes["a"].TrafficDirection != "outgoing" {
		t.Fatal("root traffic is outgoing calls, not incoming user requests")
	}
}

func TestRetentionKeepsEdgesButNotOldTraffic(t *testing.T) {
	for _, declared := range []bool{false, true} {
		memory := &Memory{Retention: time.Minute}
		now := time.Now()
		first := Build([]Result{result("metrics", "prometheus", source.Observation{Edges: []source.EdgeObservation{
			{From: "web", To: "orders", RequestRate: 5, ErrorRate: 0.2, LatencyP95Ms: 100},
		}})}, nil, nil)
		memory.Retain(first, now)
		var results []Result
		if declared {
			results = []Result{result("flows", "declared", source.Observation{Edges: []source.EdgeObservation{
				{From: "web", To: "orders", Declared: true},
			}})}
		}
		later := Build(results, nil, nil)
		memory.Retain(later, now.Add(10*time.Second))
		ApplyHealth(later, DefaultHealth)
		edge := later.Edges["web->orders"]
		if edge == nil || !edge.Observed || edge.Declared != declared {
			t.Fatalf("keep recent topology and current intent: %+v", edge)
		}
		if edge.Health != model.HealthUnknown || edge.RequestRate != 0 || edge.ErrorRate != 0 || edge.LatencyP95Ms != 0 || !edge.ErrorsUnknown {
			t.Fatalf("retained edge must not replay its old metrics: %+v", edge)
		}
	}
}

// Subjects without an override share one copy of the global rules; an
// override gets its own, and never changes the shared one.
func TestGlobalHealthRulesAreSharedNotCopied(t *testing.T) {
	g := Build([]Result{result("metrics", "prometheus", source.Observation{Edges: []source.EdgeObservation{
		{From: "a", To: "b", RequestRate: 1}, {From: "b", To: "c", RequestRate: 1}, {From: "c", To: "d", RequestRate: 1, LatencyP95Ms: 900},
	}})}, nil, nil)
	strict := DefaultHealth
	strict.LatencyDegradedMs, strict.LatencyFailingMs = 100, 800
	ApplyHealth(g, DefaultHealth, HealthOverride{Name: "strict", Target: "edge", ID: "c->d", Causes: []model.Cause{model.CauseLatency}, Limits: strict})
	ab, bc, cd := g.Edges["a->b"], g.Edges["b->c"], g.Edges["c->d"]
	if &ab.HealthRules[0] != &bc.HealthRules[0] || &ab.HealthRules[0] != &g.Nodes["a"].HealthRules[0] {
		t.Fatal("subjects without an override must share the global rules")
	}
	if &cd.HealthRules[0] == &ab.HealthRules[0] || cd.HealthRules[1].Name != "strict" || cd.Health != model.HealthFailing {
		t.Fatalf("the override gets its own rules: %+v %s", cd.HealthRules[1], cd.Health)
	}
	if ab.HealthRules[1].Name != "global" || ab.HealthRules[1].Failing != DefaultHealth.LatencyFailingMs {
		t.Fatalf("the shared rules must stay global: %+v", ab.HealthRules[1])
	}
	if cap(ab.HealthRules) != len(ab.HealthRules) {
		t.Fatal("an append must never write into the shared rules")
	}
}
