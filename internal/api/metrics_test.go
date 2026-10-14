package api

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func TestWriteMetrics(t *testing.T) {
	var buf bytes.Buffer
	writeMetrics(&buf, model.Snapshot{
		GeneratedAt: time.Unix(1700000000, 0),
		Coverage:    model.Coverage{Visibility: 0.5},
		Nodes:       []model.Node{{ID: `we"ird`, Kind: model.KindService, Visibility: model.VisibilityFull}},
		Edges:       []model.Edge{{From: "a", To: "b", State: model.EdgeGhost, Sources: []string{"x", "y"}}},
		Findings:    []model.Finding{{Rule: "r", Kind: model.FindingDrift, Severity: model.SeverityWarning}, {Rule: "r", Kind: model.FindingDrift, Severity: model.SeverityWarning}},
		Sources:     []model.SourceStatus{{Name: "s", Type: "t", State: model.SourceError}},
	})
	out := buf.String()
	for _, want := range []string{
		`teia_coverage_ratio{scope="visibility"} 0.5`,
		`teia_node_info{id="we\"ird",kind="service",visibility="full"} 1`,
		`teia_edge_info{from="a",to="b",state="ghost",protocol="",sources="x,y"} 1`,
		`teia_findings{rule="r",kind="drift",severity="warning"} 2`,
		`teia_source_up{source="s",type="t",state="error"} 0`,
		`teia_last_round_timestamp_seconds 1700000000`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
}
