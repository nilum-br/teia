package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func cachedGet(t *testing.T, c *roundCache, s model.Snapshot, gz bool, compute func() any) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/grafana/edges?cluster=east", nil)
	if gz {
		r.Header.Set("Accept-Encoding", "gzip, deflate, br")
	}
	w := httptest.NewRecorder()
	c.serve(w, r, slog.New(slog.DiscardHandler), s, compute)
	return w
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// Fifty dashboards refreshing at once cost one encoding of the round, not
// fifty, and the next round is encoded afresh.
func TestRoundCacheEncodesOncePerRound(t *testing.T) {
	c := &roundCache{}
	var computed atomic.Int32
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	round := func(n int64) model.Snapshot {
		return model.Snapshot{Round: n, GeneratedAt: at.Add(time.Duration(n) * time.Second)}
	}
	value := func(n int64) func() any {
		return func() any { computed.Add(1); time.Sleep(time.Millisecond); return map[string]int64{"round": n} }
	}
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := cachedGet(t, c, round(1), true, value(1))
			if w.Header().Get("Content-Encoding") != "gzip" || gunzip(t, w.Body.Bytes()) != "{\"round\":1}\n" {
				t.Errorf("answer: %v %q", w.Header(), w.Body.String())
			}
		}()
	}
	wg.Wait()
	if n := computed.Load(); n != 1 {
		t.Fatalf("round 1 computed %d times for 50 requests; want once", n)
	}
	if w := cachedGet(t, c, round(2), true, value(2)); gunzip(t, w.Body.Bytes()) != "{\"round\":2}\n" || computed.Load() != 2 {
		t.Errorf("a new round must be computed again: %q after %d computations", gunzip(t, w.Body.Bytes()), computed.Load())
	}
	// A request that read round 1 just before round 2 arrived gets round 1,
	// computed for it alone: it neither evicts nor joins round 2.
	if w := cachedGet(t, c, round(1), true, value(1)); w.Body.String() != "{\"round\":1}\n" {
		t.Errorf("late request: %q", w.Body.String())
	}
	if w := cachedGet(t, c, round(2), true, value(2)); computed.Load() != 3 || gunzip(t, w.Body.Bytes()) != "{\"round\":2}\n" {
		t.Errorf("round 2 must stay cached after a late request: %d computations", computed.Load())
	}
}

func TestRoundCacheServesPlainJSONWithoutGzip(t *testing.T) {
	c := &roundCache{}
	var computed atomic.Int32
	s := model.Snapshot{Round: 1, GeneratedAt: time.Unix(1, 0)}
	for range 2 {
		w := cachedGet(t, c, s, false, func() any { computed.Add(1); return []string{"a"} })
		if w.Header().Get("Content-Encoding") != "" || w.Body.String() != "[\"a\"]\n" {
			t.Errorf("plain answer: %v %q", w.Header(), w.Body.String())
		}
	}
	if computed.Load() != 2 {
		t.Errorf("computed %d times; a client without gzip gets a fresh encoding each time", computed.Load())
	}
}

func TestRoundCacheStaysBounded(t *testing.T) {
	c := &roundCache{}
	s := model.Snapshot{Round: 1, GeneratedAt: time.Unix(1, 0)}
	for i := range maxCachedAnswers + 10 {
		c.entry(s, string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	if len(c.entries) != maxCachedAnswers {
		t.Errorf("%d entries; want at most %d", len(c.entries), maxCachedAnswers)
	}
	if e := c.entry(s, "one more"); e != nil {
		t.Error("a full cache must answer without caching")
	}
}

func TestAcceptsGzip(t *testing.T) {
	for header, want := range map[string]bool{
		"gzip": true, "gzip, deflate, br": true, "br;q=1.0, gzip;q=0.8": true, "GZIP": true,
		"": false, "deflate": false, "gzip;q=0": false, "identity": false,
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Encoding", header)
		if got := acceptsGzip(r); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}

// The cached, gzipped answers are the same JSON as the plain ones.
func TestCachedAnswersMatchPlainOnes(t *testing.T) {
	eng := newFakeEngine()
	eng.snap.Edges = []model.Edge{{From: "web", To: "orders", RequestRate: 3, Observed: true}}
	eng.snap.Findings = []model.Finding{{Rule: "r", Subject: "orders", Title: "t"}}
	h := Handler(eng, Options{})
	for _, path := range []string{"grafana/nodes", "grafana/edges", "grafana/findings", "coverage", "findings", "operations", "journeys", "graph"} {
		plain := get(h, "/api/v1/"+path, nil)
		zipped := get(h, "/api/v1/"+path, map[string]string{"Accept-Encoding": "gzip"})
		if plain.Code != http.StatusOK || zipped.Code != http.StatusOK {
			t.Fatalf("%s: status %d / %d", path, plain.Code, zipped.Code)
		}
		if zipped.Header().Get("Content-Encoding") != "gzip" || zipped.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("%s: headers %v", path, zipped.Header())
			continue
		}
		if got := gunzip(t, zipped.Body.Bytes()); got != plain.Body.String() {
			t.Errorf("%s: gzipped %q, plain %q", path, got, plain.Body.String())
		}
	}
}
