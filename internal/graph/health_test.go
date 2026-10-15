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
