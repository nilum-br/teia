package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/history"
	"github.com/nilum-br/teia/internal/model"
)

func TestHistoryRoutesPreserveSnapshotAndRejectUnavailableRange(t *testing.T) {
	store, err := history.Open(history.Config{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	original := model.Snapshot{Round: 7, GeneratedAt: at, TimeMode: "current", Nodes: []model.Node{{ID: "web"}, {ID: "db"}}, Edges: []model.Edge{{From: "web", To: "db", Observed: true, Evidence: []model.Evidence{{Source: "tempo", ObservedAt: at.Add(-time.Minute), CollectedAt: at}}}}}
	original.Findings = []model.Finding{{ID: "example", Subject: "web", Title: "Example finding"}}
	original.Sources = []model.SourceStatus{{Name: "tempo"}}
	original.Changes = []model.Change{{Subject: "web", At: at, Message: "Original change"}}
	id, err := store.Save(original)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(newFakeEngine(), Options{History: store, Token: "secret"})
	auth := map[string]string{"Authorization": "Bearer secret"}
	if rec := get(h, "/api/v1/history", nil); rec.Code != http.StatusUnauthorized {
		t.Fatal("history bypasses API authentication")
	}
	for _, path := range []string{"/api/v1/history?from=bad&to=bad", "/api/v1/history?from=" + at.Format(time.RFC3339), "/api/v1/history?from=" + at.Format(time.RFC3339) + "&to=" + at.Add(-time.Hour).Format(time.RFC3339)} {
		if rec := get(h, path, auth); rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid period %s: %d", path, rec.Code)
		}
	}
	rec := get(h, "/api/v1/history?from="+at.Add(-time.Minute).Format(time.RFC3339)+"&to="+at.Add(time.Minute).Format(time.RFC3339), auth)
	var list history.Listing
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Snapshots) != 1 || list.Unavailable || list.Coverage != "discrete-snapshots" {
		t.Fatalf("list = %s (%v)", rec.Body.String(), err)
	}
	rec = get(h, "/api/v1/history/"+id, auth)
	var replay model.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &replay); err != nil || replay.TimeMode != "historical" || replay.Round != 7 || replay.History.ID != id || !replay.Edges[0].Evidence[0].ObservedAt.Equal(at.Add(-time.Minute)) {
		t.Fatalf("replay = %s (%v)", rec.Body.String(), err)
	}
	for _, frame := range []string{"nodes", "edges", "findings", "sources", "coverage", "changes"} {
		rec = get(h, "/api/v1/history/"+id+"/grafana/"+frame, auth)
		var rows []map[string]any
		if frame == "coverage" {
			var row map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
		} else if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 || rows[0]["timeMode"] != "historical" || rows[0]["snapshotAt"] != at.Format(time.RFC3339) {
			t.Fatalf("historical frame = %s (%v)", rec.Body.String(), err)
		}
		if (frame == "nodes" || frame == "coverage") && !strings.Contains(rows[0]["history"].(string), id) {
			t.Fatal("historical frame lost shareable snapshot ID")
		}
		if frame == "changes" && rows[0]["at"] != at.Format(time.RFC3339) {
			t.Fatal("historical change lost its original timestamp")
		}
		for _, row := range rows {
			if row["historyId"] != id {
				t.Fatalf("%s row does not identify historical capture: %+v", frame, row)
			}
		}
	}
	if rec := get(h, "/api/v1/history/invalid", auth); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "does not mean no traffic") {
		t.Fatalf("missing history = %s", rec.Body.String())
	}
}

func TestDisabledHistoryIsExplicit(t *testing.T) {
	rec := get(Handler(newFakeEngine(), Options{}), "/api/v1/history", nil)
	var list history.Listing
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || list.Enabled || !list.Unavailable || list.Snapshots == nil {
		t.Fatalf("disabled history = %s (%v)", rec.Body.String(), err)
	}
}

func TestEmptyHistoricalSnapshotKeepsCaptureInCoverage(t *testing.T) {
	store, err := history.Open(history.Config{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	id, err := store.Save(model.Snapshot{GeneratedAt: at, TimeMode: "current"})
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(newFakeEngine(), Options{History: store})
	rec := get(h, "/api/v1/history/"+id+"/grafana/coverage", nil)
	var row map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil || row["historyId"] != id || row["timeMode"] != "historical" {
		t.Fatalf("empty replay coverage = %s (%v)", rec.Body.String(), err)
	}
	var metadata model.HistoryContext
	if err := json.Unmarshal([]byte(row["history"].(string)), &metadata); err != nil || metadata.ID != id || metadata.Availability != "stored-snapshot" {
		t.Fatalf("empty replay metadata = %+v (%v)", metadata, err)
	}
	if rec := get(h, "/api/v1/history/"+id+"/grafana/changes", nil); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty changes = %s", rec.Body.String())
	}
}
