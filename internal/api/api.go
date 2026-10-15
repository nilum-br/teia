// Package api serves the current snapshot over HTTP: the JSON API used by
// the Grafana panel (and the planned Backstage plugin), Grafana Node Graph
// shaped endpoints, and a small built-in UI for local use.
package api

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nilum-br/teia/internal/export"
	"github.com/nilum-br/teia/internal/history"
	"github.com/nilum-br/teia/internal/journey"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/slice"
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

// Slicer is implemented by engines that can rebuild the last round from the
// calls carrying a selection of business dimensions (?slice=journey:x).
type Slicer interface {
	Sliced(sel slice.Selector) model.Snapshot
}

// DefaultMaxStreams caps concurrent /api/v1/stream clients when Options
// does not say otherwise.
const DefaultMaxStreams = 100

// Options secures and tunes the HTTP API.
type Options struct {
	History *history.Store
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
	// UI serves the built-in UI from these files instead of the copy
	// embedded at build time, such as os.DirFS("internal/api/ui") while
	// editing it: changes show on the next browser reload. Nil means the
	// embedded copy.
	UI fs.FS
	// Version is reported to MCP clients as the server's version.
	Version string
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
	registerHistory(mux, opts)
	send := func(w http.ResponseWriter, v any) { writeJSON(w, opts.Log, v) }
	// snapOf is the whole snapshot, or with ?slice= only the selected calls.
	// It answers the error itself and reports false when the request is bad.
	snapOf := func(w http.ResponseWriter, r *http.Request) (model.Snapshot, bool) {
		sel, err := slice.Parse(r.URL.Query().Get("slice"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return model.Snapshot{}, false
		}
		if sel == nil {
			return eng.Snapshot(), true
		}
		slicer, ok := eng.(Slicer)
		if !ok {
			writeError(w, http.StatusNotImplemented, "this server cannot slice snapshots by dimension")
			return model.Snapshot{}, false
		}
		return slicer.Sliced(sel), true
	}
	snap := func() model.Snapshot { return eng.Snapshot() }

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /metrics", metricsHandler(snap))
	mux.HandleFunc("GET /api/v1/changes", func(w http.ResponseWriter, _ *http.Request) {
		send(w, eng.Changes())
	})
	mux.HandleFunc("GET /api/v1/stream", streamHandler(eng, opts.MaxStreams))
	mux.HandleFunc("GET /api/v1/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := snapOf(w, r); ok {
			send(w, s)
		}
	})
	mux.HandleFunc("GET /api/v1/graph", func(w http.ResponseWriter, r *http.Request) {
		s, ok := snapOf(w, r)
		if !ok {
			return
		}
		send(w, map[string]any{"generatedAt": s.GeneratedAt, "timeMode": s.TimeMode, "nodes": s.Nodes, "edges": s.Edges, "journeys": s.Journeys, "slice": s.Slice})
	})
	mux.HandleFunc("GET /api/v1/dimensions", func(w http.ResponseWriter, _ *http.Request) {
		dims := snap().Dimensions
		if dims == nil {
			dims = map[string][]string{}
		}
		send(w, dims)
	})
	mux.HandleFunc("GET /api/v1/journeys", func(w http.ResponseWriter, r *http.Request) {
		s, ok := snapOf(w, r)
		if !ok {
			return
		}
		key := r.URL.Query().Get("key")
		contract := s.Journeys
		if contract == nil || (key != "" && key != contract.Key) {
			contract = journey.Build(s, key)
		}
		send(w, contract)
	})
	mux.HandleFunc("GET /api/v1/findings", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := snapOf(w, r); ok {
			send(w, s.Findings)
		}
	})
	mux.HandleFunc("GET /api/v1/coverage", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := snapOf(w, r); ok {
			send(w, s.Coverage)
		}
	})
	mux.HandleFunc("GET /api/v1/sources", func(w http.ResponseWriter, _ *http.Request) {
		send(w, snap().Sources)
	})
	// Flow matrix for firewall and security reviews: ?lang=pt-BR&declared=true
	mux.HandleFunc("GET /api/v1/export/matrix.csv", func(w http.ResponseWriter, r *http.Request) {
		s, ok := snapOf(w, r)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="teia-flow-matrix.csv"`)
		mopts := export.MatrixOptions{Lang: r.URL.Query().Get("lang"), DeclaredOnly: r.URL.Query().Get("declared") == "true"}
		if err := export.Matrix(w, s, mopts); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Flat frames for Grafana: the teia panel (nilum-teia-panel) reads them
	// through dashboard queries (the Infinity data source in the lab, as
	// there is no teia data source yet), and nodes and edges also fit the
	// built-in Node Graph panel.
	mux.HandleFunc("GET /api/v1/grafana/nodes", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := snapOf(w, r); ok {
			send(w, grafanaNodes(s))
		}
	})
	mux.HandleFunc("GET /api/v1/grafana/edges", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := snapOf(w, r); ok {
			send(w, grafanaEdges(s))
		}
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

	// The Model Context Protocol, for agents: the same data, read-only.
	mcpH := mcpHandler(eng, opts.Version, opts.Log)
	for _, method := range []string{"POST", "GET", "DELETE"} {
		mux.Handle(method+" /mcp", mcpH)
	}

	mux.Handle("GET /", uiHandler(opts.UI))

	// CORS answers preflights before auth: browsers never send credentials
	// on them.
	return allowOrigins(requireToken(mux, opts.Token), opts.AllowedOrigins, opts.Token != "")
}

// uiHandler serves the built-in UI: the embedded copy, or files when set.
// Both go with "Cache-Control: no-cache": the browser asks again on every
// load, so a new teia never runs an old teia-ui.js. Embedded files carry
// their content hash as ETag (they have no modification time), so a file
// that did not change costs a 304, not a download.
func uiHandler(files fs.FS) http.Handler {
	var etags map[string]string
	if files == nil {
		files, _ = fs.Sub(uiFiles, "ui")
		etags = contentETags(files)
	}
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" || strings.HasSuffix(name, "/") {
			name += "index.html"
		}
		if tag, ok := etags[name]; ok {
			w.Header().Set("ETag", tag)
		}
		fileServer.ServeHTTP(w, r)
	})
}

// contentETags hashes every file of fsys, keyed by its path.
func contentETags(fsys fs.FS) map[string]string {
	etags := map[string]string{}
	_ = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		etags[path] = `"` + hex.EncodeToString(sum[:10]) + `"`
		return nil
	})
	return etags
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
