// Command synth serves synthetic Prometheus and Tempo APIs for many clusters,
// so teia can be measured at sizes no lab cluster reaches.
//
// Cluster i answers under /c/<i>/: /c/<i>/api/v1/query (instant PromQL) and
// /c/<i>/tempo/... (tag values, search, traces). Every cluster runs the same
// services (svc-0000...) with its own traffic: what a central teia sees.
// Each service calls -fanout others; each call is one series per journey and
// tenant combination. Queries are answered by shape, not parsed: one with
// histogram_quantile is a p95 in seconds, one with status="error" is failed
// requests per second, anything else is requests per second.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

type topology struct {
	services, fanout, journeys, tenants int
}

func (t topology) name(i int) string { return fmt.Sprintf("svc-%04d", i) }

// callees are the services i calls: spread over the whole range, never itself.
func (t topology) callees(i int) []int {
	out := make([]int, 0, t.fanout)
	for k := 0; k < t.fanout && k < t.services-1; k++ {
		out = append(out, (i+1+k*7919)%t.services)
		if out[len(out)-1] == i {
			out[len(out)-1] = (i + 1) % t.services
		}
	}
	return out
}

type server struct {
	topo     topology
	clusters int
	queries  atomic.Int64
	traces   atomic.Int64
}

func main() {
	listen := flag.String("listen", "127.0.0.1:19090", "address")
	clusters := flag.Int("clusters", 1, "clusters served under /c/<i>/")
	services := flag.Int("services", 100, "services per cluster")
	fanout := flag.Int("fanout", 3, "calls each service makes")
	journeys := flag.Int("journeys", 1, "journey values per call (1 = no journey dimension)")
	tenants := flag.Int("tenants", 1, "tenant values per call (1 = no tenant dimension)")
	flag.Parse()
	s := &server{topo: topology{*services, *fanout, *journeys, *tenants}, clusters: *clusters}
	http.HandleFunc("/c/", s.route)
	http.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"queries":%d,"traces":%d}`, s.queries.Load(), s.traces.Load())
	})
	log.Printf("synth: %d clusters × %d services × %d calls × %d journeys × %d tenants on %s",
		*clusters, *services, *fanout, *journeys, *tenants, *listen)
	log.Fatal(http.ListenAndServe(*listen, nil))
}

func (s *server) route(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/c/"), "/", 2)
	cluster, err := strconv.Atoi(parts[0])
	if err != nil || cluster < 0 || cluster >= s.clusters || len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	rest := "/" + parts[1]
	switch {
	case rest == "/api/v1/query":
		s.query(w, r, cluster)
	case rest == "/tempo/api/v2/search/tag/resource.service.name/values":
		s.tagValues(w)
	case rest == "/tempo/api/search":
		s.search(w, r)
	case strings.HasPrefix(rest, "/tempo/api/v2/traces/"):
		s.trace(w, cluster, strings.TrimPrefix(rest, "/tempo/api/v2/traces/"))
	default:
		http.NotFound(w, r)
	}
}

// rate is a deterministic, cluster-specific value for one series.
func rate(cluster, from, to, j, t int) float64 {
	return float64((cluster*31+from*17+to*13+j*7+t*3)%97+1) / 10
}

func (s *server) query(w http.ResponseWriter, r *http.Request, cluster int) {
	s.queries.Add(1)
	q := r.FormValue("query")
	kind := "rate"
	switch {
	case strings.Contains(q, "histogram_quantile"):
		kind = "p95"
	case strings.Contains(q, `status="error"`):
		kind = "errors"
	}
	w.Header().Set("Content-Type", "application/json")
	var b strings.Builder
	b.WriteString(`{"status":"success","data":{"resultType":"vector","result":[`)
	first := true
	for from := 0; from < s.topo.services; from++ {
		for _, to := range s.topo.callees(from) {
			for j := 0; j < s.topo.journeys; j++ {
				for t := 0; t < s.topo.tenants; t++ {
					v := rate(cluster, from, to, j, t)
					switch kind {
					case "errors":
						v = v * float64((from+to)%5) / 100
					case "p95":
						v = float64((from*3+to+j)%400+5) / 1000
					}
					if !first {
						b.WriteByte(',')
					}
					first = false
					fmt.Fprintf(&b, `{"metric":{"cluster":"c%d","source":"%s","target":"%s","journey":"j%d","tenant":"t%d"},"value":[%d,"%g"]}`,
						cluster, s.topo.name(from), s.topo.name(to), j, t, 1, v)
				}
			}
		}
	}
	b.WriteString(`]}}`)
	_, _ = w.Write([]byte(b.String()))
}

func (s *server) tagValues(w http.ResponseWriter) {
	type value struct {
		Value string `json:"value"`
	}
	out := struct {
		TagValues []value `json:"tagValues"`
	}{}
	for i := 0; i < s.topo.services; i++ {
		out.TagValues = append(out.TagValues, value{s.topo.name(i)})
	}
	_ = json.NewEncoder(w).Encode(out)
}

// search returns one trace per call the service makes, up to the limit.
func (s *server) search(w http.ResponseWriter, r *http.Request) {
	q := r.FormValue("q")
	start := strings.Index(q, `"`)
	end := strings.LastIndex(q, `"`)
	limit, _ := strconv.Atoi(r.FormValue("limit"))
	type trace struct {
		TraceID string `json:"traceID"`
	}
	out := struct {
		Traces []trace `json:"traces"`
	}{Traces: []trace{}}
	if start < 0 || end <= start {
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	svc, err := strconv.Atoi(strings.TrimPrefix(q[start+1:end], "svc-"))
	if err == nil {
		for k, to := range s.topo.callees(svc) {
			if k >= limit {
				break
			}
			out.Traces = append(out.Traces, trace{fmt.Sprintf("%016x%08x%08x", svc, to, k)})
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}

// trace is one call: a server span in the caller, its client span, and the
// callee's server span, tagged with a journey.
func (s *server) trace(w http.ResponseWriter, cluster int, id string) {
	s.traces.Add(1)
	if len(id) != 32 {
		http.NotFound(w, nil)
		return
	}
	from, _ := strconv.ParseInt(id[:16], 16, 64)
	to, _ := strconv.ParseInt(id[16:24], 16, 64)
	at := int64(1_700_000_000_000_000_000)
	journey := fmt.Sprintf("j%d", (int(from)+cluster)%s.topo.journeys)
	span := func(spanID, parent string, kind int, start, end int64) string {
		return fmt.Sprintf(`{"traceId":"%s","spanId":"%s","parentSpanId":"%s","name":"call","kind":%d,"startTimeUnixNano":"%d","endTimeUnixNano":"%d","attributes":[{"key":"business.journey","value":{"stringValue":"%s"}}]}`,
			id, spanID, parent, kind, start, end, journey)
	}
	batch := func(service string, spans ...string) string {
		return fmt.Sprintf(`{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"%s"}}]},"scopeSpans":[{"spans":[%s]}]}`,
			service, strings.Join(spans, ","))
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"trace":{"resourceSpans":[%s,%s]}}`,
		batch(s.topo.name(int(from)), span("0000000000000001", "", 2, at, at+50e6), span("0000000000000002", "0000000000000001", 3, at+1e6, at+40e6)),
		batch(s.topo.name(int(to)), span("0000000000000003", "0000000000000002", 2, at+2e6, at+38e6)))
}
