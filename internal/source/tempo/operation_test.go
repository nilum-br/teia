package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func operationWindow(at time.Time) url.Values {
	return url.Values{"start": {strconv.FormatInt(at.Add(-5*time.Minute).Unix(), 10)}, "end": {strconv.FormatInt(at.Unix(), 10)}}
}

func operationRoot(id, route string, at time.Time) span {
	return span{id: id, service: "web", kind: "server", attrs: map[string]string{"http.route": route, "http.request.method": "POST"}, startedAt: at, endedAt: at.Add(time.Millisecond)}
}

func TestOperationAcceptanceSeparatesSharedServicesAndKeepsOrphans(t *testing.T) {
	data, err := os.ReadFile("../../journey/testdata/operation-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		QueryPeriod model.OperationPeriod `json:"queryPeriod"`
		Traces      []struct {
			TraceID string `json:"traceId"`
			Root    *struct {
				Service, Operation, SpanID string
				DurationMs                 float64
			} `json:"root"`
			Spans []struct{ SpanID, ParentSpanID, Service string } `json:"spans"`
		} `json:"traces"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var traces []sampledTrace
	for _, item := range fixture.Traces {
		trace := sampledTrace{id: item.TraceID}
		if root := item.Root; root != nil {
			method, route, _ := strings.Cut(root.Operation, " ")
			trace.spans = append(trace.spans, span{id: root.SpanID, service: root.Service, kind: "server", attrs: map[string]string{"http.request.method": method, "http.route": route}, startedAt: fixture.QueryPeriod.From, endedAt: fixture.QueryPeriod.From.Add(time.Duration(root.DurationMs * float64(time.Millisecond)))})
		}
		for _, sp := range item.Spans {
			trace.spans = append(trace.spans, span{id: sp.SpanID, parent: sp.ParentSpanID, service: sp.Service, kind: "server"})
		}
		traces = append(traces, trace)
	}
	got := operationSamples("tempo", operationWindow(fixture.QueryPeriod.To), traces, 4, 0, 4, 4, false)
	encodedFixture, _ := json.Marshal(got)
	expectedJSON, err := os.ReadFile("../../journey/testdata/operation-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual any
	if err := json.Unmarshal(expectedJSON, &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encodedFixture, &actual); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("shared runtime fixture differs from the source contract")
	}
	if got.Sampling.TotalRequests != nil || got.Sampling.Coverage != nil || got.Sampling.Policy != "unknown" {
		t.Fatal("invented a population or sampling denominator")
	}
	scopes := map[string]model.OperationScope{}
	for _, scope := range got.Scopes {
		scopes[scope.Operation] = scope
	}
	checkout, browse := scopes["POST /checkout"], scopes["GET /browse"]
	if checkout.SampledRootCount != 2 || checkout.IncompleteTraceCount != 1 || browse.SampledRootCount != 1 || browse.IncompleteTraceCount != 0 {
		t.Fatalf("root/gap counts: checkout=%+v browse=%+v", checkout, browse)
	}
	for name, want := range map[string][]string{"POST /checkout": {"orders->db", "web->orders"}, "GET /browse": {"catalog->db", "web->catalog"}} {
		var edges []string
		for _, edge := range scopes[name].Edges {
			edges = append(edges, edge.From+"->"+edge.To)
		}
		sort.Strings(edges)
		if !reflect.DeepEqual(edges, want) {
			t.Fatalf("%s contains another operation: %v", name, edges)
		}
	}
	if !reflect.DeepEqual(checkout.SampleRootDurationsMs, []float64{180, 300}) || !reflect.DeepEqual(browse.SampleRootDurationsMs, []float64{80}) {
		t.Fatal("sample root durations changed")
	}
	var orphans []string
	for _, orphan := range got.Unattributed {
		orphans = append(orphans, orphan.SpanID)
	}
	sort.Strings(orphans)
	if !reflect.DeepEqual(orphans, []string{"o1", "p2"}) {
		t.Fatalf("orphans assigned from labels: %v", orphans)
	}
	for _, scope := range got.Scopes {
		for _, trace := range scope.Traces {
			if trace.StructuralStatus == "complete" {
				t.Fatal("absence of detected gaps advertised as full trace coverage")
			}
		}
	}
}

func TestOperationRetriesLinksCyclesAndMissingErrorStatus(t *testing.T) {
	at := time.Now().UTC().Add(-time.Minute)
	root := operationRoot("root", "/checkout", at)
	first := span{id: "attempt-1", parent: "root", service: "web", kind: "client", attrs: map[string]string{"peer.service": "db"}, startedAt: at, endedAt: at.Add(2 * time.Millisecond)}
	second := first
	second.id = "attempt-2"
	second.links = []model.OperationSpanLink{{TraceID: "async-trace", SpanID: "consumer"}}
	got := operationSamples("tempo", operationWindow(at.Add(time.Minute)), []sampledTrace{{id: "retries", spans: []span{root, first, second}}}, 1, 0, 1, 1, false)
	scope := got.Scopes[0]
	if len(scope.Edges) != 1 || len(scope.Edges[0].Calls) != 2 || scope.RootOutcomes.Unknown != 1 || scope.IncompleteTraceCount != 1 || len(scope.Traces[0].Spans[2].Links) != 1 {
		t.Fatalf("retry attempts/links/errors lost: %+v", scope)
	}
	if slices.ContainsFunc(scope.Nodes, func(n model.OperationNode) bool { return n.ID == "consumer" }) {
		t.Fatal("followed an async link as synchronous ancestry")
	}
	root.parent = "attempt-1"
	got = operationSamples("tempo", operationWindow(at.Add(time.Minute)), []sampledTrace{{id: "cycle", spans: []span{root, first}}}, 1, 0, 1, 1, false)
	if got.Scopes[0].IncompleteTraceCount != 1 {
		t.Fatal("parent cycle was not qualified")
	}
}

func TestOperationRequiresNormalizedRouteAndSeparatesOutcomeFromGlobalHealth(t *testing.T) {
	at := time.Now().UTC()
	unsafe := operationRoot("unsafe", "", at)
	unsafe.name = "POST /users/secret-token?access_token=secret"
	unsafe.attrs["url.full"] = "https://example.com/users/123"
	unsafe.attrs["journey"] = "checkout"
	valid := operationRoot("valid", "/users/{id}", at)
	valid.attrs["http.response.status_code"] = "500"
	got := operationSamples("tempo", operationWindow(at), []sampledTrace{{id: "unsafe", spans: []span{unsafe}}, {id: "valid", spans: []span{valid}}}, 2, 0, 1, 1, false)
	if len(got.Scopes) != 1 || got.Scopes[0].Operation != "POST /users/{id}" || got.Scopes[0].RootOutcomes.Error != 1 || len(got.Unattributed) != 1 {
		t.Fatalf("normalization/outcome: %+v", got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "url.full") {
		t.Fatal("raw request data leaked into operation contract")
	}
}

func TestOperationCapsAreExplicitAndBoundNestedRoots(t *testing.T) {
	at := time.Now().UTC()
	spans := make([]span, 250)
	for i := range spans {
		spans[i] = operationRoot(fmt.Sprint(i), "/recursive", at)
		if i > 0 {
			spans[i].parent = fmt.Sprint(i - 1)
		}
	}
	got := operationSamples("tempo", operationWindow(at), []sampledTrace{{id: "many", spans: spans}}, 1, 0, 1, 1, false)
	count := 0
	for _, scope := range got.Scopes {
		for _, trace := range scope.Traces {
			count += len(trace.Spans)
		}
	}
	if count > maxOperationSpans || !got.Sources[0].Truncated || got.Sources[0].Status != "partial" || !got.Scopes[0].Truncated {
		t.Fatalf("unbounded or hidden truncation: %d %+v", count, got.Sources)
	}
}

func TestOperationNestedRootsAtInputLimitRemainBounded(t *testing.T) {
	at := time.Now().UTC()
	spans := make([]span, maxOperationSpans)
	for i := range spans {
		spans[i] = operationRoot(fmt.Sprintf("%05d", i), "/recursive", at)
		if i > 0 {
			spans[i].parent = spans[i-1].id
		}
	}
	got := operationSamples("tempo", operationWindow(at), []sampledTrace{{id: "nested", spans: spans}}, 1, 0, 1, 1, false)
	if len(got.Scopes) != 1 || len(got.Scopes[0].Traces) != 1 || len(got.Scopes[0].Traces[0].Spans) != maxOperationSpans {
		t.Fatal("nested roots exceeded the global output budget")
	}
	if !got.Scopes[0].Truncated || !got.Sources[0].Truncated {
		t.Fatal("omitted roots did not qualify the scope and source")
	}
}

func TestDiscoverExportsOperationTraceIDsAndPinnedPeriod(t *testing.T) {
	at := time.Date(2026, 10, 15, 12, 5, 0, 0, time.UTC)
	rootStart := at.Add(-time.Minute).UnixNano()
	raw := fmt.Sprintf(`{"trace":{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"web"}}]},"scopeSpans":[{"spans":[{"spanId":"root","name":"raw name ignored","kind":2,"startTimeUnixNano":"%d","endTimeUnixNano":"%d","status":{"code":2},"attributes":[{"key":"http.route","value":{"stringValue":"/checkout"}},{"key":"http.request.method","value":{"stringValue":"POST"}}]}]}]}]}}`, rootStart, rootStart+180000000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/tag/"):
			fmt.Fprint(w, `{"tagValues":[{"value":"web"}]}`)
		case r.URL.Path == "/api/search":
			fmt.Fprint(w, `{"traces":[{"traceID":"real-trace-id"}]}`)
		default:
			fmt.Fprint(w, raw)
		}
	}))
	defer server.Close()
	src := New("tempo", Config{URL: server.URL, Window: 5 * time.Minute})
	src.now = func() time.Time { return at }
	obs, err := src.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if obs.Operations.Sources[0].QueryPeriod.To != at || obs.Operations.Scopes[0].Traces[0].TraceID != "real-trace-id" || obs.Operations.Scopes[0].RootOutcomes.Error != 1 {
		t.Fatalf("operation transport: %+v", obs.Operations)
	}
}
