package history

import (
	"reflect"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func TestReplayPreservesIdentityDecisionBeforeAddressReuse(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	s := archive(t, Config{})
	original := sample(at, 1)
	identity := model.IdentityResolution{Input: "ip:10.0.0.1", ResolvedID: "web", Method: model.IdentityIPTemporal, Source: "network", SourceType: "prometheus", CollectedAt: at, TimeBasis: "collection-window",
		Candidates: []model.IdentityCandidate{{ID: "web", Source: "inventory", SourceType: "docker", Since: at.Add(-time.Hour), CollectedAt: at}}}
	original.Nodes[0].Identity = []model.IdentityResolution{identity}
	original.Edges[0].Identity = &model.ConnectionIdentity{From: []model.IdentityResolution{identity}, To: []model.IdentityResolution{{Input: "db", ResolvedID: "db", Method: model.IdentityName, Source: "network", SourceType: "prometheus", TimeBasis: "not-applicable"}}}
	id := save(t, s, original)
	newOwner := sample(at.Add(time.Minute), 2)
	newOwner.Nodes[0].ID = "new-owner"
	newOwner.Edges[0].From = "new-owner"
	save(t, s, newOwner)
	restarted := archive(t, s.cfg)
	replay, err := restarted.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Nodes[0].Identity, replay.Nodes[0].Identity) || !reflect.DeepEqual(original.Edges[0].Identity, replay.Edges[0].Identity) {
		t.Fatal("replay re-resolved historical identity against a newer address owner")
	}
}
