package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/source/promapi"
)

// queryLog answers every detection with a match, so every preset and probe
// runs, and keeps each query it was sent.
type queryLog struct {
	mu      sync.Mutex
	queries []string
}

func (l *queryLog) serve() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		l.mu.Lock()
		l.queries = append(l.queries, q)
		l.mu.Unlock()
		result := ""
		if strings.HasPrefix(q, "count(") {
			result = `{"metric":{},"value":[0,"1"]}`
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[%s]}}`, result)
	}))
}

// On a Mimir, Thanos or VictoriaMetrics that keeps several clusters, every
// query of a cluster's source, detection included, must read only that
// cluster's series: one unscoped query would show the sum of every cluster
// as this one's traffic.
func TestSelectorScopesEveryQuery(t *testing.T) {
	log := &queryLog{}
	srv := log.serve()
	defer srv.Close()
	if _, err := New("prometheus", Config{URL: srv.URL, Selector: map[string]string{"cluster": "east"}}).Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if len(log.queries) < len(Presets)+len(Probes) {
		t.Fatalf("only %d queries ran; want every preset and probe", len(log.queries))
	}
	for _, q := range log.queries {
		if !strings.Contains(q, `cluster="east"`) {
			t.Errorf("query sent without the selector: %s", q)
		}
	}
}

func TestNoSelectorLeavesQueriesAlone(t *testing.T) {
	log := &queryLog{}
	srv := log.serve()
	defer srv.Close()
	if _, err := New("prometheus", Config{URL: srv.URL}).Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, q := range log.queries {
		if strings.Contains(q, "cluster=") {
			t.Errorf("unscoped source changed a query: %s", q)
		}
	}
}

// Every query teia writes for a preset or a probe can take a selector, so a
// new one cannot quietly run unscoped on a shared backend.
func TestEveryPresetAndProbeQueryTakesASelector(t *testing.T) {
	scope := promapi.Scope{"cluster": "east"}
	check := func(what, q string) {
		t.Helper()
		got, err := scope.Apply(q, promapi.PromQL)
		if err != nil {
			t.Errorf("%s: %v\n%s", what, err, q)
			return
		}
		// Every metric the query names is read with the selector.
		for _, metric := range metricNames(q) {
			if !strings.Contains(got, metric+`{cluster="east"`) {
				t.Errorf("%s: %s is not scoped in\n%s", what, metric, got)
			}
		}
	}
	w := window{promql: "5m", seconds: 300, at: time.Unix(1700000000, 0)}
	for _, p := range Presets {
		check(p.Name+" detect", "count("+p.Detect+")")
		check(p.Name+" requests", p.Requests(w.promql))
		if p.Failures != nil {
			check(p.Name+" failures", p.Failures(w.promql))
		}
		if p.Latency != nil {
			check(p.Name+" latency", p.Latency(w.promql, 0.95))
		}
	}
	for _, p := range Probes {
		check(p.Name+" detect", "count("+p.Detect+")")
		for i, r := range p.Readings {
			check(fmt.Sprintf("%s reading %d", p.Name, i), r.Query(w))
			if r.Max != nil {
				check(fmt.Sprintf("%s reading %d max", p.Name, i), r.Max(w))
			}
			for j, q := range r.SampleTimes {
				check(fmt.Sprintf("%s reading %d time %d", p.Name, i, j), q)
			}
		}
	}
}

// metricNames lists the metric names a teia query reads: the identifiers
// followed by a selector or a range, a cheap check independent of Apply.
func metricNames(q string) []string {
	var out []string
	for i := 0; i < len(q); {
		j := i
		for j < len(q) && (q[j] == '_' || q[j] == ':' || (q[j] >= 'a' && q[j] <= 'z') || (q[j] >= 'A' && q[j] <= 'Z') || (j > i && q[j] >= '0' && q[j] <= '9')) {
			j++
		}
		if j > i && j < len(q) && (q[j] == '{' || q[j] == '[') && (i == 0 || (q[i-1] != '"' && q[i-1] != '.')) {
			out = append(out, q[i:j])
		}
		if j == i {
			j++
		}
		i = j
	}
	return out
}
