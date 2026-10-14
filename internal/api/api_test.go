package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// fakeEngine serves a fixed snapshot and lets tests push updates.
type fakeEngine struct {
	snap    model.Snapshot
	updates chan []byte
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		snap:    model.Snapshot{Round: 1, GeneratedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Nodes: []model.Node{{ID: "orders"}}},
		updates: make(chan []byte, 1),
	}
}

func (f *fakeEngine) Snapshot() model.Snapshot { return f.snap }
func (f *fakeEngine) SnapshotJSON() []byte {
	data, _ := json.Marshal(f.snap)
	return data
}
func (f *fakeEngine) Changes() []model.Change            { return nil }
func (f *fakeEngine) Subscribe() (<-chan []byte, func()) { return f.updates, func() {} }

func get(h http.Handler, target string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUnencodableResponseIsAnError(t *testing.T) {
	eng := newFakeEngine()
	eng.snap.Coverage.Visibility = math.NaN()
	rec := get(Handler(eng, Options{}), "/api/v1/coverage", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body struct{ Error string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == "" {
		t.Errorf("want a JSON error body, got %q (%v)", rec.Body.String(), err)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type = %q", ct)
	}
}

func TestAuth(t *testing.T) {
	h := Handler(newFakeEngine(), Options{Token: "s3cret"})
	bearer := map[string]string{"Authorization": "Bearer s3cret"}
	tests := []struct {
		name   string
		target string
		header map[string]string
		want   int
	}{
		{"healthz stays open", "/healthz", nil, http.StatusNoContent},
		{"api needs the token", "/api/v1/snapshot", nil, http.StatusUnauthorized},
		{"api with the token", "/api/v1/snapshot", bearer, http.StatusOK},
		{"scheme is case-insensitive", "/api/v1/findings", map[string]string{"Authorization": "bearer s3cret"}, http.StatusOK},
		{"wrong token", "/api/v1/snapshot", map[string]string{"Authorization": "Bearer s3cre"}, http.StatusUnauthorized},
		{"basic auth is not a bearer token", "/api/v1/snapshot", map[string]string{"Authorization": "Basic czNjcmV0"}, http.StatusUnauthorized},
		{"metrics need the token", "/metrics", nil, http.StatusUnauthorized},
		{"metrics with the token", "/metrics", bearer, http.StatusOK},
		{"the UI needs it too", "/", nil, http.StatusUnauthorized},
		{"a wrong token in the URL", "/?token=nope", nil, http.StatusUnauthorized},
		{"the token in the URL only works on the UI", "/api/v1/snapshot?token=s3cret", nil, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(h, tt.target, tt.header)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body.String())
			}
			if rec.Code == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("401 without WWW-Authenticate")
			}
		})
	}
}

func TestAuthCookieForTheUI(t *testing.T) {
	h := Handler(newFakeEngine(), Options{Token: "s3cret"})
	rec := get(h, "/?token=s3cret&group=team", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "./?group=team" {
		t.Fatalf("status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookies = %+v", cookies)
	}
	if strings.Contains(cookies[0].Value, "s3cret") {
		t.Error("the cookie must not carry the token itself")
	}

	cookie := map[string]string{"Cookie": sessionCookie + "=" + cookies[0].Value}
	for _, target := range []string{"/", "/api/v1/snapshot", "/api/v1/changes"} {
		if rec := get(h, target, cookie); rec.Code != http.StatusOK {
			t.Errorf("%s with the cookie: status %d", target, rec.Code)
		}
	}
	if rec := get(h, "/api/v1/snapshot", map[string]string{"Cookie": sessionCookie + "=forged"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("forged cookie: status %d", rec.Code)
	}
	if rec := get(h, "/api/v1/snapshot", map[string]string{"Authorization": "Bearer " + cookies[0].Value}); rec.Code != http.StatusUnauthorized {
		t.Errorf("the cookie value must not work as a bearer token: status %d", rec.Code)
	}
}

func TestNoTokenMeansOpen(t *testing.T) {
	if rec := get(Handler(newFakeEngine(), Options{}), "/api/v1/snapshot", nil); rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestUIFromDisk(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "index.html")
	if err := os.WriteFile(index, []byte("<p>first draft</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := Handler(newFakeEngine(), Options{UI: os.DirFS(dir)})

	rec := get(h, "/", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "first draft") {
		t.Fatalf("status %d, body %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q: browsers must revalidate files served from disk", cc)
	}

	// An edit shows on the next request, with no restart.
	if err := os.WriteFile(index, []byte("<p>second draft</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := get(h, "/", nil); !strings.Contains(rec.Body.String(), "second draft") {
		t.Errorf("edit not served: %q", rec.Body.String())
	}
	// The directory replaces the embedded copy; it does not fall back to it.
	if rec := get(h, "/teia-ui.js", nil); rec.Code != http.StatusNotFound {
		t.Errorf("teia-ui.js: status %d, want 404", rec.Code)
	}
	// The API is unaffected.
	if rec := get(h, "/api/v1/snapshot", nil); rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "" {
		t.Errorf("api: status %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestUIFromDiskStillNeedsTheToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ui"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := Handler(newFakeEngine(), Options{UI: os.DirFS(dir), Token: "s3cret"})
	if rec := get(h, "/", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if rec := get(h, "/", map[string]string{"Authorization": "Bearer s3cret"}); rec.Code != http.StatusOK {
		t.Errorf("with the token: status = %d", rec.Code)
	}
}

func TestEmbeddedUIByDefault(t *testing.T) {
	rec := get(Handler(newFakeEngine(), Options{}), "/", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "TeiaUI.mount") {
		t.Fatalf("status %d, body %.80q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache: an upgraded teia must not run a cached teia-ui.js", cc)
	}
	h := Handler(newFakeEngine(), Options{})
	js := get(h, "/teia-ui.js", nil)
	tag := js.Header().Get("ETag")
	if js.Code != http.StatusOK || tag == "" {
		t.Fatalf("teia-ui.js: status %d, ETag %q", js.Code, tag)
	}
	if again := get(h, "/teia-ui.js", map[string]string{"If-None-Match": tag}); again.Code != http.StatusNotModified {
		t.Errorf("unchanged teia-ui.js: status %d, want 304", again.Code)
	}
	if page := get(h, "/", map[string]string{"If-None-Match": rec.Header().Get("ETag")}); page.Code != http.StatusNotModified {
		t.Errorf("unchanged page: status %d, want 304", page.Code)
	}
}

func TestCORS(t *testing.T) {
	const grafana = "https://grafana.example.com"
	tests := []struct {
		name      string
		origins   []string
		token     string
		method    string
		target    string
		header    map[string]string
		status    int
		allow     string // Access-Control-Allow-Origin
		allowHdrs string // Access-Control-Allow-Headers
	}{
		{name: "allowed origin", origins: []string{grafana}, method: http.MethodGet, target: "/api/v1/snapshot",
			header: map[string]string{"Origin": grafana}, status: http.StatusOK, allow: grafana},
		{name: "other origin gets no header", origins: []string{grafana}, method: http.MethodGet, target: "/api/v1/snapshot",
			header: map[string]string{"Origin": "https://evil.example.com"}, status: http.StatusOK},
		{name: "no cors configured", method: http.MethodGet, target: "/api/v1/snapshot",
			header: map[string]string{"Origin": grafana}, status: http.StatusOK},
		{name: "only the API", origins: []string{grafana}, method: http.MethodGet, target: "/healthz",
			header: map[string]string{"Origin": grafana}, status: http.StatusNoContent},
		{name: "wildcard when listed", origins: []string{"*"}, method: http.MethodGet, target: "/api/v1/findings",
			header: map[string]string{"Origin": "https://any.example.com"}, status: http.StatusOK, allow: "*"},
		{name: "preflight with auth", origins: []string{grafana}, token: "s3cret", method: http.MethodOptions, target: "/api/v1/stream",
			header: map[string]string{"Origin": grafana, "Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "authorization"},
			status: http.StatusNoContent, allow: grafana, allowHdrs: "Authorization"},
		{name: "preflight from another origin", origins: []string{grafana}, method: http.MethodOptions, target: "/api/v1/snapshot",
			header: map[string]string{"Origin": "https://evil.example.com", "Access-Control-Request-Method": "GET"}, status: http.StatusForbidden},
		{name: "a 401 is readable by the allowed origin", origins: []string{grafana}, token: "s3cret", method: http.MethodGet, target: "/api/v1/snapshot",
			header: map[string]string{"Origin": grafana}, status: http.StatusUnauthorized, allow: grafana},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Handler(newFakeEngine(), Options{AllowedOrigins: tt.origins, Token: tt.token})
			req := httptest.NewRequest(tt.method, tt.target, nil)
			for k, v := range tt.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.allow {
				t.Errorf("allow origin = %q, want %q", got, tt.allow)
			}
			if got := rec.Header().Get("Access-Control-Allow-Headers"); got != tt.allowHdrs {
				t.Errorf("allow headers = %q, want %q", got, tt.allowHdrs)
			}
			if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Error("credentials must never be allowed")
			}
		})
	}
}
