package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/review"
)

func TestReviewCLIRequestFilesAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	snapshotPath, requestPath := filepath.Join(dir, "snapshot.json"), filepath.Join(dir, "request.json")
	at := time.Date(2026, 10, 15, 12, 0, 0, 123456789, time.UTC)
	s := model.Snapshot{GeneratedAt: at, Nodes: []model.Node{{ID: "web"}, {ID: "db"}}, Edges: []model.Edge{{From: "web", To: "db", Observed: true}}}
	data, _ := json.Marshal(s)
	os.WriteFile(snapshotPath, data, 0o600)
	request := map[string]any{"from": "web", "to": "db", "rationale": "Store orders; $(never-run) `literal`", "generatedAt": at.Format(time.RFC3339Nano)}
	data, _ = json.Marshal(request)
	os.WriteFile(requestPath, data, 0o600)
	args := []string{"propose", "--snapshot", snapshotPath, "--request", requestPath}
	var out bytes.Buffer
	if err := reviewTo(args, &out); err != nil {
		t.Fatal(err)
	}
	var p review.Proposal
	if err := json.Unmarshal(out.Bytes(), &p); err != nil || p.Rationale != request["rationale"] || p.Source.InputKind != "provided-snapshot" {
		t.Fatal("CLI did not preserve the local request", err, out.String())
	}
	output := filepath.Join(dir, "proposal.zip")
	if err := reviewTo(append(args, "--format", "bundle", "--output", output), &out); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 3 {
		t.Fatal("missing bundle files")
	}
	z.Close()
	before, _ := os.ReadFile(output)
	if err := reviewTo(append(args, "--format", "bundle", "--output", output), &out); err == nil {
		t.Fatal("existing output was overwritten")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(before, after) {
		t.Fatal("existing file changed")
	}
	for _, extra := range [][]string{{"--from", "web"}, {"--format", "shell"}} {
		out.Reset()
		if err := reviewTo(append(args, extra...), &out); err == nil || out.Len() != 0 {
			t.Fatal("invalid flags wrote output")
		}
	}
	request["generatedAt"] = at.Add(time.Second).Format(time.RFC3339Nano)
	data, _ = json.Marshal(request)
	os.WriteFile(requestPath, data, 0o600)
	out.Reset()
	if err := reviewTo(args, &out); err == nil || !strings.Contains(err.Error(), "does not match") || out.Len() != 0 {
		t.Fatal("mismatched input was accepted", err)
	}
}
