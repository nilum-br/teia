package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

func TestTargetedHealthAcrossIdentityResolutionAndSourceFailure(t *testing.T) {
	observed := &scripted{name: "mesh", typ: "prometheus", steps: []func(context.Context) (source.Observation, error){
		returns(source.Observation{Edges: []source.EdgeObservation{{From: "WEB", To: "ReportingService", Signal: source.SignalMesh,
			RequestRate: 1, LatencyP95Ms: 3000, Window: time.Minute}}}),
		fails(errors.New("source unavailable")),
	}}
	limits := graph.DefaultHealth
	limits.LatencyDegradedMs, limits.LatencyFailingMs = 1000, 6000
	e := New([]source.Source{observed}, Options{Retention: time.Minute,
		Aliases:         map[string]string{"reportingservice": "reports"},
		HealthOverrides: []graph.HealthOverride{{Name: "reporting-call", Target: "edge", ID: "web->reports", Limits: limits, Causes: []model.Cause{model.CauseLatency}}},
	}, quiet())
	first := e.Refresh(context.Background())
	if len(first.Edges) != 1 || first.Edges[0].Health != model.HealthDegraded || first.Edges[0].Reasons[0].Rule != "reporting-call" {
		t.Fatalf("override must use the resolved canonical identity: %+v", first.Edges)
	}
	if len(first.Edges[0].Reasons[0].Evidence) != 1 || first.Edges[0].Reasons[0].Evidence[0].WindowSeconds != 60 {
		t.Fatalf("a real source round must expose the judged signal's period: %+v", first.Edges[0].Reasons)
	}
	later := e.Refresh(context.Background())
	if len(later.Edges) != 1 || later.Edges[0].Health != model.HealthUnknown || len(later.Edges[0].Reasons) != 0 {
		t.Fatalf("retention cannot replay a breached threshold or measured value: %+v", later.Edges)
	}
	if later.Edges[0].HealthRules[1].Name != "reporting-call" || !later.Edges[0].Evidence[0].Retained {
		t.Fatalf("rule remains inspectable while the retained observation is unavailable: %+v", later.Edges[0])
	}
}
