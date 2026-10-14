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
