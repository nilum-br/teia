package api

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/nilum-br/teia/internal/model"
)

// Limits of one cluster's roundCache. Answers are kept gzipped, so the
// frames of a map with thousands of connections take a few hundred KB.
const (
	maxCachedAnswers = 64
	maxCachedBytes   = 32 << 20
)

// roundCache keeps, for the current round, the gzipped encoding of answers
// that only change once a round: a Grafana dashboard refreshing every few
// seconds asks again for frames of several megabytes, and fifty open
// dashboards ask fifty times. Each answer is computed once per round, even
// when requests arrive together, and every entry goes when the round
// changes. Clients that do not take gzip get a fresh encoding.
type roundCache struct {
	mu      sync.Mutex
	round   int64
	id      string // the round's identity: its number and time
	entries map[string]*cachedAnswer
	bytes   int
}

type cachedAnswer struct {
	once sync.Once
	gz   []byte
	err  error
}

// roundOf identifies the round a snapshot belongs to; a slice of a round
// belongs to it too.
func roundOf(s model.Snapshot) string {
	return fmt.Sprintf("%d@%d", s.Round, s.GeneratedAt.UnixNano())
}

// entry returns the answer for key in round s, a new one when there is
// room, or nil when the cache is full or s is older than the cached round
// (a request that read the last round just before a new one arrived).
func (c *roundCache) entry(s model.Snapshot, key string) *cachedAnswer {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch id := roundOf(s); {
	case s.Round < c.round:
		return nil
	case id != c.id:
		c.round, c.id, c.entries, c.bytes = s.Round, id, map[string]*cachedAnswer{}, 0
	}
	if e, ok := c.entries[key]; ok {
		return e
	}
	if len(c.entries) >= maxCachedAnswers || c.bytes >= maxCachedBytes {
		return nil
	}
	e := &cachedAnswer{}
	c.entries[key] = e
	return e
}

func (c *roundCache) account(id string, n int) {
	c.mu.Lock()
	if c.id == id {
		c.bytes += n
	}
	c.mu.Unlock()
}

// serve answers r with compute's value for round s, from the cache when
// the client takes gzip.
func (c *roundCache) serve(w http.ResponseWriter, r *http.Request, log *slog.Logger, s model.Snapshot, compute func() any) {
	if !acceptsGzip(r) {
		writeJSON(w, log, compute())
		return
	}
	e := c.entry(s, r.URL.Path+"?"+r.URL.Query().Encode())
	if e == nil {
		writeJSON(w, log, compute())
		return
	}
	e.once.Do(func() {
		data, err := encodeJSON(compute())
		if err != nil {
			e.err = err
			return
		}
		e.gz = gzipped(data)
		c.account(roundOf(s), len(e.gz))
	})
	if e.err != nil {
		log.Error("encoding response", "err", e.err)
		writeError(w, http.StatusInternalServerError, "teia could not encode this response; the server log has the details")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Vary", "Accept-Encoding")
	_, _ = w.Write(e.gz)
}

// acceptsGzip reports whether the client takes a gzipped answer.
func acceptsGzip(r *http.Request) bool {
	for part := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			q := strings.ReplaceAll(params, " ", "")
			return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
		}
	}
	return false
}

func gzipped(data []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}
