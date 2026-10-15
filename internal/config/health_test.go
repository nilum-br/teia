package config

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
)

func TestTargetedHealthRulesPreserveScopeAndMissingData(t *testing.T) {
	cfg, err := Parse([]byte(`
health:
  overrides:
    - name: reporting-latency
      target: node
      id: reports
      limits: {latency: {degraded: 2s, failing: 5s}}
    - name: interactive-latency
      target: node
      id: api
      limits: {latency: {degraded: 100ms, failing: 500ms}}
    - name: external-connection
      target: edge
      id: web->reports
      limits: {latency: {degraded: 3s, failing: 8s}}
`))
	if err != nil {
		t.Fatal(err)
	}
	makeGraph := func() *graph.Graph {
		return &graph.Graph{Nodes: map[string]*model.Node{
			"web": {ID: "web"}, "reports": {ID: "reports"}, "api": {ID: "api"}, "missing": {ID: "missing"},
		}, Edges: map[string]*model.Edge{
			"web->reports": {From: "web", To: "reports", RequestRate: 1, LatencyP95Ms: 900},
			"web->api":     {From: "web", To: "api", RequestRate: 1000, LatencyP95Ms: 900, LatencyApproximate: true},
			"web->missing": {From: "web", To: "missing", RequestRate: 2, ErrorsUnknown: true},
		}}
	}
	g := makeGraph()
	graph.ApplyHealth(g, cfg.HealthThresholds(), cfg.HealthOverrides()...)
	if g.Nodes["reports"].Health != model.HealthHealthy || g.Nodes["api"].Health != model.HealthFailing {
		t.Fatalf("different latency budgets at 1 and 1000 req/s: reports=%+v api=%+v", g.Nodes["reports"], g.Nodes["api"])
	}
	if g.Edges["web->reports"].Health != model.HealthHealthy || g.Edges["web->api"].Health != model.HealthDegraded {
		t.Fatal("node overrides must not silently apply to incoming connections")
	}
	r := g.Nodes["api"].Reasons[0]
	if r.Rule != "interactive-latency" || r.Unit != "milliseconds" || r.Limit != 500 || !r.Approximate || !strings.Contains(r.Summary, "approximation") {
		t.Fatalf("threshold provenance must preserve approximate latency: %+v", r)
	}
	if g.Nodes["missing"].Health != model.HealthUnknown || len(g.Nodes["missing"].Reasons) != 0 {
		t.Fatalf("a selected rule is not a measurement: %+v", g.Nodes["missing"])
	}
	if len(g.Nodes["missing"].HealthRules) != 9 || g.Nodes["missing"].HealthRules[1].Name != "global" {
		t.Fatal("effective defaults should be inspectable even without data")
	}
	// Rule order must not affect the graph or metadata.
	slices.Reverse(cfg.Health.Overrides)
	reordered := makeGraph()
	graph.ApplyHealth(reordered, cfg.HealthThresholds(), cfg.HealthOverrides()...)
	if !reflect.DeepEqual(g.Nodes, reordered.Nodes) || !reflect.DeepEqual(g.Edges, reordered.Edges) {
		t.Fatal("YAML rule order changed the result")
	}
}

func TestHealthSelectorsPrecedenceAndSignalInheritance(t *testing.T) {
	cfg, err := Parse([]byte(`
health:
  latency: {degraded: 400ms, failing: 4s}
  overrides:
    - name: prod-default
      target: node
      labels: {namespace: prod}
      priority: 10
      limits: {latency: {degraded: 1s}, degraded: 0.1, failing: 0.2}
    - name: api-tier
      target: node
      labels: {tier: api}
      priority: 20
      limits: {latency: {degraded: 2s}}
    - name: specific-api
      target: node
      id: api
      priority: -10
      limits: {latency: {degraded: 3s}}
    - name: prod-to-db
      target: edge
      fromLabels: {namespace: prod}
      toLabels: {tier: database}
      limits: {latency: {degraded: 5s, failing: 10s}}
`))
	if err != nil {
		t.Fatal(err)
	}
	g := &graph.Graph{Nodes: map[string]*model.Node{
		"api":   {ID: "api", Labels: map[string]string{"namespace": "prod", "tier": "api"}},
		"other": {ID: "other", Labels: map[string]string{"namespace": "prod", "tier": "api"}},
		"db":    {ID: "db", Labels: map[string]string{"tier": "database"}},
	}, Edges: map[string]*model.Edge{
		"api->db": {From: "api", To: "db", RequestRate: 10, ErrorRate: 0.08, LatencyP95Ms: 3000},
		"db->api": {From: "db", To: "api", RequestRate: 10, ErrorRate: 0.08, LatencyP95Ms: 3000},
	}}
	graph.ApplyHealth(g, cfg.HealthThresholds(), cfg.HealthOverrides()...)
	rule := func(rules []model.HealthRule, cause model.Cause) model.HealthRule {
		for _, r := range rules {
			if r.Cause == cause {
				return r
			}
		}
		t.Fatalf("missing %s rule", cause)
		return model.HealthRule{}
	}
	latency := rule(g.Nodes["api"].HealthRules, model.CauseLatency)
	if latency.Name != "specific-api" || latency.Match != "identity" || latency.Degraded != 3000 || latency.Failing != 4000 {
		t.Fatalf("identity wins over higher selector priority, partial pair inherits global: %+v", latency)
	}
	if rule(g.Nodes["api"].HealthRules, model.CauseErrors).Name != "prod-default" {
		t.Fatal("selection must be per signal")
	}
	if rule(g.Nodes["other"].HealthRules, model.CauseLatency).Name != "api-tier" {
		t.Fatal("higher selector priority must win")
	}
	if rule(g.Edges["api->db"].HealthRules, model.CauseLatency).Name != "prod-to-db" || rule(g.Edges["db->api"].HealthRules, model.CauseLatency).Name != "global" {
		t.Fatal("connection selectors must respect direction and require every label")
	}
}

func TestHealthReasonEvidenceKeepsSignalPeriods(t *testing.T) {
	start := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	evidence := []model.Evidence{
		{Source: "mesh", Metrics: []string{"latency"}, EvaluatedAt: start, WindowSeconds: 60},
		{Source: "traces", Metrics: []string{"latency"}, ObservedFrom: start.Add(-5 * time.Minute), ObservedAt: start, WindowSeconds: 300},
		{Source: "exporter", Metrics: []string{"backlog: consumer lag"}, WindowSeconds: 30},
		{Source: "old", Metrics: []string{"latency"}, Retained: true},
	}
	g := &graph.Graph{Nodes: map[string]*model.Node{"queue": {ID: "queue", Measures: []model.Measure{{Cause: model.CauseBacklog, What: "consumer lag", Value: 10000}}, Evidence: evidence}}, Edges: map[string]*model.Edge{"web->queue": {From: "web", To: "queue", RequestRate: 1, LatencyP95Ms: 2000, Evidence: evidence}}}
	graph.ApplyHealth(g, graph.DefaultHealth)
	latency := g.Edges["web->queue"].Reasons[0]
	if latency.Rule != "global" || latency.Unit != "milliseconds" || len(latency.Evidence) != 2 || latency.Evidence[0].WindowSeconds != 60 || latency.Evidence[1].WindowSeconds != 300 {
		t.Fatalf("keep actual distinct periods, never substitute one union window: %+v", latency)
	}
	for _, r := range g.Nodes["queue"].Reasons {
		if r.Cause == model.CauseBacklog && (r.Unit != "count" || len(r.Evidence) == 0 || r.Evidence[0].Source != "exporter") {
			t.Fatalf("wrong backlog evidence: %+v", r)
		}
	}
	if _, err := json.Marshal(g.SortedNodes()); err != nil {
		t.Fatal(err)
	}
}

func TestHealthOverrideValidation(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"missing name", "{target: node, id: a, limits: {failing: 0.2}}", ".name"},
		{"reserved name", "{name: global, target: node, id: a, limits: {failing: 0.2}}", "reserved"},
		{"bad target", "{name: x, target: service, id: a, limits: {failing: 0.2}}", "node or edge"},
		{"no selector", "{name: x, target: node, limits: {failing: 0.2}}", "matching mode"},
		{"mixed selector", "{name: x, target: node, id: a, labels: {namespace: prod}, limits: {failing: 0.2}}", "matching mode"},
		{"wrong labels", "{name: x, target: edge, labels: {namespace: prod}, limits: {failing: 0.2}}", "fromLabels/toLabels"},
		{"wrong endpoint labels", "{name: x, target: node, fromLabels: {namespace: prod}, limits: {failing: 0.2}}", "node rules use labels"},
		{"empty label key", "{name: x, target: node, labels: {'': prod}, limits: {failing: 0.2}}", "label key"},
		{"bad edge id", "{name: x, target: edge, id: a, limits: {failing: 0.2}}", "caller->callee"},
		{"empty limits", "{name: x, target: node, id: a, limits: {}}", "positive threshold"},
		{"negative", "{name: x, target: node, id: a, limits: {latency: {degraded: -1s}}}", "negative"},
		{"out of range", "{name: x, target: node, id: a, limits: {saturation: {failing: 2}}}", "between 0 and 1"},
		{"nan", "{name: x, target: node, id: a, limits: {failing: .nan}}", "finite"},
		{"infinity", "{name: x, target: node, id: a, limits: {backlog: {failing: .inf}}}", "finite"},
		{"inherited inverted", "{name: x, target: node, id: a, limits: {latency: {degraded: 3s}}}", "global inheritance included"},
		{"unknown key", "{name: x, target: node, id: a, limits: {latenci: 5s}}", "unknown key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte("health:\n  overrides:\n    - " + tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
		})
	}
	for _, value := range []string{".nan", ".inf"} {
		if _, err := Parse([]byte("health: {degraded: " + value + "}")); err == nil {
			t.Fatalf("global %s accepted", value)
		}
	}
}

func TestHealthOverrideAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name, second string
		valid        bool
	}{
		{"overlapping different labels", "labels: {tier: api}", false},
		{"overlapping subset", "labels: {namespace: prod, tier: api}", false},
		{"identical selector", "labels: {namespace: prod}", false},
		{"disjoint selectors", "labels: {namespace: staging}", true},
		{"explicit priority", "labels: {tier: api}, priority: 1", true},
		{"identity precedence", "id: orders", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte("health:\n  overrides:\n    - {name: a, target: node, labels: {namespace: prod}, limits: {failing: 0.1}}\n    - {name: b, target: node, " + tc.second + ", limits: {failing: 0.2}}"))
			if tc.valid && err != nil {
				t.Fatal(err)
			}
			if !tc.valid && (err == nil || !strings.Contains(err.Error(), "ambiguous")) {
				t.Fatalf("want ambiguous, got %v", err)
			}
		})
	}
	for _, second := range []string{
		"{name: b, target: edge, id: web->db, limits: {failing: 0.2}}",
		"{name: a, target: edge, id: other->db, limits: {failing: 0.2}}",
	} {
		if _, err := Parse([]byte("health:\n  overrides:\n    - {name: a, target: edge, id: web->db, limits: {failing: 0.1}}\n    - " + second)); err == nil {
			t.Fatalf("duplicate identity/priority or name accepted: %s", second)
		}
	}
	if _, err := Parse([]byte("health:\n  overrides:\n    - {name: errors, target: node, id: api, limits: {failing: 0.1}}\n    - {name: latency, target: node, id: api, limits: {latency: {failing: 3s}}}")); err != nil {
		t.Fatalf("distinct signals are unambiguous: %v", err)
	}
}
