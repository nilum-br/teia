package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func TestOperationEndpointAndEmptyTopologyFramePreserveSamplingContract(t *testing.T) {
	engine := newFakeEngine()
	engine.snap.Operations = model.EmptyOperations()
	engine.snap.Operations.Sources = []model.OperationSource{{Source: "tempo", Status: "unavailable"}}
	engine.snap.GeneratedAt = time.Now().UTC()
	h := Handler(engine, Options{Token: "secret"})
	if rec := get(h, "/api/v1/operations", nil); rec.Code != http.StatusUnauthorized {
		t.Fatal("operation endpoint bypasses authentication")
	}
	auth := map[string]string{"Authorization": "Bearer secret"}
	var contract model.OperationContract
	rec := get(h, "/api/v1/operations", auth)
	if err := json.Unmarshal(rec.Body.Bytes(), &contract); err != nil || contract.Mode != "operation" || contract.Sampling.Coverage != nil || len(contract.Scopes) != 0 || contract.Sources[0].Status != "unavailable" {
		t.Fatalf("operation response=%s err=%v", rec.Body.String(), err)
	}
	rec = get(h, "/api/v1/coverage", auth)
	var row map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["operations"] == nil || !strings.Contains(row["operations"].(string), `"mode":"operation"`) {
		t.Fatal("empty topology lost operation metadata")
	}
	engine.snap.Nodes = []model.Node{{ID: "web"}, {ID: "db"}}
	rows := grafanaNodes(engine.snap)
	if rows[0].Operations == "" || rows[1].Operations != "" {
		t.Fatal("operation contract must be attached once per nodes frame")
	}
}
