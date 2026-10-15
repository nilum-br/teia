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

// With ?slice= the operations are the sliced round's, never the whole
// platform's; a bad selection or an engine that cannot slice says so.
func TestOperationEndpointFollowsSlices(t *testing.T) {
	eng := &slicingEngine{fakeEngine: newFakeEngine()}
	eng.snap.Operations = model.EmptyOperations()
	eng.snap.Operations.Sources = []model.OperationSource{{Source: "tempo", Status: "sampled"}}
	eng.operations = model.EmptyOperations()
	eng.operations.Sources = []model.OperationSource{{Source: "tempo", Status: "excluded"}}
	h := Handler(eng, Options{})
	status := func(path string) string {
		var contract model.OperationContract
		rec := get(h, path, nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &contract); err != nil || rec.Code != http.StatusOK || len(contract.Sources) != 1 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		return contract.Sources[0].Status
	}
	if got := status("/api/v1/operations"); got != "sampled" {
		t.Errorf("whole round = %q", got)
	}
	if got := status("/api/v1/operations?slice="); got != "sampled" {
		t.Errorf("an empty selection is the whole round, got %q", got)
	}
	if got := status("/api/v1/operations?slice=journey:checkout"); got != "excluded" || eng.asked[len(eng.asked)-1] != "journey:checkout" {
		t.Errorf("sliced round = %q, asked %v", got, eng.asked)
	}
	if rec := get(h, "/api/v1/operations?slice=journey", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed selection: %d", rec.Code)
	}
	if rec := get(Handler(newFakeEngine(), Options{}), "/api/v1/operations?slice=journey:checkout", nil); rec.Code != http.StatusNotImplemented {
		t.Errorf("an engine that cannot slice must say so, got %d", rec.Code)
	}
}
