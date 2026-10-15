package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/nilum-br/teia/internal/history"
	"github.com/nilum-br/teia/internal/model"
)

func registerHistory(mux *http.ServeMux, opts Options) {
	for name, rows := range map[string]func(model.Snapshot) any{
		"nodes":    func(s model.Snapshot) any { return grafanaNodes(s) },
		"edges":    func(s model.Snapshot) any { return grafanaEdges(s) },
		"findings": func(s model.Snapshot) any { return grafanaFindings(s) },
		"sources":  func(s model.Snapshot) any { return grafanaSources(s) },
		"coverage": func(s model.Snapshot) any { return historicalCoverage(s) },
		"changes":  func(s model.Snapshot) any { return historicalChanges(s) },
	} {
		mux.HandleFunc("GET /api/v1/history/{id}/grafana/"+name, func(w http.ResponseWriter, r *http.Request) {
			snapshot, err := opts.History.Snapshot(r.PathValue("id"))
			if errors.Is(err, history.ErrNotFound) {
				writeError(w, http.StatusNotFound, "historical snapshot is unavailable; this does not mean no traffic")
				return
			}
			if err != nil {
				opts.Log.Error("reading historical frame", "err", err)
				writeError(w, http.StatusInternalServerError, "historical snapshot could not be read; check the server log")
				return
			}
			writeJSON(w, opts.Log, rows(snapshot))
		})
	}
	mux.HandleFunc("GET /api/v1/history", func(w http.ResponseWriter, r *http.Request) {
		var from, to time.Time
		for key, target := range map[string]*time.Time{"from": &from, "to": &to} {
			if raw := r.URL.Query().Get(key); raw != "" {
				parsed, err := time.Parse(time.RFC3339Nano, raw)
				if err != nil {
					writeError(w, http.StatusBadRequest, key+" must be an RFC3339 timestamp with timezone")
					return
				}
				*target = parsed
			}
		}
		if (!from.IsZero() && to.IsZero()) || (from.IsZero() && !to.IsZero()) || (!from.IsZero() && from.After(to)) {
			writeError(w, http.StatusBadRequest, "provide both from and to, with from at or before to")
			return
		}
		result, err := opts.History.List(from, to)
		if err != nil {
			opts.Log.Error("listing observation history", "err", err)
			writeError(w, http.StatusInternalServerError, "history could not be read; check the server log")
			return
		}
		writeJSON(w, opts.Log, result)
	})
	mux.HandleFunc("GET /api/v1/history/{id}", func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := opts.History.Snapshot(r.PathValue("id"))
		if errors.Is(err, history.ErrNotFound) {
			writeError(w, http.StatusNotFound, "historical snapshot is unavailable or history is disabled; this does not mean no traffic")
			return
		}
		if err != nil {
			opts.Log.Error("reading historical snapshot", "err", err)
			writeError(w, http.StatusInternalServerError, "historical snapshot could not be read; check the server log")
			return
		}
		writeJSON(w, opts.Log, snapshot)
	})
}
