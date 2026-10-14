package servicegraph

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

func fakeProm(t *testing.T, answers map[string]string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		q := r.URL.Query().Get("query")
		for prefix, body := range answers {
			if strings.Contains(q, prefix) {
				fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[%s]}}`, body)
				return
			}
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
}

func TestDiscoverEdgesAndVirtualNodes(t *testing.T) {
	srv := fakeProm(t, map[string]string{
		"rate(" + DefaultRequestMetric: `
			{"metric":{"client":"web","server":"orders"},"value":[0,"4"]},
			{"metric":{"client":"orders","server":"orders-db","client_db_system":"postgresql","connection_type":"virtual_node"},"value":[0,"2"]},
			{"metric":{"client":"user","server":"web","connection_type":"virtual_node"},"value":[0,"1"]},
			{"metric":{"client":"web","server":"unknown","connection_type":"virtual_node"},"value":[0,"0.5"]}`,
		"rate(" + DefaultFailedMetric: `{"metric":{"client":"web","server":"orders"},"value":[0,"1"]}`,
	}, 0)
	defer srv.Close()

	obs, err := New("sg", Config{PrometheusURL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Edges) != 3 {
		t.Fatalf("got %d edges, want 3 (the unnamed target is not an edge)", len(obs.Edges))
	}
	if len(obs.Findings) != 1 || obs.Findings[0].Rule != "servicegraph-unnamed-target" || obs.Findings[0].Subject != "web" {
		t.Errorf("want one unnamed-target finding for web, got %+v", obs.Findings)
	}
	if e := obs.Edges[0]; e.RequestRate != 4 || e.ErrorRate != 0.25 {
		t.Errorf("web->orders rates wrong: %+v", e)
	}
	if e := obs.Edges[1]; e.Protocol != "postgresql" {
		t.Errorf("db edge protocol %q", e.Protocol)
	}
	self := map[string]bool{}
	kinds := map[string]model.NodeKind{}
	for _, n := range obs.Nodes {
		self[n.ID] = self[n.ID] || n.SelfReported
		if n.Kind != model.KindUnknown {
			kinds[n.ID] = n.Kind
		}
	}
	if !self["web"] || !self["orders"] || self["orders-db"] || self["user"] {
		t.Errorf("self-reported flags wrong: %v", self)
	}
	if kinds["orders-db"] != model.KindDatabase || kinds["user"] != model.KindExternal || kinds["orders"] != "" {
		t.Errorf("kinds wrong: %v", kinds)
	}
}

func TestDiscoverWithoutMetricsSuggestsConnector(t *testing.T) {
	srv := fakeProm(t, nil, 0)
	defer srv.Close()
	obs, err := New("sg", Config{PrometheusURL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Degraded || len(obs.Findings) != 1 || obs.Findings[0].Rule != "servicegraph-no-metrics" {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}

func TestDiscoverDeniedIsAPermissionFinding(t *testing.T) {
	srv := fakeProm(t, nil, http.StatusForbidden)
	defer srv.Close()
	obs, err := New("sg", Config{PrometheusURL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Findings) != 1 || obs.Findings[0].Kind != model.FindingPermission {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}
