package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/nilum-br/teia/internal/slice"
)

// streamRetryAfter is what a client over the stream cap is told to wait.
const streamRetryAfter = "30"

// streamHandler pushes every new snapshot as a Server-Sent Event, so the UI
// (and anything else) follows the platform live without polling:
//
//	event: snapshot
//	data: {...}
//
// The engine encodes each snapshot once and every stream sends those bytes.
// At most max streams are open at once, and each ends with its request
// context, which the server cancels on shutdown.
func streamHandler(eng Snapshotter, max int) http.HandlerFunc {
	slots := make(chan struct{}, max)
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming unsupported")
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", streamRetryAfter)
			writeError(w, http.StatusServiceUnavailable, "too many open streams; retry later or poll /api/v1/snapshot")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // nginx: do not buffer

		// ?slice= streams the selected calls of every round instead.
		sel, err := slice.Parse(r.URL.Query().Get("slice"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slicer, _ := eng.(Slicer)
		if sel != nil && slicer == nil {
			writeError(w, http.StatusNotImplemented, "this server cannot slice snapshots by dimension")
			return
		}
		encode := func(data []byte) []byte {
			if sel == nil || len(data) == 0 {
				return data
			}
			out, err := json.Marshal(slicer.Sliced(sel))
			if err != nil {
				return nil
			}
			return out
		}

		updates, stop := eng.Subscribe()
		defer stop()

		send := func(data []byte) bool {
			for _, part := range [][]byte{[]byte("event: snapshot\ndata: "), data, []byte("\n\n")} {
				if _, err := w.Write(part); err != nil {
					return false
				}
			}
			flusher.Flush()
			return true
		}
		if data := encode(eng.SnapshotJSON()); len(data) > 0 && !send(data) {
			return
		}
		// Headers go out now even before the first round, so clients know
		// the stream is open.
		flusher.Flush()

		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case data := <-updates:
				if data = encode(data); len(data) > 0 && !send(data) {
					return
				}
			case <-keepalive.C:
				if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
