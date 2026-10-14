package tempo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

// One trace: web (server) -> web client -> orders (server) -> orders client to
// postgres (no server span) ; plus a span kind given as a number.
const trace = `{"trace":{"resourceSpans":[
 {"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"web"}}]},
  "scopeSpans":[{"spans":[
   {"spanId":"A","kind":"SPAN_KIND_SERVER","attributes":[{"key":"http.request.method","value":{"stringValue":"GET"}}]},
   {"spanId":"B","parentSpanId":"A","kind":"SPAN_KIND_CLIENT","attributes":[{"key":"http.request.method","value":{"stringValue":"GET"}},{"key":"peer.service","value":{"stringValue":"orders"}}]}]}]},
 {"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"orders"}}]},
  "scopeSpans":[{"spans":[
   {"spanId":"C","parentSpanId":"B","kind":2},
   {"spanId":"D","parentSpanId":"C","kind":"SPAN_KIND_CLIENT","attributes":[{"key":"db.system.name","value":{"stringValue":"postgresql"}},{"key":"peer.service","value":{"stringValue":"orders-db"}}]}]}]}]}}`

func fakeTempo(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/search/tag/resource.service.name/values"):
			fmt.Fprint(w, `{"tagValues":[{"type":"string","value":"web"},{"type":"string","value":"orders"}]}`)
		case r.URL.Path == "/api/search":
			fmt.Fprint(w, `{"traces":[{"traceID":"t1"}]}`)
		case r.URL.Path == "/api/v2/traces/t1":
			fmt.Fprint(w, trace)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestDiscoverPairsSpans(t *testing.T) {
	srv := fakeTempo(t)
	defer srv.Close()
	obs, err := New("tempo", Config{URL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range obs.Edges {
		got[e.From+"->"+e.To] = e.Protocol
	}
	want := map[string]string{"web->orders": "http", "orders->orders-db": "postgresql"}
	if len(got) != len(want) {
		t.Fatalf("edges %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s protocol %q, want %q", k, got[k], v)
		}
	}
	kinds := map[string]model.NodeKind{}
	self := map[string]bool{}
	for _, n := range obs.Nodes {
		kinds[n.ID], self[n.ID] = n.Kind, n.SelfReported
	}
	if !self["web"] || !self["orders"] || self["orders-db"] || kinds["orders-db"] != model.KindDatabase {
		t.Errorf("nodes wrong: kinds=%v self=%v", kinds, self)
	}
}

func TestDeniedIsAPermissionFinding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	obs, err := New("tempo", Config{URL: srv.URL}).Discover(context.Background())
	if err != nil || len(obs.Findings) != 1 || obs.Findings[0].Kind != model.FindingPermission {
		t.Fatalf("got %+v, %v", obs, err)
	}
}
