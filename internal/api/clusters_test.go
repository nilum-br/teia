package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

// twoClusters runs the same apps in two clusters with different traffic and
// health: east is quiet and failing, west is busy and healthy.
func twoClusters() []Cluster {
	east, west := newMCPEngine(), newMCPEngine()
	east.snap.Cluster, west.snap.Cluster = "east", "west"
	east.snap.Edges = []model.Edge{{From: "app-a", To: "app-b", Observed: true, State: model.EdgeOK, Health: model.HealthFailing, RequestRate: 2, ErrorRate: 0.55}}
	west.snap.Edges = []model.Edge{{From: "app-a", To: "app-b", Observed: true, State: model.EdgeOK, Health: model.HealthHealthy, RequestRate: 8, ErrorRate: 0.01}}
	east.snap.Nodes[0].Health, west.snap.Nodes[0].Health = model.HealthFailing, model.HealthHealthy
	return []Cluster{{Name: "east", Engine: east}, {Name: "west", Engine: west}}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return v
}

func TestEachRequestReadsOneCluster(t *testing.T) {
	h := Clusters(twoClusters(), Options{})

	list := decode[struct {
		Default  string           `json:"default"`
		Clusters []clusterSummary `json:"clusters"`
	}](t, get(h, "/api/v1/clusters", nil))
	if list.Default != "east" || len(list.Clusters) != 2 || list.Clusters[1].Name != "west" || list.Clusters[0].Health["failing"] != 1 {
		t.Fatalf("clusters = %+v", list)
	}

	// Without ?cluster= the first cluster answers, and says so.
	whole := decode[model.Snapshot](t, get(h, "/api/v1/snapshot", nil))
	if whole.Cluster != "east" || whole.Edges[0].RequestRate != 2 {
		t.Fatalf("default snapshot = %s %+v", whole.Cluster, whole.Edges)
	}
	west := decode[model.Snapshot](t, get(h, "/api/v1/snapshot?cluster=west", nil))
	if west.Cluster != "west" || len(west.Edges) != 1 || west.Edges[0].RequestRate != 8 || west.Edges[0].Health != model.HealthHealthy {
		t.Fatalf("west = %s %+v: one cluster's numbers, never merged with another's", west.Cluster, west.Edges)
	}
	// Every endpoint behind /api/v1 follows ?cluster=, slices included.
	for _, path := range []string{"graph", "findings", "coverage", "journeys", "sources", "changes", "dimensions", "grafana/nodes", "grafana/edges", "export/matrix.csv"} {
		if rec := get(h, "/api/v1/"+path+"?cluster=west", nil); rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	// Grafana rows name their cluster, so the panel can say which one it shows.
	edges := decode[[]map[string]any](t, get(h, "/api/v1/grafana/edges?cluster=west", nil))
	nodes := decode[[]map[string]any](t, get(h, "/api/v1/grafana/nodes?cluster=west", nil))
	findings := decode[[]map[string]any](t, get(h, "/api/v1/grafana/findings?cluster=west", nil))
	if len(edges) != 1 || edges[0]["cluster"] != "west" || nodes[0]["cluster"] != "west" || findings[0]["cluster"] != "west" {
		t.Fatalf("grafana rows: edges %v nodes %v findings %v", edges, nodes[0], findings)
	}
	if rows := decode[[]map[string]any](t, get(Handler(newFakeEngine(), Options{}), "/api/v1/grafana/nodes", nil)); rows[0]["cluster"] != nil {
		t.Fatalf("a single scope's rows have no cluster: %v", rows[0])
	}

	rec := get(h, "/api/v1/snapshot?cluster=north", nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `no cluster \"north\"`) || !strings.Contains(rec.Body.String(), "east, west") {
		t.Fatalf("an unknown cluster is a 404 naming the others, never another cluster's map: %d %s", rec.Code, rec.Body)
	}
	if rec := get(h, "/api/v1/stream?cluster=north", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("stream of an unknown cluster: %d", rec.Code)
	}
}

func TestSingleScopeRejectsAClusterName(t *testing.T) {
	h := Handler(newFakeEngine(), Options{})
	if rec := get(h, "/api/v1/snapshot", nil); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"cluster"`) {
		t.Fatalf("a single scope answers as before, without a cluster: %d %s", rec.Code, rec.Body)
	}
	if rec := get(h, "/api/v1/snapshot?cluster=east", nil); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "single scope") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	list := decode[map[string]any](t, get(h, "/api/v1/clusters", nil))
	if list["default"] != "" || len(list["clusters"].([]any)) != 1 {
		t.Fatalf("clusters of a single scope = %v", list)
	}
}

func TestStreamFollowsTheCluster(t *testing.T) {
	clusters := twoClusters()
	for _, c := range clusters {
		e := c.Engine.(*mcpEngine)
		data, _ := json.Marshal(e.snap)
		e.updates <- data
	}
	srv := httptest.NewServer(Clusters(clusters, Options{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/stream?cluster=west")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("stream: %v %v", resp, err)
	}
	defer resp.Body.Close()
	_, data := nextEvent(t, bufio.NewReader(resp.Body))
	var snap model.Snapshot
	if err := json.Unmarshal([]byte(data), &snap); err != nil || snap.Cluster != "west" {
		t.Fatalf("west stream sent %q (%v)", snap.Cluster, err)
	}
}

func TestMetricsLabelEachCluster(t *testing.T) {
	body := get(Clusters(twoClusters(), Options{}), "/metrics", nil).Body.String()
	if strings.Count(body, "# HELP teia_edge_info ") != 1 {
		t.Fatalf("each family once:\n%s", body)
	}
	for _, want := range []string{
		`teia_edge_info{cluster="east",from="app-a",to="app-b",`,
		`teia_edge_info{cluster="west",from="app-a",to="app-b",`,
		`teia_coverage_ratio{cluster="west",scope="visibility"}`,
		`teia_last_round_timestamp_seconds{cluster="east"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in\n%s", want, body)
		}
	}
	single := get(Handler(newFakeEngine(), Options{}), "/metrics", nil).Body.String()
	if strings.Contains(single, "cluster=") || !strings.Contains(single, "teia_last_round_timestamp_seconds ") {
		t.Fatalf("a single scope keeps its series unlabelled:\n%s", single)
	}
}

func TestMCPReadsOneClusterAtATime(t *testing.T) {
	s, err := mcpSession(t, Clusters(twoClusters(), Options{}), "")
	if err != nil {
		t.Fatal(err)
	}
	overview, _ := call(t, s, "teia_overview", nil)
	if overview["cluster"] != "east" || len(overview["clusters"].([]any)) != 2 {
		t.Fatalf("overview = %v", overview)
	}
	west, _ := call(t, s, "teia_map", map[string]any{"cluster": "west"})
	edge := west["edges"].([]any)[0].(map[string]any)
	if west["cluster"] != "west" || edge["requestRate"] != 8.0 || edge["health"] != "healthy" {
		t.Fatalf("west map = %v", west)
	}
	changes, _ := call(t, s, "teia_changes", map[string]any{"cluster": "west"})
	if changes["cluster"] != "west" {
		t.Fatalf("changes = %v", changes)
	}
	if _, res := call(t, s, "teia_component", map[string]any{"cluster": "north", "id": "app-a"}); !strings.Contains(errorText(res), `no cluster "north"`) {
		t.Fatalf("unknown cluster: %v", res)
	}
	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		raw, _ := json.Marshal(tool.InputSchema)
		if !strings.Contains(string(raw), `"cluster"`) {
			t.Errorf("%s takes no cluster argument: %s", tool.Name, raw)
		}
	}
}

// Change rows carry their cluster next to the temporal fields: embedding
// both would make encoding/json drop the name they share.
func TestGrafanaChangeRowsNameTheirCluster(t *testing.T) {
	snap := model.Snapshot{Cluster: "east", TimeMode: "current", Changes: []model.Change{{Subject: "app-a->app-b", Message: "failing", Cluster: "east"}}}
	data, err := json.Marshal(historicalChanges(snap))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) != 1 || rows[0]["cluster"] != "east" || rows[0]["timeMode"] != "current" || rows[0]["subject"] != "app-a->app-b" {
		t.Fatalf("change rows = %s (%v)", data, err)
	}
}
