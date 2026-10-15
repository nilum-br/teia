package api

import (
	"encoding/json"
	"testing"

	"github.com/nilum-br/teia/internal/journey"
	"github.com/nilum-br/teia/internal/model"
)

func TestJourneyContractIsSharedBySnapshotGraphAndFrames(t *testing.T) {
	eng := newFakeEngine()
	eng.snap.Nodes[0].Journeys = []string{"checkout", "browse"}
	eng.snap.Nodes[0].Labels = map[string]string{"team": "storefront"}
	eng.snap.Journeys = journey.Build(eng.snap, "")
	h := Handler(eng, Options{})
	for _, endpoint := range []string{"snapshot", "graph", "journeys"} {
		rec := get(h, "/api/v1/"+endpoint, nil)
		if rec.Code != 200 {
			t.Fatalf("%s status %d: %s", endpoint, rec.Code, rec.Body.String())
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		data := rec.Body.Bytes()
		if endpoint != "journeys" {
			data = payload["journeys"]
		}
		var contract model.JourneyContract
		if err := json.Unmarshal(data, &contract); err != nil || contract.Mode != "ownership" || len(contract.Scopes) != 2 {
			t.Fatalf("%s contract: %+v (%v)", endpoint, contract, err)
		}
	}
	rows := grafanaNodes(eng.snap)
	var contract model.JourneyContract
	if err := json.Unmarshal([]byte(rows[0].JourneyContract), &contract); err != nil || len(contract.Scopes) != 2 {
		t.Fatalf("frame contract lost: %s %v", rows[0].JourneyContract, err)
	}
	var memberships []string
	if err := json.Unmarshal([]byte(rows[0].Journeys), &memberships); err != nil || len(memberships) != 2 {
		t.Fatalf("frame membership lost: %s %v", rows[0].Journeys, err)
	}
	rec := get(h, "/api/v1/journeys?key=team", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &contract); err != nil || contract.Key != "team" || len(contract.Scopes) != 1 || contract.Scopes[0].Name != "storefront" {
		t.Fatalf("custom scope = %s (%v)", rec.Body.String(), err)
	}
}
