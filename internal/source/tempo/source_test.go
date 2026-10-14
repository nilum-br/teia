package tempo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

// One trace: web (server) -> web client -> orders (server) -> orders client to
// postgres (no server span) ; plus a span kind given as a number. The client
// spans last 20 ms (times as strings) and 5 ms (times as numbers).
const trace = `{"trace":{"resourceSpans":[
 {"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"web"}}]},
  "scopeSpans":[{"spans":[
   {"spanId":"A","kind":"SPAN_KIND_SERVER","attributes":[{"key":"http.request.method","value":{"stringValue":"GET"}}]},
   {"spanId":"B","parentSpanId":"A","kind":"SPAN_KIND_CLIENT","attributes":[{"key":"http.request.method","value":{"stringValue":"GET"}},{"key":"peer.service","value":{"stringValue":"orders"}}],"startTimeUnixNano":"1000000000","endTimeUnixNano":"1020000000"}]}]},
 {"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"orders"}}]},
  "scopeSpans":[{"spans":[
   {"spanId":"C","parentSpanId":"B","kind":2},
   {"spanId":"D","parentSpanId":"C","kind":"SPAN_KIND_CLIENT","attributes":[{"key":"db.system.name","value":{"stringValue":"postgresql"}},{"key":"peer.service","value":{"stringValue":"orders-db"}}],"startTimeUnixNano":2000000000,"endTimeUnixNano":2005000000}]}]}]}}`

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

// platform fakes Tempo with services s0..s<n-1>. Each search answers
// "limit" traces of that service, each trace is one server span of it.
type platform struct {
	services int
	mu       sync.Mutex
	searched []string // services searched, in order
	limits   map[string]bool
}

func (p *platform) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/search/tag/"):
			var values []string
			for i := range p.services {
				values = append(values, fmt.Sprintf(`{"value":"s%d"}`, i))
			}
			fmt.Fprintf(w, `{"tagValues":[%s]}`, strings.Join(values, ","))
		case r.URL.Path == "/api/search":
			svc := strings.Split(r.URL.Query().Get("q"), `"`)[1]
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			p.mu.Lock()
			p.searched = append(p.searched, svc)
			p.limits[r.URL.Query().Get("limit")] = true
			p.mu.Unlock()
			var ids []string
			for i := range limit {
				ids = append(ids, fmt.Sprintf(`{"traceID":"%s-%d"}`, svc, i))
			}
			fmt.Fprintf(w, `{"traces":[%s]}`, strings.Join(ids, ","))
		case strings.HasPrefix(r.URL.Path, "/api/v2/traces/"):
			svc, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/v2/traces/"), "-")
			fmt.Fprintf(w, `{"trace":{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":%q}}]},
				"scopeSpans":[{"spans":[{"spanId":"A","kind":"SPAN_KIND_SERVER"}]}]}]}}`, svc)
		default:
			http.NotFound(w, r)
		}
	}))
}

// round runs one Discover and returns the services it searched, in order.
func (p *platform) round(t *testing.T, src *Source) []string {
	t.Helper()
	p.mu.Lock()
	p.searched, p.limits = nil, map[string]bool{}
	p.mu.Unlock()
	if _, err := src.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.searched
}

func TestSamplingSharesTheCap(t *testing.T) {
	cases := []struct {
		services, maxTraces, perService int
		wantLimit                       string
	}{
		{3, 400, 20, "20"}, // room for everyone: TracesPerService each
		{3, 30, 20, "10"},  // an even share of the cap
		{10, 4, 20, "1"},   // more services than the cap: at least one each
	}
	for _, c := range cases {
		p := &platform{services: c.services}
		srv := p.serve()
		p.round(t, New("tempo", Config{URL: srv.URL, MaxTraces: c.maxTraces, TracesPerService: c.perService}))
		srv.Close()
		p.mu.Lock()
		limits := p.limits
		p.mu.Unlock()
		if len(limits) != 1 || !limits[c.wantLimit] {
			t.Errorf("%+v: searched with limits %v, want %s", c, limits, c.wantLimit)
		}
	}
}

func TestSamplingRotatesAndStopsAtTheCap(t *testing.T) {
	p := &platform{services: 10}
	srv := p.serve()
	defer srv.Close()
	src := New("tempo", Config{URL: srv.URL, MaxTraces: 4})

	seen := map[string]bool{}
	for i, want := range []string{"s0 s1 s2 s3", "s4 s5 s6 s7", "s8 s9 s0 s1"} {
		got := p.round(t, src)
		if strings.Join(got, " ") != want {
			t.Errorf("round %d searched %v, want %s (stop at the cap, start where the last round stopped)", i+1, got, want)
		}
		for _, s := range got {
			seen[s] = true
		}
	}
	if len(seen) != 10 {
		t.Errorf("after three rounds only %d of 10 services were sampled", len(seen))
	}
}

func TestTraceFetchFailures(t *testing.T) {
	cases := []struct {
		name         string
		failing      map[string]bool
		wantErr      bool
		wantDegraded bool
		wantEdges    int
	}{
		{"none", nil, false, false, 2},
		{"some", map[string]bool{"t2": true}, false, true, 2},
		{"all", map[string]bool{"t1": true, "t2": true}, true, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id := strings.TrimPrefix(r.URL.Path, "/api/v2/traces/")
				switch {
				case strings.HasPrefix(r.URL.Path, "/api/v2/search/tag/"):
					fmt.Fprint(w, `{"tagValues":[{"value":"web"},{"value":"orders"}]}`)
				case r.URL.Path == "/api/search":
					fmt.Fprint(w, `{"traces":[{"traceID":"t1"},{"traceID":"t2"}]}`)
				case c.failing[id]:
					http.Error(w, "trace store unavailable", http.StatusInternalServerError)
				default:
					fmt.Fprint(w, trace)
				}
			}))
			defer srv.Close()
			obs, err := New("tempo", Config{URL: srv.URL}).Discover(context.Background())
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, want error: %v", err, c.wantErr)
			}
			if obs.Degraded != c.wantDegraded || len(obs.Edges) != c.wantEdges {
				t.Errorf("degraded=%v edges=%d, want %v and %d (%s)", obs.Degraded, len(obs.Edges), c.wantDegraded, c.wantEdges, obs.Message)
			}
			if c.wantDegraded && !strings.Contains(obs.Message, "1 of 2 traces could not be fetched") {
				t.Errorf("message: %s", obs.Message)
			}
		})
	}
}

func TestPeerOf(t *testing.T) {
	cases := []struct {
		attrs map[string]string
		id    string
		kind  model.NodeKind
	}{
		{map[string]string{"peer.service": "payments", "server.address": "10.1.2.3"}, "payments", model.KindUnknown},
		{map[string]string{"server.address": "payments.shop.svc.cluster.local"}, "payments", model.KindUnknown},
		{map[string]string{"net.peer.name": "payments.shop.svc:8080"}, "payments", model.KindUnknown},
		{map[string]string{"server.address": "api.stripe.com"}, "api.stripe.com", model.KindUnknown},
		{map[string]string{"server.address": "10.1.2.3"}, "ip:10.1.2.3", model.KindUnknown},
		{map[string]string{"server.address": "[fd00::7]:443"}, "ip:fd00::7", model.KindUnknown},
		// db.namespace/db.name name a database inside a server, not a node:
		// "orders" would merge with the orders service.
		{map[string]string{"db.system.name": "postgresql", "db.namespace": "orders", "server.address": "orders-db.shop.svc.cluster.local"}, "orders-db", model.KindDatabase},
		{map[string]string{"db.system": "postgresql", "db.name": "orders"}, "postgresql", model.KindDatabase},
		{map[string]string{"db.system": "redis", "server.address": "10.1.2.9"}, "ip:10.1.2.9", model.KindCache},
	}
	for _, c := range cases {
		if id, kind := peerOf(c.attrs); id != c.id || kind != c.kind {
			t.Errorf("peerOf(%v) = %q, %q; want %q, %q", c.attrs, id, kind, c.id, c.kind)
		}
	}
}

func TestURLPasswordStaysOutOfMessages(t *testing.T) {
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer denied.Close()
	withPassword := strings.Replace(denied.URL, "http://", "http://teia:s3cret@", 1)
	obs, err := New("tempo", Config{URL: withPassword}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d := obs.Findings[0].Detail; strings.Contains(d, "s3cret") || !strings.Contains(d, "teia:xxxxx@") {
		t.Errorf("finding leaks or drops the URL: %s", d)
	}
	_, err = New("tempo", Config{URL: "http://teia:s3cret@tempo.example:port"}).Discover(context.Background())
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error leaks the password: %v", err)
	}
}

func TestDiscoverMeasuresCallDurations(t *testing.T) {
	srv := fakeTempo(t)
	defer srv.Close()
	obs, err := New("tempo", Config{URL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"web->orders": 20, "orders->orders-db": 5}
	for _, e := range obs.Edges {
		key := e.From + "->" + e.To
		if e.LatencyP50Ms != want[key] || e.LatencyP95Ms != want[key] {
			t.Errorf("%s p50/p95 = %v/%v ms, want %v (the caller's span)", key, e.LatencyP50Ms, e.LatencyP95Ms, want[key])
		}
	}
}

func TestMessagesHaveNoLatency(t *testing.T) {
	edges := map[string]*source.EdgeObservation{}
	durations := map[string][]float64{}
	pairTrace([]span{
		{id: "P", service: "orders", kind: "producer", dur: 3 * time.Millisecond, attrs: map[string]string{"messaging.system": "kafka", "peer.service": "order-events"}},
		{id: "C", parent: "P", service: "email-worker", kind: "consumer", dur: 900 * time.Millisecond},
	}, edges, map[string]bool{}, map[string]model.NodeKind{}, durations)
	if len(edges) == 0 {
		t.Fatal("the message should still be an edge")
	}
	if len(durations) != 0 {
		t.Errorf("waiting in a queue is not latency, got %v", durations)
	}
}

func TestQuantile(t *testing.T) {
	sample := make([]float64, 20)
	for i := range sample {
		sample[19-i] = float64(i + 1) // unsorted on purpose
	}
	if got := quantile(sample, 0.5); got != 10 {
		t.Errorf("p50 = %v, want 10", got)
	}
	if got := quantile(sample, 0.95); got != 19 {
		t.Errorf("p95 = %v, want 19", got)
	}
	if got := quantile([]float64{7}, 0.95); got != 7 {
		t.Errorf("one value is every quantile, got %v", got)
	}
	if got := quantile(nil, 0.5); got != 0 {
		t.Errorf("no sample, no latency, got %v", got)
	}
	if sample[0] != 20 {
		t.Error("quantile must not reorder its input")
	}
}
