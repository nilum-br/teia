package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func TestGrafanaPreservesEffectiveHealthRulesAndEvidence(t *testing.T) {
	rules := []model.HealthRule{{Cause: model.CauseLatency, Name: "reporting", Match: "identity", Degraded: 3000, Failing: 8000, Unit: "milliseconds"}}
	reasons := []model.Reason{{Cause: model.CauseLatency, Health: model.HealthFailing, Value: 9000, Limit: 8000, Rule: "reporting", Unit: "milliseconds", Approximate: true,
		Evidence: []model.Evidence{{Source: "tempo", WindowSeconds: 300, EvaluatedAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}}}}
	snapshot := model.Snapshot{Nodes: []model.Node{{ID: "reports", HealthRules: rules, Reasons: reasons}}, Edges: []model.Edge{{From: "web", To: "reports", HealthRules: rules, Reasons: reasons}}}
	for _, raw := range []string{grafanaNodes(snapshot)[0].HealthRules, grafanaEdges(snapshot)[0].HealthRules} {
		var decoded []model.HealthRule
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded) != 1 || decoded[0] != rules[0] {
			t.Fatalf("lost effective rule: %s", raw)
		}
	}
	for _, raw := range []string{grafanaNodes(snapshot)[0].Reasons, grafanaEdges(snapshot)[0].Reasons} {
		var decoded []model.Reason
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded) != 1 || decoded[0].Rule != "reporting" || decoded[0].Unit != "milliseconds" || !decoded[0].Approximate || len(decoded[0].Evidence) != 1 || decoded[0].Evidence[0].WindowSeconds != 300 {
			t.Fatalf("lost rule/evidence/qualification: %s", raw)
		}
	}
}
