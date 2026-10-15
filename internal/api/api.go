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
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

// Cluster is one cluster's engine and observation history, when teia maps
// several. Each one answers on its own: nothing is merged across clusters.
type Cluster struct {
	Name    string
	Engine  Snapshotter
	History *history.Store
}

// Handler returns the HTTP handler for the API and the UI of one scope.
func Handler(eng Snapshotter, opts Options) http.Handler {
	return Clusters([]Cluster{{Engine: eng, History: opts.History}}, opts)
}

// Clusters serves several clusters. Every /api/v1 request reads one of them,
// chosen by ?cluster=name (the first when absent), and every answer says
// which: an unknown name is a 404, never another cluster's map.
// GET /api/v1/clusters lists them; /metrics labels each series with its
// cluster; the MCP tools take a cluster argument.
func Clusters(clusters []Cluster, opts Options) http.Handler {
	if opts.MaxStreams <= 0 {
		opts.MaxStreams = DefaultMaxStreams
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	// Streams of every cluster share one cap.
	slots := make(chan struct{}, opts.MaxStreams)
	byName := make(map[string]http.Handler, len(clusters))
	for _, c := range clusters {
		o := opts
		o.History = c.History
		byName[c.Name] = routes(c.Engine, o, slots)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /metrics", metricsHandler(func() []model.Snapshot {
		snaps := make([]model.Snapshot, 0, len(clusters))
		for _, c := range clusters {
			snaps = append(snaps, c.Engine.Snapshot())
		}
		return snaps
	}))
	mux.HandleFunc("GET /api/v1/clusters", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, opts.Log, clusterList(clusters))
	})
	dispatch := func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("cluster")
		if name == "" {
			name = clusters[0].Name
		}
		h, ok := byName[name]
		if !ok {
			writeError(w, http.StatusNotFound, unknownCluster(name, clusters))
			return
		}
		h.ServeHTTP(w, r)
	}
	mux.HandleFunc("GET /api/v1/", dispatch)
	mux.HandleFunc("POST /api/v1/", dispatch)

	// The Model Context Protocol, for agents: the same data, read-only.
	mcpH := mcpHandler(clusters, opts.Version, opts.Log)
	for _, method := range []string{"POST", "GET", "DELETE"} {
		mux.Handle(method+" /mcp", mcpH)
	}

	mux.Handle("GET /", uiHandler(opts.UI))

	// CORS answers preflights before auth: browsers never send credentials
	// on them.
	return allowOrigins(requireToken(mux, opts.Token), opts.AllowedOrigins, opts.Token != "")
}

// clusterSummary is one entry of GET /api/v1/clusters.
type clusterSummary struct {
	Name        string         `json:"name"`
	Round       int64          `json:"round"`
	GeneratedAt time.Time      `json:"generatedAt,omitzero"`
	Nodes       int            `json:"nodes"`
	Edges       int            `json:"edges"`
	Health      map[string]int `json:"health"`
	Sources     int            `json:"sources"`
	SourcesOK   int            `json:"sourcesOk"`
}

// clusterList summarises every cluster in configuration order; the first
// is the default. A single scope is one entry with an empty name.
func clusterList(clusters []Cluster) map[string]any {
	out := make([]clusterSummary, 0, len(clusters))
	for _, c := range clusters {
		s := c.Engine.Snapshot()
		sum := clusterSummary{Name: c.Name, Round: s.Round, GeneratedAt: s.GeneratedAt, Nodes: len(s.Nodes),
			Edges: len(s.Edges), Health: map[string]int{}, Sources: len(s.Sources)}
		for _, n := range s.Nodes {
			sum.Health[string(n.Health)]++
		}
		for _, src := range s.Sources {
			if src.State == model.SourceOK {
				sum.SourcesOK++
			}
		}
		out = append(out, sum)
	}
	return map[string]any{"default": clusters[0].Name, "clusters": out}
}

func unknownCluster(name string, clusters []Cluster) string {
	if len(clusters) == 1 && clusters[0].Name == "" {
		return fmt.Sprintf("no cluster %q: this teia maps a single scope (no clusters in its config)", name)
	}
	names := make([]string, len(clusters))
	for i, c := range clusters {
		names[i] = c.Name
	}
	return fmt.Sprintf("no cluster %q; GET /api/v1/clusters lists them (%s)", name, strings.Join(names, ", "))
}

// routes is the API of one cluster (or of the single scope).
func routes(eng Snapshotter, opts Options, slots chan struct{}) *http.ServeMux {
	mux := http.NewServeMux()
	registerHistory(mux, opts)
	registerReview(mux, eng, opts)
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

	mux.HandleFunc("GET /api/v1/changes", func(w http.ResponseWriter, _ *http.Request) {
		send(w, eng.Changes())
	})
	mux.HandleFunc("GET /api/v1/stream", streamHandler(eng, slots))
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
		send(w, map[string]any{"generatedAt": s.GeneratedAt, "timeMode": s.TimeMode, "nodes": s.Nodes, "edges": s.Edges, "journeys": s.Journeys, "operations": s.Operations, "slice": s.Slice})
	})
	mux.HandleFunc("GET /api/v1/dimensions", func(w http.ResponseWriter, _ *http.Request) {
		dims := snap().Dimensions
		if dims == nil {
			dims = map[string][]string{}
		}
		send(w, dims)
	})
	// With ?slice=, the traces whose entry carried the selection and their
	// calls that did; a source that cannot split them is marked excluded.
	mux.HandleFunc("GET /api/v1/operations", func(w http.ResponseWriter, r *http.Request) {
		s, ok := snapOf(w, r)
		if !ok {
			return
		}
		contract := s.Operations
		if contract == nil {
			contract = model.EmptyOperations()
		}
		send(w, contract)
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
			send(w, grafanaCoverageOf(s))
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
	mux.HandleFunc("GET /api/v1/grafana/findings", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := snapOf(w, r); ok {
			send(w, grafanaFindings(s))
		}
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

	return mux
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
