package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
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
// A search takes delay, and fails for the services in failing.
type platform struct {
	services int
	delay    time.Duration
	failing  map[string]bool
	mu       sync.Mutex
	searched []string // services searched, in the order they arrived
	limits   map[string]bool
	inflight int
	most     int          // the most searches running at once
	fetched  []url.Values // the query of every trace fetch
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
			if p.limits == nil {
				p.limits = map[string]bool{}
			}
			p.limits[r.URL.Query().Get("limit")] = true
			p.inflight++
			p.most = max(p.most, p.inflight)
			p.mu.Unlock()
			defer func() {
				p.mu.Lock()
				p.inflight--
				p.mu.Unlock()
			}()
			select {
			case <-time.After(p.delay):
			case <-r.Context().Done():
				return
			}
			if p.failing[svc] {
				http.Error(w, "search failed", http.StatusInternalServerError)
				return
			}
			var ids []string
			for i := range limit {
				ids = append(ids, fmt.Sprintf(`{"traceID":"%s-%d"}`, svc, i))
			}
			fmt.Fprintf(w, `{"traces":[%s]}`, strings.Join(ids, ","))
		case strings.HasPrefix(r.URL.Path, "/api/v2/traces/"):
			p.mu.Lock()
			p.fetched = append(p.fetched, r.URL.Query())
			p.mu.Unlock()
			svc, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/v2/traces/"), "-")
			fmt.Fprintf(w, `{"trace":{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":%q}}]},
				"scopeSpans":[{"spans":[{"spanId":"A","kind":"SPAN_KIND_SERVER"}]}]}]}}`, svc)
		default:
			http.NotFound(w, r)
		}
	}))
}

// round runs one Discover and returns the services it searched, sorted:
// searches run concurrently, so they arrive in any order.
func (p *platform) round(t *testing.T, src *Source) []string {
	t.Helper()
	got, _ := p.roundWithin(t, src, 0)
	return got
}

// roundWithin is round with the round's timeout (none when zero), and also
// returns the observation.
func (p *platform) roundWithin(t *testing.T, src *Source, timeout time.Duration) ([]string, source.Observation) {
	t.Helper()
	p.mu.Lock()
	p.searched, p.limits, p.fetched = nil, map[string]bool{}, nil
	p.mu.Unlock()
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	obs, err := src.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return sortedServices(p.searched), obs
}

// sortedServices sorts s0..s<n> by number.
func sortedServices(names []string) []string {
	out := slices.Clone(names)
	slices.SortFunc(out, func(a, b string) int {
		x, _ := strconv.Atoi(strings.TrimPrefix(a, "s"))
		y, _ := strconv.Atoi(strings.TrimPrefix(b, "s"))
		return x - y
	})
	return out
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
	for i, want := range []string{"s0 s1 s2 s3", "s4 s5 s6 s7", "s0 s1 s8 s9"} {
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

// A Tempo that answers each search in a fraction of a second still needs
// minutes to search a thousand services one at a time. The round keeps what
// it had time for, says so, and the next round carries on from there.
func TestSamplingKeepsWhatTheRoundHadTimeFor(t *testing.T) {
	p := &platform{services: 40, delay: 100 * time.Millisecond}
	srv := p.serve()
	defer srv.Close()
	src := New("tempo", Config{URL: srv.URL, MaxTraces: 40})

	first, obs := p.roundWithin(t, src, 800*time.Millisecond)
	if len(first) == 0 || len(first) == 40 {
		t.Fatalf("searched %d services; want some, not all, within the round", len(first))
	}
	if len(obs.Nodes) == 0 || !obs.Degraded || !strings.Contains(obs.Message, "out of time") {
		t.Errorf("want the traces found so far, degraded, saying it ran out of time: nodes=%d degraded=%v %q", len(obs.Nodes), obs.Degraded, obs.Message)
	}
	if obs.Operations == nil || !obs.Operations.Sources[0].Truncated {
		t.Errorf("the operation sample must say it is truncated: %+v", obs.Operations)
	}
	second, _ := p.roundWithin(t, src, 800*time.Millisecond)
	if len(second) == 0 || slices.Contains(second, first[0]) {
		t.Errorf("second round searched %v after %v; want it to carry on with services not searched yet", second, first)
	}
}

// A service whose search fails is skipped, not retried first every round:
// before, one bad search failed the whole source, and the next round started
// from the same service, so the services after it were never sampled.
func TestSamplingMovesOnAfterAFailedSearch(t *testing.T) {
	p := &platform{services: 6, failing: map[string]bool{"s1": true}}
	srv := p.serve()
	defer srv.Close()
	src := New("tempo", Config{URL: srv.URL, MaxTraces: 3})

	first, obs := p.roundWithin(t, src, 0)
	if !obs.Degraded || !strings.Contains(obs.Message, "1 of 4 searches failed") {
		t.Errorf("want a degraded round naming the failed search: %v %q", obs.Degraded, obs.Message)
	}
	if strings.Join(first, " ") != "s0 s1 s2 s3" {
		t.Errorf("first round searched %v; want s0..s3 (s1 failed, s3 filled the cap)", first)
	}
	if second := p.round(t, src); strings.Join(second, " ") != "s0 s4 s5" {
		t.Errorf("second round searched %v; want s4, s5 and then s0", second)
	}
}

func TestSamplingFailsWhenEverySearchFails(t *testing.T) {
	p := &platform{services: 2, failing: map[string]bool{"s0": true, "s1": true}}
	srv := p.serve()
	defer srv.Close()
	if _, err := New("tempo", Config{URL: srv.URL}).Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "every search failed") {
		t.Errorf("err = %v; want every search failed", err)
	}
}

func TestSearchesRunConcurrentlyAndBounded(t *testing.T) {
	p := &platform{services: 12, delay: 50 * time.Millisecond}
	srv := p.serve()
	defer srv.Close()
	p.round(t, New("tempo", Config{URL: srv.URL, MaxTraces: 12}))
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.most < 2 || p.most > searchConcurrency {
		t.Errorf("at most %d searches ran at once; want between 2 and %d", p.most, searchConcurrency)
	}
}

// A trace fetched by ID without a time range makes Tempo look through every
// block it keeps. The fetch covers the search window and one window before
// it, where the first spans of a trace found in the window can start.
func TestTraceFetchesAreBoundedToTheWindow(t *testing.T) {
	p := &platform{services: 2}
	srv := p.serve()
	defer srv.Close()
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	src := New("tempo", Config{URL: srv.URL, Window: 15 * time.Minute})
	src.now = func() time.Time { return at }
	p.round(t, src)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.fetched) == 0 {
		t.Fatal("no trace was fetched")
	}
	for _, q := range p.fetched {
		if q.Get("start") != strconv.FormatInt(at.Add(-30*time.Minute).Unix(), 10) || q.Get("end") != strconv.FormatInt(at.Unix(), 10) {
			t.Errorf("trace fetched with %v; want from one window before the search to its end", q)
		}
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

func TestDiscoverKeepsOriginalSampleTimesAndOneWindow(t *testing.T) {
	srv := fakeTempo(t)
	defer srv.Close()
	src := New("tempo", Config{URL: srv.URL})
	clock := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	calls := 0
	src.now = func() time.Time {
		calls++
		return clock.Add(time.Duration(calls) * time.Minute)
	}
	obs, err := src.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("the search window moved during the round: read clock %d times", calls)
	}
	want := map[string][2]int64{"web->orders": {1000000000, 1020000000}, "orders->orders-db": {2000000000, 2005000000}}
	for _, e := range obs.Edges {
		bounds := want[e.From+"->"+e.To]
		if e.SampleCount != 1 || e.ObservedFrom.UnixNano() != bounds[0] || e.ObservedAt.UnixNano() != bounds[1] {
			t.Errorf("old spans in a successful answer must not inherit collection time: %+v", e)
		}
	}
}

func TestSpanTimes(t *testing.T) {
	const start = int64(1792065600123456789)
	for _, tc := range []struct {
		name, start, end string
		valid            bool
		duration         time.Duration
	}{
		{"strings", `"1792065600123456789"`, `"1792065600123456799"`, true, 10},
		{"numbers", `1792065600123456789`, `1792065600123456799`, true, 10},
		{"zero duration", `1792065600123456789`, `1792065600123456789`, true, 0},
		{"missing start", ``, `1792065600123456799`, false, 0},
		{"missing end", `1792065600123456789`, ``, false, 0},
		{"null", `null`, `null`, false, 0},
		{"zero", `0`, `0`, false, 0},
		{"negative", `-1`, `10`, false, 0},
		{"reversed", `20`, `10`, false, 0},
		{"overflow", `1`, `18446744073709551615`, false, 0},
		{"fraction", `1.5`, `2.5`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sp := otlpSpan{StartTimeUnixNano: json.RawMessage(tc.start), EndTimeUnixNano: json.RawMessage(tc.end)}
			from, to := sp.times()
			if !tc.valid {
				if !from.IsZero() || !to.IsZero() {
					t.Fatalf("invalid times must remain unknown: %v, %v", from, to)
				}
			} else if from.UnixNano() != start || to.Sub(from) != tc.duration {
				t.Fatalf("timestamps lost precision: %v, %v", from, to)
			}
		})
	}
}

func TestSampleEnvelopeDoesNotHideUndatedCalls(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	old := span{id: "old", service: "web", kind: "client", startedAt: now.Add(-time.Hour), endedAt: now.Add(-time.Hour + time.Second),
		attrs: map[string]string{"peer.service": "orders"}}
	recent := old
	recent.id, recent.startedAt, recent.endedAt = "recent", now.Add(-time.Minute), now
	undated := old
	undated.id, undated.startedAt, undated.endedAt = "undated", time.Time{}, time.Time{}
	for _, tc := range []struct {
		name  string
		calls []span
		known bool
	}{
		{"old then recent", []span{old, recent}, true},
		{"recent then old", []span{recent, old}, true},
		{"missing first", []span{undated, old, recent}, false},
		{"missing middle", []span{old, undated, recent}, false},
		{"missing last", []span{old, recent, undated}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edges := map[string]*source.EdgeObservation{}
			for _, call := range tc.calls {
				pairTrace([]span{call}, edges, map[string]bool{}, map[string]model.NodeKind{}, map[string][]float64{})
			}
			e := edges["web->orders"]
			if e.SampleCount != len(tc.calls) {
				t.Fatalf("count must include undated calls: %+v", e)
			}
			if tc.known {
				if !e.ObservedFrom.Equal(old.startedAt) || !e.ObservedAt.Equal(recent.endedAt) {
					t.Errorf("sample envelope = %v - %v", e.ObservedFrom, e.ObservedAt)
				}
			} else if !e.ObservedFrom.IsZero() || !e.ObservedAt.IsZero() {
				t.Errorf("an undated call must prevent a false envelope: %+v", e)
			}
		})
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
