package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/api"
	"github.com/nilum-br/teia/internal/model"
)

func TestFailOn(t *testing.T) {
	findings := []model.Finding{
		{Kind: model.FindingDrift, Severity: model.SeverityWarning},
		{Kind: model.FindingBlindSpot, Severity: model.SeverityInfo},
	}
	tests := []struct {
		flag    string
		badFlag bool
		fails   string // substring of the error, empty when it passes
	}{
		{flag: ""},
		{flag: "critical"},
		{flag: "warning", fails: "1 finding(s) at or above warning (most urgent: warning)"},
		{flag: "info", fails: "2 finding(s) at or above info (most urgent: warning)"},
		{flag: "warn", badFlag: true},
		{flag: "Critical", badFlag: true},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			threshold, err := parseFailOn(tt.flag)
			if tt.badFlag {
				if err == nil {
					t.Fatalf("--fail-on %q should be rejected", tt.flag)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			err = failOnFindings(findings, threshold)
			switch {
			case tt.fails == "" && err != nil:
				t.Errorf("unexpected failure: %v", err)
			case tt.fails != "" && (err == nil || !strings.Contains(err.Error(), tt.fails)):
				t.Errorf("error = %v, want %q", err, tt.fails)
			}
		})
	}
}

func TestUIFiles(t *testing.T) {
	if ui, err := uiFiles(""); ui != nil || err != nil {
		t.Errorf("no --ui-dir means the embedded UI: got %v, %v", ui, err)
	}
	empty := t.TempDir()
	if _, err := uiFiles(empty); err == nil || !strings.Contains(err.Error(), "internal/api/ui") {
		t.Errorf("a directory without index.html should be refused with a hint, got %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>from disk</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	ui, err := uiFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.Handler(fixedEngine{}, api.Options{UI: ui}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "from disk") {
		t.Errorf("status %d, body %q", resp.StatusCode, body)
	}
}

// fixedEngine is the smallest api.Snapshotter.
type fixedEngine struct{ data []byte }

func (f fixedEngine) Snapshot() model.Snapshot           { return model.Snapshot{} }
func (f fixedEngine) SnapshotJSON() []byte               { return f.data }
func (f fixedEngine) Changes() []model.Change            { return nil }
func (f fixedEngine) Subscribe() (<-chan []byte, func()) { return make(chan []byte), func() {} }

func TestShutdownEndsStreams(t *testing.T) {
	data, _ := json.Marshal(model.Snapshot{Round: 1})
	srv := newServer("127.0.0.1:0", api.Handler(fixedEngine{data: data}, api.Options{}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/api/v1/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := bufio.NewReader(resp.Body).ReadString('\n'); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown waited for the open stream: %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("shutdown took %s", took)
	}
}
