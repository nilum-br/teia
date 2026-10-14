package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

// fakeProm answers by matching a fragment of the query.
func fakeProm(t *testing.T, answers map[string]string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
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

func edges(obs source.Observation) map[string]source.EdgeObservation {
	out := map[string]source.EdgeObservation{}
	for _, e := range obs.Edges {
		out[e.From+"->"+e.To] = e
	}
	return out
}

func selfReported(obs source.Observation) (map[string]bool, map[string]model.NodeKind) {
	self, kinds := map[string]bool{}, map[string]model.NodeKind{}
	for _, n := range obs.Nodes {
		self[n.ID] = self[n.ID] || n.SelfReported
		if n.Kind != model.KindUnknown {
			kinds[n.ID] = n.Kind
		}
	}
	return self, kinds
}

func TestAutoDetectsMixedMeshAndTraces(t *testing.T) {
	srv := fakeProm(t, map[string]string{
		"count(traces_service_graph_request_total)": `{"metric":{},"value":[0,"12"]}`,
		"count(istio_requests_total)":               `{"metric":{},"value":[0,"40"]}`,
		"rate(traces_service_graph_request_total": `
			{"metric":{"client":"web","server":"orders"},"value":[0,"4"]},
			{"metric":{"client":"orders","server":"orders-db","client_db_system":"postgresql","connection_type":"virtual_node"},"value":[0,"2"]},
			{"metric":{"client":"user","server":"catalog","connection_type":"virtual_node"},"value":[0,"0.3"]},
			{"metric":{"client":"web","server":"unknown","connection_type":"virtual_node"},"value":[0,"0.5"]}`,
		"rate(traces_service_graph_request_failed_total": `{"metric":{"client":"web","server":"orders"},"value":[0,"1"]}`,
		`response_code=~`: `
			{"metric":{"reporter":"source","source_workload":"recommendations","destination_workload":"inventory","destination_service_name":"inventory","request_protocol":"http"},"value":[0,"0.2"]}`,
		`istio_requests_total{reporter="source"}[`: `
			{"metric":{"reporter":"source","source_workload":"recommendations","destination_workload":"inventory","destination_service_name":"inventory","request_protocol":"http"},"value":[0,"2"]},
			{"metric":{"reporter":"destination","source_workload":"recommendations","destination_workload":"inventory","destination_service_name":"inventory","request_protocol":"http"},"value":[0,"1.9"]},
			{"metric":{"reporter":"destination","source_workload":"unknown","destination_workload":"recommendations","destination_service_name":"recommendations","request_protocol":"http"},"value":[0,"2"]},
			{"metric":{"reporter":"source","source_workload":"inventory","destination_workload":"unknown","destination_service_name":"api.supplier.example","request_protocol":"http"},"value":[0,"0.4"]}`,
	}, 0)
	defer srv.Close()

	obs, err := New("prometheus", Config{URL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := edges(obs)
	for _, want := range []string{"web->orders", "orders->orders-db", "recommendations->inventory", "inventory->api.supplier.example"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing edge %s; got %v", want, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("want 4 edges (user, unknown and the mesh's double report do not add edges), got %d", len(got))
	}
	if e := got["web->orders"]; e.RequestRate != 4 || e.ErrorRate != 0.25 {
		t.Errorf("web->orders rates: %+v", e)
	}
	if e := got["recommendations->inventory"]; e.RequestRate != 2 || e.ErrorRate != 0.1 {
		t.Errorf("mesh edge should keep the largest report and its failures: %+v", e)
	}
	unknown := map[string]string{}
	for _, u := range obs.UnknownCallers {
		unknown[u.Callee] = fmt.Sprintf("%s %g", u.Signal, u.Rate)
	}
	if unknown["catalog"] != "traces 0.3" || unknown["recommendations"] != "mesh 2" {
		t.Errorf("unknown callers: %v", unknown)
	}
	self, kinds := selfReported(obs)
	if !self["recommendations"] || !self["inventory"] || self["api.supplier.example"] || self["orders-db"] {
		t.Errorf("self-reported: %v", self)
	}
	if kinds["orders-db"] != model.KindDatabase {
		t.Errorf("kinds: %v", kinds)
	}
	if len(obs.Findings) != 1 || obs.Findings[0].Rule != "servicegraph-unnamed-target" {
		t.Errorf("findings: %+v", obs.Findings)
	}
	if !strings.Contains(obs.Message, "servicegraph (2)") || !strings.Contains(obs.Message, "istio (2)") {
		t.Errorf("message: %s", obs.Message)
	}
}

func TestNothingFoundSuggestsTheCheapestSignal(t *testing.T) {
	srv := fakeProm(t, nil, 0)
	defer srv.Close()
	obs, err := New("prometheus", Config{URL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Degraded || len(obs.Findings) != 1 || obs.Findings[0].Rule != "prometheus-no-signals" {
		t.Fatalf("unexpected: %+v", obs)
	}
}

func TestDeniedIsAPermissionFinding(t *testing.T) {
	srv := fakeProm(t, nil, http.StatusForbidden)
	defer srv.Close()
	obs, err := New("prometheus", Config{URL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Findings) != 1 || obs.Findings[0].Kind != model.FindingPermission {
		t.Fatalf("unexpected: %+v", obs)
	}
}

func TestExplicitPresetSkipsDetection(t *testing.T) {
	srv := fakeProm(t, map[string]string{
		"rate(istio_tcp_connections_opened_total": `{"metric":{"reporter":"source","source_workload":"orders","destination_workload":"unknown","destination_service_name":"orders-db.shop.svc.cluster.local"},"value":[0,"0.1"]}`,
	}, 0)
	defer srv.Close()
	obs, err := New("prometheus", Config{URL: srv.URL, Presets: []string{"istio-tcp"}}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e, ok := edges(obs)["orders->orders-db"]
	if !ok || e.Protocol != "tcp" {
		t.Fatalf("want orders->orders-db over tcp with the service host shortened, got %+v", obs.Edges)
	}
}

func TestPresetsReadTheirLabels(t *testing.T) {
	cases := []struct {
		preset string
		labels map[string]string
		want   string // "from->to", "unknown->callee" or ""
	}{
		{"istio", map[string]string{"source_workload": "web", "destination_workload": "waypoint", "destination_service_name": "orders"}, "web->orders"},
		{"istio", map[string]string{"source_workload": "waypoint", "destination_workload": "orders"}, ""},
		{"istio", map[string]string{"source_workload": "web", "destination_workload": "unknown", "destination_service_name": "PassthroughCluster"}, ""},
		{"linkerd", map[string]string{"deployment": "web", "dst_deployment": "orders", "namespace": "shop"}, "web->orders"},
		{"linkerd", map[string]string{"deployment": "web", "dst_service": "orders.shop.svc.cluster.local"}, "web->orders"},
		{"linkerd", map[string]string{"dst_deployment": "orders"}, ""},
		{"beyla-network", map[string]string{"k8s_src_owner_name": "web", "k8s_src_owner_type": "Deployment", "k8s_dst_owner_name": "orders", "k8s_dst_owner_type": "Service"}, "web->orders"},
		{"obi-network", map[string]string{"k8s_src_owner_name": "web", "k8s_dst_owner_name": "node-1", "k8s_dst_owner_type": "Node"}, ""},
		{"hubble-tcp", map[string]string{"source_workload": "web", "destination_workload": "orders"}, "web->orders"},
		{"retina", map[string]string{"source_workload_name": "web", "destination_workload_name": "orders"}, "web->orders"},
		{"servicegraph", map[string]string{"client": "user", "server": "catalog", "connection_type": "virtual_node"}, "unknown->catalog"},
	}
	for _, c := range cases {
		res := Lookup(c.preset).Map(c.labels)
		got := ""
		switch {
		case res.Edge != nil:
			got = res.Edge.From + "->" + res.Edge.To
		case res.UnknownCallerOf != "":
			got = "unknown->" + res.UnknownCallerOf
		}
		if got != c.want {
			t.Errorf("%s %v = %q, want %q", c.preset, c.labels, got, c.want)
		}
	}
}

func TestNetworkPresetsDoNotReportBytesAsRequests(t *testing.T) {
	srv := fakeProm(t, map[string]string{
		"rate(beyla_network_flow_bytes_total": `{"metric":{"k8s_src_owner_name":"web","k8s_dst_owner_name":"orders","k8s_dst_owner_type":"Deployment","k8s_src_namespace":"shop"},"value":[0,"52000"]}`,
	}, 0)
	defer srv.Close()
	obs, err := New("prometheus", Config{URL: srv.URL, Presets: []string{"beyla-network"}}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e := edges(obs)["web->orders"]
	if e.RequestRate != 0 || e.Signal != "network" {
		t.Errorf("edge = %+v", e)
	}
	if obs.Nodes[0].Labels["k8s.namespace"] != "shop" || obs.Nodes[0].Labels["teia.signal.network"] != "true" {
		t.Errorf("node labels = %v", obs.Nodes[0].Labels)
	}
}
