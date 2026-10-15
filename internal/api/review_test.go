package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/history"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/review"
)

func proposalEngine() *fakeEngine {
	e := newFakeEngine()
	e.snap.GeneratedAt = time.Now().UTC()
	e.snap.Nodes = append(e.snap.Nodes, model.Node{ID: "db", Kind: model.KindDatabase})
	e.snap.Edges = []model.Edge{{From: "orders", To: "db", Observed: true, Protocol: "postgresql", Port: 5432, Sources: []string{"tempo"}}}
	return e
}

func proposalPost(h http.Handler, target, body string, auth bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if auth {
		r.Header.Set("Authorization", "Bearer secret")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestProposalRoutesPinSourceAndNeverApplyAcceptance(t *testing.T) {
	e := proposalEngine()
	h := Handler(e, Options{Token: "secret"})
	body := `{"from":"orders","to":"db","rationale":"store orders","generatedAt":"` + e.snap.GeneratedAt.Format(time.RFC3339Nano) + `"}`
	if w := proposalPost(h, "/api/v1/review/proposals", body, false); w.Code != http.StatusUnauthorized {
		t.Fatal("proposal bypassed authentication", w.Code)
	}
	w := proposalPost(h, "/api/v1/review/proposals", body, true)
	var p review.Proposal
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Source.InputKind != "current-discovery" || len(p.Files) != 3 || e.snap.Edges[0].Declared {
		t.Fatalf("unexpected proposal: %d %s", w.Code, w.Body.String())
	}
	w = proposalPost(h, "/api/v1/review/proposals?format=bundle", body, true)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("bad bundle response: %d %s", w.Code, w.Body.String())
	}
	if z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len())); err != nil || len(z.File) != 3 {
		t.Fatal("bad bundle", err)
	}
	e.snap.GeneratedAt = e.snap.GeneratedAt.Add(time.Nanosecond)
	if w = proposalPost(h, "/api/v1/review/proposals", body, true); w.Code != http.StatusConflict {
		t.Fatal("stale evidence silently accepted", w.Code)
	}
}

func TestProposalHistoricalSourceAndInvalidInputs(t *testing.T) {
	e := proposalEngine()
	store, err := history.Open(history.Config{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Save(e.snap)
	if err != nil {
		t.Fatal(err)
	}
	at := e.snap.GeneratedAt.Format(time.RFC3339Nano)
	e.snap.GeneratedAt = e.snap.GeneratedAt.Add(time.Hour)
	h := Handler(e, Options{History: store})
	body := `{"from":"orders","to":"db","rationale":"store orders","generatedAt":"` + at + `","historyId":"` + id + `"}`
	w := proposalPost(h, "/api/v1/review/proposals", body, false)
	var p review.Proposal
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Source.HistoryID != id || p.Source.InputKind != "stored-history" || p.Source.TimeMode != "historical" {
		t.Fatalf("wrong historical source: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{}`, http.StatusBadRequest},
		{body + `{}`, http.StatusBadRequest},
		{strings.Replace(body, `"from":"orders"`, `"from":"orders","snapshot":{}`, 1), http.StatusBadRequest},
		{strings.Replace(body, `"from":"orders"`, `"from":"orders","inputKind":"current-discovery"`, 1), http.StatusBadRequest},
		{strings.Replace(body, id, "../invalid", 1), http.StatusNotFound},
		{strings.Replace(body, "store orders", "", 1), http.StatusUnprocessableEntity},
		{strings.Repeat(" ", 17<<10) + body, http.StatusRequestEntityTooLarge},
	} {
		w = proposalPost(h, "/api/v1/review/proposals", tc.body, false)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
	}
	if w = proposalPost(Handler(e, Options{}), "/api/v1/review/proposals", body, false); w.Code != http.StatusNotFound {
		t.Fatal("disabled history did not reject historical source", w.Code)
	}
}

func TestProposalPreflightAllowsJSONOnlyOnProposalRoute(t *testing.T) {
	h := Handler(proposalEngine(), Options{Token: "secret", AllowedOrigins: []string{"https://grafana.example"}})
	for _, path := range []string{"/api/v1/review/proposals", "/api/v1/snapshot"} {
		r := httptest.NewRequest(http.MethodOptions, path, nil)
		r.Header.Set("Origin", "https://grafana.example")
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNoContent || strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "POST") != (path == "/api/v1/review/proposals") {
			t.Fatal("proposal CORS scope regressed", w.Header())
		}
		if path == "/api/v1/review/proposals" && !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Content-Type") {
			t.Fatal("JSON preflight cannot succeed")
		}
	}
}
