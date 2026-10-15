package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func TestGrafanaFramesPreserveNodeAndEdgeEvidenceLinks(t *testing.T) {
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	link := model.EvidenceLink{Label: "Search traces", Kind: "traces", URL: "https://grafana.example/explore?panes=%7B%7D", Description: "Service/time search; not an exact trace", PeriodFrom: at.Add(-time.Minute), PeriodTo: at}
	s := model.Snapshot{GeneratedAt: at, Nodes: []model.Node{{ID: "a", Links: []model.EvidenceLink{link}}}, Edges: []model.Edge{{From: "a", To: "b", Links: []model.EvidenceLink{link}}}}
	for _, packed := range []string{grafanaNodes(s)[0].Links, grafanaEdges(s)[0].Links} {
		var links []model.EvidenceLink
		if err := json.Unmarshal([]byte(packed), &links); err != nil || len(links) != 1 || links[0] != link {
			t.Fatalf("link context lost in frame: %s (%v)", packed, err)
		}
	}
}
