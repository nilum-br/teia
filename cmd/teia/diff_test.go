package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source/declared"
)

func TestDiffCLIReviewAndSnapshotModes(t *testing.T) {
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "snapshot.json")
	s := model.Snapshot{Nodes: []model.Node{{ID: "web"}, {ID: "db", Kind: model.KindDatabase}}, Edges: []model.Edge{{From: "web", To: "db", Observed: true, Protocol: "postgresql", Port: 5432}}}
	data, _ := json.Marshal(s)
	if err := os.WriteFile(snapshot, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := diffTo([]string{"--snapshot", snapshot, "--format", "flows", "--accept", "web->db=store orders"}, &out); err != nil {
		t.Fatal(err)
	}
	docs, err := declared.Parse(out.Bytes())
	if err != nil || len(docs) != 1 || docs[0].Spec.Egress[0].Port != 5432 {
		t.Fatalf("invalid generated artifact: %v %s", err, out.String())
	}
	baseline := filepath.Join(dir, "flows.yaml")
	if err := os.WriteFile(baseline, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := diffTo([]string{"--snapshot", snapshot, "--flows", baseline, "--format", "json"}, &out); err != nil {
		t.Fatal(err)
	}
	var report struct{ Candidates []any }
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || len(report.Candidates) != 0 {
		t.Fatalf("reconciled review still has additions: %v %s", err, out.String())
	}
	out.Reset()
	if err := diffTo([]string{"--before", snapshot, "--after", snapshot}, &out); err != nil || !strings.Contains(out.String(), "No topology") {
		t.Fatalf("identical snapshot diff failed: %v %s", err, out.String())
	}
	for _, args := range [][]string{{}, {"--snapshot", snapshot, "--before", snapshot}, {"--snapshot", snapshot, "--format", "flows"}, {"--snapshot", snapshot, "--accept", "web->db=why"}, {"--snapshot", snapshot, "--format", "flows", "--accept", "web->db="}} {
		out.Reset()
		if err := diffTo(args, &out); err == nil {
			t.Fatalf("invalid flags accepted: %v", args)
		}
		if out.Len() != 0 {
			t.Fatalf("invalid flags wrote artifact: %v", args)
		}
	}
	if err := os.WriteFile(snapshot, []byte(`{"status":"ok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSnapshot(snapshot); err == nil {
		t.Fatal("non-snapshot JSON accepted as empty graph")
	}
}
