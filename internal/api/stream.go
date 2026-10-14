package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// streamHandler pushes every new snapshot as a Server-Sent Event, so the UI
// (and anything else) follows the platform live without polling:
//
//	event: snapshot
//	data: {...}
func streamHandler(eng Snapshotter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // nginx: do not buffer

		updates, stop := eng.Subscribe()
		defer stop()

		send := func(v any) bool {
			data, err := json.Marshal(v)
			if err != nil {
				return false
			}
			if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data); err != nil {
				return false
			}
			flusher.Flush()
			return true
		}
		if s := eng.Snapshot(); !s.GeneratedAt.IsZero() && !send(s) {
			return
		}
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case s := <-updates:
				if !send(s) {
					return
				}
			case <-keepalive.C:
				if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
