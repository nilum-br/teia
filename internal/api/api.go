// Package api serves the current snapshot over HTTP: the JSON API used by
// the Backstage and Grafana plugins, Grafana Node Graph shaped endpoints,
// and a small built-in UI for local use.
package api

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/nilum-br/teia/internal/export"
	"github.com/nilum-br/teia/internal/model"
)

//go:embed ui
var uiFiles embed.FS

// Snapshotter is the part of the engine the API needs.
type Snapshotter interface {
	Snapshot() model.Snapshot
}

// Handler returns the HTTP handler for the API and the UI.
func Handler(eng Snapshotter) http.Handler {
	mux := http.NewServeMux()
	snap := func() model.Snapshot { return eng.Snapshot() }

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/snapshot", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, snap())
	})
	mux.HandleFunc("GET /api/v1/graph", func(w http.ResponseWriter, _ *http.Request) {
		s := snap()
		writeJSON(w, map[string]any{"generatedAt": s.GeneratedAt, "nodes": s.Nodes, "edges": s.Edges})
	})
	mux.HandleFunc("GET /api/v1/findings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, snap().Findings)
	})
	mux.HandleFunc("GET /api/v1/coverage", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, snap().Coverage)
	})
	mux.HandleFunc("GET /api/v1/sources", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, snap().Sources)
	})
	// Flow matrix for firewall and security reviews: ?lang=pt-BR&declared=true
	mux.HandleFunc("GET /api/v1/export/matrix.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="teia-flow-matrix.csv"`)
		opts := export.MatrixOptions{Lang: r.URL.Query().Get("lang"), DeclaredOnly: r.URL.Query().Get("declared") == "true"}
		if err := export.Matrix(w, snap(), opts); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Shapes the Grafana Node Graph panel understands (through the Infinity
	// data source, until the teia Grafana plugin exists).
	mux.HandleFunc("GET /api/v1/grafana/nodes", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, grafanaNodes(snap()))
	})
	mux.HandleFunc("GET /api/v1/grafana/edges", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, grafanaEdges(snap()))
	})
	mux.HandleFunc("GET /api/v1/grafana/findings", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, grafanaFindings(snap()))
	})

	ui, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServerFS(ui))
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// Colours follow the UI: state is carried by colour and dash, never motion.
var (
	visibilityColor = map[model.Visibility]string{
		model.VisibilityFull:    "#3f9e6e",
		model.VisibilityPartial: "#d39b2a",
		model.VisibilityNone:    "#8a8f98",
	}
	stateColor = map[model.EdgeState]string{
		model.EdgeOK:    "#8a8f98",
		model.EdgeGhost: "#d9822b",
		model.EdgeStale: "#b0b4ba",
	}
)

type grafanaNode struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Subtitle      string `json:"subtitle"`
	MainStat      string `json:"mainstat"`
	SecondaryStat string `json:"secondarystat"`
	Color         string `json:"color"`
	DetailSources string `json:"detail__sources"`
}

type grafanaEdge struct {
	ID              string  `json:"id"`
	Source          string  `json:"source"`
	Target          string  `json:"target"`
	MainStat        string  `json:"mainstat"`
	SecondaryStat   string  `json:"secondarystat"`
	Color           string  `json:"color"`
	StrokeDasharray string  `json:"strokeDasharray,omitempty"`
	Thickness       float64 `json:"thickness"`
	DetailState     string  `json:"detail__state"`
	DetailSources   string  `json:"detail__sources"`
}

func grafanaNodes(s model.Snapshot) []grafanaNode {
	findings := map[string]int{}
	for _, f := range s.Findings {
		findings[f.Subject]++
	}
	out := make([]grafanaNode, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		secondary := ""
		if c := findings[n.ID]; c > 0 {
			secondary = fmt.Sprintf("%d finding(s)", c)
		}
		out = append(out, grafanaNode{
			ID:            n.ID,
			Title:         n.ID,
			Subtitle:      string(n.Kind),
			MainStat:      string(n.Visibility),
			SecondaryStat: secondary,
			Color:         visibilityColor[n.Visibility],
			DetailSources: strings.Join(n.Sources, ", "),
		})
	}
	return out
}

func grafanaEdges(s model.Snapshot) []grafanaEdge {
	out := make([]grafanaEdge, 0, len(s.Edges))
	for _, e := range s.Edges {
		ge := grafanaEdge{
			ID:            e.Key(),
			Source:        e.From,
			Target:        e.To,
			MainStat:      string(e.State),
			Color:         stateColor[e.State],
			Thickness:     1,
			DetailState:   string(e.State),
			DetailSources: strings.Join(e.Sources, ", "),
		}
		if e.RequestRate > 0 {
			ge.MainStat = fmt.Sprintf("%.2f req/s", e.RequestRate)
			ge.SecondaryStat = fmt.Sprintf("%.1f%% errors", e.ErrorRate*100)
			ge.Thickness = 2
		}
		switch e.State {
		case model.EdgeGhost:
			ge.StrokeDasharray = "6 4"
		case model.EdgeStale:
			ge.StrokeDasharray = "2 4"
		}
		out = append(out, ge)
	}
	return out
}

type grafanaFinding struct {
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Title    string `json:"title"`
	NextStep string `json:"nextStep"`
}

func grafanaFindings(s model.Snapshot) []grafanaFinding {
	out := make([]grafanaFinding, 0, len(s.Findings))
	for _, f := range s.Findings {
		next := ""
		if f.Fix != nil {
			next = f.Fix.Summary
		}
		out = append(out, grafanaFinding{
			Severity: string(f.Severity), Kind: string(f.Kind),
			Subject: f.Subject, Title: f.Title, NextStep: next,
		})
	}
	return out
}
