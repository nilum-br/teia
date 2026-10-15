package api

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func TestGrafanaIdentityPreservesEndpointProvenance(t *testing.T) {
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	from := model.IdentityResolution{Input: "ip:10.0.0.1", ResolvedID: "orders", Method: model.IdentityIPTemporal, Source: "network", SourceType: "prometheus", CollectedAt: at, EvaluatedAt: at.Add(-time.Second), TimeBasis: "evaluation-window", WindowSeconds: 60, Retained: true,
		Candidates: []model.IdentityCandidate{{ID: "orders", Source: "inventory", SourceType: "kubernetes", Since: at.Add(-time.Hour), CollectedAt: at.Add(-time.Minute), Retained: true}}}
	to := model.IdentityResolution{Input: "old-db", ResolvedID: "db", Method: model.IdentityAlias, Source: "network", SourceType: "prometheus", TimeBasis: "not-applicable"}
	connection := &model.ConnectionIdentity{From: []model.IdentityResolution{from}, To: []model.IdentityResolution{to}}
	s := model.Snapshot{Nodes: []model.Node{{ID: "orders", Identity: []model.IdentityResolution{from}}}, Edges: []model.Edge{{From: "orders", To: "db", Identity: connection}}}
	var nodes []model.IdentityResolution
	if err := json.Unmarshal([]byte(grafanaNodes(s)[0].Identity), &nodes); err != nil || !reflect.DeepEqual(nodes, s.Nodes[0].Identity) {
		t.Fatalf("node provenance changed in frames: %v %+v", err, nodes)
	}
	var edge model.ConnectionIdentity
	if err := json.Unmarshal([]byte(grafanaEdges(s)[0].Identity), &edge); err != nil || !reflect.DeepEqual(edge, *connection) {
		t.Fatalf("endpoint provenance changed in frames: %v %+v", err, edge)
	}
	// Older persisted snapshots have no metadata; neither frame invents it.
	if grafanaNodes(model.Snapshot{Nodes: []model.Node{{ID: "old"}}})[0].Identity != "[]" || grafanaEdges(model.Snapshot{Edges: []model.Edge{{From: "old", To: "db"}}})[0].Identity != "null" {
		t.Fatal("old snapshots were assigned fabricated identity provenance")
	}
}
