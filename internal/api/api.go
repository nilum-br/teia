// Package api serves the current snapshot over HTTP: the JSON API used by
// the Backstage and Grafana plugins, Grafana Node Graph shaped endpoints,
// and a small built-in UI for local use.
package api

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/nilum-br/teia/internal/export"
	"github.com/nilum-br/teia/internal/model"
)

//go:embed ui
var uiFiles embed.FS

// Snapshotter is the part of the engine the API needs.
type Snapshotter interface {
	Snapshot() model.Snapshot
	// SnapshotJSON is the last snapshot encoded once for every stream.
	SnapshotJSON() []byte
	Changes() []model.Change
	// Subscribe delivers the JSON of every new snapshot.
	Subscribe() (<-chan []byte, func())
}

// DefaultMaxStreams caps concurrent /api/v1/stream clients when Options
// does not say otherwise.
const DefaultMaxStreams = 100

// Options secures and tunes the HTTP API.
type Options struct {
	// Token, when set, is required on every endpoint but /healthz: as
	// "Authorization: Bearer <token>", or as the cookie a browser gets by
	// opening /?token=<token> once (EventSource cannot send headers).
	Token string
	// AllowedOrigins are the browser origins (scheme://host[:port]) that may
	// read /api/v1/* from another site, such as Grafana streaming straight
	// from teia. "*" allows any origin. Credentials are never allowed.
	AllowedOrigins []string
	// MaxStreams caps concurrent /api/v1/stream clients; zero means
	// DefaultMaxStreams.
	MaxStreams int
	// Log receives what clients cannot see (encoding errors); nil discards.
	Log *slog.Logger
}

// Handler returns the HTTP handler for the API and the UI.
func Handler(eng Snapshotter, opts Options) http.Handler {
	if opts.MaxStreams <= 0 {
		opts.MaxStreams = DefaultMaxStreams
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	mux := http.NewServeMux()
	snap := func() model.Snapshot { return eng.Snapshot() }
	send := func(w http.ResponseWriter, v any) { writeJSON(w, opts.Log, v) }

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /metrics", metricsHandler(snap))
	mux.HandleFunc("GET /api/v1/changes", func(w http.ResponseWriter, _ *http.Request) {
		send(w, eng.Changes())
	})
	mux.HandleFunc("GET /api/v1/stream", streamHandler(eng, opts.MaxStreams))
	mux.HandleFunc("GET /api/v1/snapshot", func(w http.ResponseWriter, _ *http.Request) {
		send(w, snap())
	})
	mux.HandleFunc("GET /api/v1/graph", func(w http.ResponseWriter, _ *http.Request) {
		s := snap()
		send(w, map[string]any{"generatedAt": s.GeneratedAt, "nodes": s.Nodes, "edges": s.Edges})
	})
	mux.HandleFunc("GET /api/v1/findings", func(w http.ResponseWriter, _ *http.Request) {
		send(w, snap().Findings)
	})
	mux.HandleFunc("GET /api/v1/coverage", func(w http.ResponseWriter, _ *http.Request) {
		send(w, snap().Coverage)
	})
	mux.HandleFunc("GET /api/v1/sources", func(w http.ResponseWriter, _ *http.Request) {
		send(w, snap().Sources)
	})
	// Flow matrix for firewall and security reviews: ?lang=pt-BR&declared=true
	mux.HandleFunc("GET /api/v1/export/matrix.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="teia-flow-matrix.csv"`)
		mopts := export.MatrixOptions{Lang: r.URL.Query().Get("lang"), DeclaredOnly: r.URL.Query().Get("declared") == "true"}
		if err := export.Matrix(w, snap(), mopts); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Shapes the Grafana Node Graph panel understands (through the Infinity
	// data source, until the teia Grafana plugin exists).
	mux.HandleFunc("GET /api/v1/grafana/nodes", func(w http.ResponseWriter, _ *http.Request) {
		send(w, grafanaNodes(snap()))
	})
	mux.HandleFunc("GET /api/v1/grafana/edges", func(w http.ResponseWriter, _ *http.Request) {
		send(w, grafanaEdges(snap()))
	})
	mux.HandleFunc("GET /api/v1/grafana/findings", func(w http.ResponseWriter, _ *http.Request) {
		send(w, grafanaFindings(snap()))
	})
	mux.HandleFunc("GET /api/v1/grafana/sources", func(w http.ResponseWriter, _ *http.Request) {
		send(w, grafanaSources(snap()))
	})
	mux.HandleFunc("GET /api/v1/grafana/changes", func(w http.ResponseWriter, _ *http.Request) {
		changes := eng.Changes()
		if changes == nil {
			changes = []model.Change{}
		}
		send(w, changes)
	})

	ui, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServerFS(ui))

	// CORS answers preflights before auth: browsers never send credentials
	// on them.
	return allowOrigins(requireToken(mux, opts.Token), opts.AllowedOrigins, opts.Token != "")
}

// writeJSON encodes v before sending anything, so a value JSON cannot
// represent (NaN, Inf) becomes a 500 instead of a 200 with an empty body.
func writeJSON(w http.ResponseWriter, log *slog.Logger, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Error("encoding response", "err", err)
		writeError(w, http.StatusInternalServerError, "teia could not encode this response; the server log has the details")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(buf.Bytes())
}

// writeError sends {"error": msg} with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	data, _ := json.Marshal(map[string]string{"error": msg})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}
