package query

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Two journeys and two tenants share app-a -> app-b; only whatsapp reaches
// redis. Each combination must keep its own rate, error share and p95, and
// the edge must remain the total.
func TestDimensionsSplitEdgesIntoSlices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		var result string
		switch {
		case strings.HasPrefix(q, "errors"):
			result = `{"metric":{"source":"app-a","target":"app-b","journey":"notification","tenant":"a"},"value":[0,"0.5"]},
			{"metric":{"source":"app-a","target":"app-b","journey":"whatsapp","tenant":"a"},"value":[0,"0"]},
			{"metric":{"source":"app-a","target":"app-b","journey":"whatsapp","tenant":"b"},"value":[0,"0"]},
			{"metric":{"source":"app-b","target":"redis","journey":"whatsapp","tenant":"a"},"value":[0,"0"]}`
		case strings.HasPrefix(q, "latency"):
			result = `{"metric":{"source":"app-a","target":"app-b","journey":"notification","tenant":"a"},"value":[0,"0.24"]},
			{"metric":{"source":"app-a","target":"app-b","journey":"whatsapp","tenant":"a"},"value":[0,"0.05"]},
			{"metric":{"source":"app-a","target":"app-b","journey":"whatsapp","tenant":"b"},"value":[0,"NaN"]}`
		default:
			result = `{"metric":{"source":"app-a","target":"app-b","journey":"notification","tenant":"a"},"value":[0,"1"]},
			{"metric":{"source":"app-a","target":"app-b","journey":"whatsapp","tenant":"a"},"value":[0,"2"]},
			{"metric":{"source":"app-a","target":"app-b","journey":"whatsapp","tenant":"b"},"value":[0,"3"]},
			{"metric":{"source":"app-b","target":"redis","journey":"whatsapp","tenant":"a"},"value":[0,"2"]},
			{"metric":{"source":"app-b","target":"redis"},"value":[0,"7"]}`
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[%s]}}`, result)
	}))
	defer srv.Close()
	obs, err := New("calls", Config{URL: srv.URL, Query: "requests", Errors: "errors", Latency: "latency", From: "source", To: "target",
		Dimensions: map[string]string{"journey": "journey", "tenant": "tenant"}}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(obs.Dimensions) != "[journey tenant]" {
		t.Fatalf("dimensions = %v", obs.Dimensions)
	}
	edges := map[string]int{}
	for i, e := range obs.Edges {
		edges[e.From+"->"+e.To] = i
	}
	ab := obs.Edges[edges["app-a->app-b"]]
	if ab.RequestRate != 6 || len(ab.Slices) != 3 {
		t.Fatalf("app-a->app-b total %v with %d slices", ab.RequestRate, len(ab.Slices))
	}
	for _, sl := range ab.Slices {
		switch sl.Dims["journey"] + "/" + sl.Dims["tenant"] {
		case "notification/a":
			if sl.RequestRate != 1 || sl.ErrorRate != 0.5 || sl.NoErrors || sl.LatencyP95Ms != 240 {
				t.Errorf("notification slice = %+v", sl)
			}
		case "whatsapp/a":
			if sl.RequestRate != 2 || sl.ErrorRate != 0 || sl.NoErrors || sl.LatencyP95Ms != 50 {
				t.Errorf("whatsapp/a slice = %+v", sl)
			}
		case "whatsapp/b":
			if sl.RequestRate != 3 || sl.LatencyP95Ms != 0 {
				t.Errorf("a NaN quantile must stay unmeasured, not zero or NaN: %+v", sl)
			}
		default:
			t.Errorf("unexpected slice %v", sl.Dims)
		}
	}
	// p95 of the whole edge: slices with a p95 weighted by their traffic.
	if want := (240*1 + 50*2) / 3.0; ab.LatencyP95Ms != want || !ab.LatencyApproximate {
		t.Errorf("edge p95 = %v approx %v, want %v approximate", ab.LatencyP95Ms, ab.LatencyApproximate, want)
	}
	redis := obs.Edges[edges["app-b->redis"]]
	if redis.RequestRate != 9 || len(redis.Slices) != 1 || redis.Slices[0].Dims["journey"] != "whatsapp" {
		t.Fatalf("series without dimensions count in the edge but form no slice: %+v", redis)
	}
}
