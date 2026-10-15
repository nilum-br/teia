package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/nilum-br/teia/internal/history"
	"github.com/nilum-br/teia/internal/review"
)

func registerReview(mux *http.ServeMux, eng Snapshotter, opts Options) {
	mux.HandleFunc("POST /api/v1/review/proposals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "proposal requests require application/json")
			return
		}
		format := r.URL.Query().Get("format")
		if format != "" && format != "json" && format != "bundle" {
			writeError(w, http.StatusBadRequest, "format must be json or bundle")
			return
		}
		var request struct {
			review.ProposalRequest
			GeneratedAt time.Time `json:"generatedAt"`
			HistoryID   string    `json:"historyId,omitempty"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&request); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "proposal request exceeds 16 KiB")
			} else {
				writeError(w, http.StatusBadRequest, "invalid proposal JSON; provide from, to, rationale, generatedAt and optional historyId")
			}
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			writeError(w, http.StatusBadRequest, "provide one proposal JSON object")
			return
		}
		snapshot := eng.Snapshot()
		request.InputKind = "current-discovery"
		if request.HistoryID != "" {
			request.InputKind = "stored-history"
			snapshot, err = opts.History.Snapshot(request.HistoryID)
			if errors.Is(err, history.ErrNotFound) {
				writeError(w, http.StatusNotFound, "selected historical snapshot is unavailable")
				return
			}
			if err != nil {
				opts.Log.Error("reading proposal source snapshot", "err", err)
				writeError(w, http.StatusInternalServerError, "historical snapshot could not be read")
				return
			}
		}
		if request.GeneratedAt.IsZero() {
			writeError(w, http.StatusBadRequest, "generatedAt is required to identify the observation being reviewed")
			return
		}
		if !snapshot.GeneratedAt.Equal(request.GeneratedAt) {
			writeError(w, http.StatusConflict, "snapshot changed; refresh the selected communication and review its evidence again")
			return
		}
		proposal, err := review.Propose(snapshot, request.ProposalRequest)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if format == "bundle" {
			var body bytes.Buffer
			if err := review.Bundle(&body, proposal); err != nil {
				opts.Log.Error("encoding proposal bundle", "err", err)
				writeError(w, http.StatusInternalServerError, "proposal bundle could not be encoded")
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", `attachment; filename="teia-contract-proposal.zip"`)
			_, _ = w.Write(body.Bytes())
			return
		}
		writeJSON(w, opts.Log, proposal)
	})
}
