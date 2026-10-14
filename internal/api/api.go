// Package api serves the current snapshot over HTTP: the JSON API used by
// the Backstage and Grafana plugins, Grafana Node Graph shaped endpoints,
// and a small built-in UI for local use.
package api

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/nilum-br/teia/internal/export"
	"github.com/nilum-br/teia/internal/model"
)

//go:embed ui
var uiFiles embed.FS

// Snapshotter is the part of the engine the API needs.
type Snapshotter interface {
	Snapshot() model.Snapshot
	Changes() []model.Change
	Subscribe() (<-chan model.Snapshot, func())
}

// Handler returns the HTTP handler for the API and the UI.
func Handler(eng Snapshotter) http.Handler {
	mux := http.NewServeMux()
	snap := func() model.Snapshot { return eng.Snapshot() }

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /metrics", metricsHandler(snap))
	mux.HandleFunc("GET /api/v1/changes", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, eng.Changes())
	})
	mux.HandleFunc("GET /api/v1/stream", streamHandler(eng))
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
	mux.HandleFunc("GET /api/v1/grafana/sources", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, grafanaSources(snap()))
	})
	mux.HandleFunc("GET /api/v1/grafana/changes", func(w http.ResponseWriter, _ *http.Request) {
		changes := eng.Changes()
		if changes == nil {
			changes = []model.Change{}
		}
		writeJSON(w, changes)
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
