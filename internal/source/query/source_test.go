package query

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLokiQueryWithAddresses(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query().Get("query")
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[
			{"metric":{"service":"catalog","remote_addr":"172.18.0.9:51234"},"value":[0,"0.2"]},
			{"metric":{"service":"catalog","remote_addr":"[fd00::7]:443"},"value":[0,"0.1"]},
			{"metric":{"service":"catalog"},"value":[0,"3"]}]}}`)
	}))
	defer srv.Close()

	s := New("access-logs", Config{
		Dialect: Loki, URL: srv.URL,
		Query: `sum by (service, remote_addr) (rate({app="demo"} | json [$window]))`,
		From:  "remote_addr", FromAddress: true, To: "service", Protocol: "http",
	})
	obs, err := s.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/loki/api/v1/query" || !strings.Contains(gotQuery, "[5m]") {
		t.Errorf("sent %s ?query=%s", gotPath, gotQuery)
	}
	if len(obs.Edges) != 2 {
		t.Fatalf("want 2 edges (the series without remote_addr is skipped), got %+v", obs.Edges)
	}
	if obs.Edges[0].From != "ip:172.18.0.9" || obs.Edges[1].From != "ip:fd00::7" || obs.Edges[0].To != "catalog" {
		t.Errorf("addresses not normalised: %+v", obs.Edges)
	}
	if obs.Edges[0].Protocol != "http" || obs.Edges[0].RequestRate != 0.2 {
		t.Errorf("edge data wrong: %+v", obs.Edges[0])
	}
}

func TestDeniedQueryIsAPermissionFinding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	obs, err := New("q", Config{Dialect: Prometheus, URL: srv.URL, Query: "up", From: "a", To: "b"}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Findings) != 1 || obs.Findings[0].Rule != "query-denied" {
		t.Fatalf("unexpected: %+v", obs)
	}
}

func TestURLPasswordStaysOutOfMessages(t *testing.T) {
	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer denied.Close()
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	withPassword := func(u string) string { return strings.Replace(u, "http://", "http://teia:s3cret@", 1) }
	cfg := func(u string) Config { return Config{Dialect: Loki, URL: u, Query: "up", From: "a", To: "b"} }

	obs, err := New("q", cfg(withPassword(denied.URL))).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if title := obs.Findings[0].Title; strings.Contains(title, "s3cret") || !strings.Contains(title, "teia:xxxxx@") {
		t.Errorf("finding title leaks or drops the URL: %s", title)
	}
	for _, u := range []string{withPassword(closed.URL), "http://teia:s3cret@loki.example:port"} {
		_, err := New("q", cfg(u)).Discover(context.Background())
		if err == nil || strings.Contains(err.Error(), "s3cret") {
			t.Errorf("error leaks the password: %v", err)
		}
	}
}

// fakeQueries answers each query by the first fragment it contains.
func fakeQueries(t *testing.T, answers map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		for fragment, body := range answers {
			if strings.Contains(q, fragment) {
				fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[%s]}}`, body)
				return
			}
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
}

// The case that came from a real lab: app-b answers 503, app-a 502, more
// than half the calls fail, and the map must say so.
func TestErrorsAndLatencyQueries(t *testing.T) {
	srv := fakeQueries(t, map[string]string{
		`code=~"5.."`:        `{"metric":{"src":"app-a","dst":"app-b","code":"503"},"value":[0,"6"]}`,
		"histogram_quantile": `{"metric":{"src":"app-a","dst":"app-b"},"value":[0,"0.25"]}`,
		"http_requests_total[": `{"metric":{"src":"app-a","dst":"app-b","method":"GET"},"value":[0,"8"]},
			{"metric":{"src":"app-a","dst":"app-b","method":"POST"},"value":[0,"2"]}`,
	})
	defer srv.Close()
	obs, err := New("http", Config{
		Dialect: Prometheus, URL: srv.URL, From: "src", To: "dst",
		Query:   `sum by (src, dst, method) (rate(http_requests_total[$window]))`,
		Errors:  `sum by (src, dst, code) (rate(http_requests_total{code=~"5.."}[$window]))`,
		Latency: `histogram_quantile(0.95, sum by (src, dst, le) (rate(http_request_duration_seconds_bucket[$window])))`,
	}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Edges) != 1 {
		t.Fatalf("series of one pair make one edge: %+v", obs.Edges)
	}
	e := obs.Edges[0]
	if e.RequestRate != 10 || e.ErrorRate != 0.6 || e.NoErrors || e.LatencyP95Ms != 250 {
		t.Errorf("want 10 req/s, 60%% errors, p95 250 ms: %+v", e)
	}
}

func TestWithoutAnErrorsQueryFailuresAreUnknown(t *testing.T) {
	srv := fakeQueries(t, map[string]string{"rate(": `{"metric":{"src":"app-a","dst":"app-b"},"value":[0,"10"]}`})
	defer srv.Close()
	obs, err := New("http", Config{Dialect: Prometheus, URL: srv.URL, From: "src", To: "dst",
		Query: `sum by (src, dst) (rate(http_requests_total[$window]))`}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e := obs.Edges[0]; !e.NoErrors || e.ErrorRate != 0 {
		t.Errorf("no errors query means failures are not measured, not zero: %+v", e)
	}
}

func TestFailingErrorsQueryKeepsTheEdges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("query"), "5..") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"src":"a","dst":"b"},"value":[0,"3"]}]}}`)
	}))
	defer srv.Close()
	obs, err := New("http", Config{Dialect: Prometheus, URL: srv.URL, From: "src", To: "dst", Query: "rate(x[$window])",
		Errors: `rate(x{code=~"5.."}[$window])`}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Edges) != 1 || !obs.Edges[0].NoErrors || !obs.Degraded || !strings.Contains(obs.Message, "errors query failed") {
		t.Errorf("edges kept, failures unknown, the source says why: %+v %q", obs.Edges, obs.Message)
	}
}
