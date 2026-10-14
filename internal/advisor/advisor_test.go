package advisor

import (
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
)

func node(id string, kind model.NodeKind, vis model.Visibility, labels map[string]string) *model.Node {
	return &model.Node{ID: id, Kind: kind, Visibility: vis, Sources: []string{"test"}, Labels: labels}
}

func edge(from, to string, state model.EdgeState, protocol string) *model.Edge {
	return &model.Edge{
		From: from, To: to, State: state, Protocol: protocol,
		Declared: state != model.EdgeGhost, Observed: state != model.EdgeStale,
	}
}

func findingsByRule(fs []model.Finding) map[string][]model.Finding {
	out := map[string][]model.Finding{}
	for _, f := range fs {
		out[f.Rule] = append(out[f.Rule], f)
	}
	return out
}

func TestAdvise(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*model.Node{
			"web":        node("web", model.KindService, model.VisibilityFull, nil),
			"orders":     node("orders", model.KindService, model.VisibilityFull, map[string]string{"teia.declared": "true"}),
			"catalog":    node("catalog", model.KindService, model.VisibilityFull, nil),
			"cache":      node("cache", model.KindCache, model.VisibilityPartial, nil),
			"orders-db":  node("orders-db", model.KindDatabase, model.VisibilityPartial, nil),
			"legacy":     node("legacy", model.KindService, model.VisibilityNone, nil),
			"notify":     node("notify", model.KindService, model.VisibilityNone, nil),
			"ip:1.2.3.4": node("ip:1.2.3.4", model.KindUnknown, model.VisibilityPartial, nil),
		},
		Edges: map[string]*model.Edge{
			"web->orders":           edge("web", "orders", model.EdgeGhost, "http"),
			"web->catalog":          edge("web", "catalog", model.EdgeGhost, "http"),
			"orders->orders-db":     edge("orders", "orders-db", model.EdgeGhost, "postgresql"),
			"orders->notify":        edge("orders", "notify", model.EdgeStale, ""),
			"catalog->cache":        edge("catalog", "cache", model.EdgeGhost, "redis"),
			"ip:1.2.3.4->orders-db": edge("ip:1.2.3.4", "orders-db", model.EdgeGhost, "postgresql"),
		},
	}
	in := Input{Graph: g, Statuses: []model.SourceStatus{{Name: "servicegraph", Type: "servicegraph", State: model.SourceOK}}}
	by := findingsByRule(Advise(in, nil))

	// web has no flows file: one finding with a generated file for all its calls.
	var webFile *model.Finding
	for _, f := range by["undeclared-flow"] {
		if f.Subject == "web" {
			webFile = &f
		}
	}
	if webFile == nil {
		t.Fatal("expected a generated flows file for web")
	}
	for _, want := range []string{"owner: component:default/web", "to: component:default/orders", "to: component:default/catalog"} {
		if !strings.Contains(webFile.Fix.Snippet, want) {
			t.Errorf("generated flows file misses %q:\n%s", want, webFile.Fix.Snippet)
		}
	}

	// orders has a flows file: per-edge finding instead.
	found := false
	for _, f := range by["undeclared-flow"] {
		if f.Subject == "orders->orders-db" {
			found = true
			if !strings.Contains(f.Fix.Snippet, "resource:default/orders-db") || !strings.Contains(f.Fix.Snippet, "kind: database") {
				t.Errorf("egress entry for a database is wrong:\n%s", f.Fix.Snippet)
			}
		}
	}
	if !found {
		t.Error("expected a per-edge finding for orders->orders-db")
	}

	// Stale only when the caller reports its own telemetry.
	if len(by["stale-flow"]) != 1 || by["stale-flow"][0].Subject != "orders->notify" {
		t.Errorf("stale-flow findings: %+v", by["stale-flow"])
	}

	// Datastores without an explorer get a hint; postgres gets a concrete one.
	unexplored := map[string]model.Finding{}
	for _, f := range by["datastore-unexplored"] {
		unexplored[f.Subject] = f
	}
	if !strings.Contains(unexplored["orders-db"].Fix.Snippet, "GRANT pg_monitor TO teia") {
		t.Errorf("postgres hint misses the grant: %+v", unexplored["orders-db"].Fix)
	}
	if _, ok := unexplored["cache"]; !ok {
		t.Error("expected a hint for the cache")
	}

	// No inventory: unresolved addresses suggest enabling one, and so does no-inventory.
	if len(by["unresolved-address"]) != 1 || !strings.Contains(by["unresolved-address"][0].Fix.Snippet, "docker") {
		t.Errorf("unresolved-address: %+v", by["unresolved-address"])
	}
	if len(by["no-inventory"]) != 1 {
		t.Error("expected the no-inventory hint")
	}

	// Workloads without full visibility.
	subjects := map[string]bool{}
	for _, f := range by["no-telemetry"] {
		subjects[f.Subject] = true
	}
	if !subjects["legacy"] || !subjects["notify"] || subjects["orders-db"] || subjects["ip:1.2.3.4"] {
		t.Errorf("no-telemetry subjects: %v", subjects)
	}
}

func TestAdviseExploredAndSorted(t *testing.T) {
	g := &graph.Graph{
		Nodes: map[string]*model.Node{
			"orders-db": node("orders-db", model.KindDatabase, model.VisibilityFull, nil),
		},
		Edges: map[string]*model.Edge{},
	}
	in := Input{Graph: g, Statuses: []model.SourceStatus{
		{Name: "postgres/orders-db", Type: "postgres", State: model.SourceOK, Explores: []string{"orders-db"}},
		{Name: "docker", Type: "docker", State: model.SourceOK},
		{Name: "servicegraph", Type: "servicegraph", State: model.SourceError, Message: "connection refused"},
	}}
	fs := Advise(in, []model.Finding{{Rule: "postgres-privilege", Kind: model.FindingPermission, Severity: model.SeverityWarning, Subject: "orders-db"}})
	if len(fs) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(fs), fs)
	}
	if fs[0].Rule != "source-error" || fs[0].ID != "source-error:servicegraph" {
		t.Errorf("critical source error should come first, got %+v", fs[0])
	}
	if fs[1].ID != "postgres-privilege:orders-db" {
		t.Errorf("source findings should get IDs, got %q", fs[1].ID)
	}
}

func TestUnknownCallersAreExplainedPerSignal(t *testing.T) {
	traces := map[string]string{"teia.signal.traces": "true"}
	mesh := map[string]string{"teia.signal.mesh": "true"}
	g := &graph.Graph{
		Nodes: map[string]*model.Node{
			"web":             node("web", model.KindService, model.VisibilityFull, traces),
			"catalog":         node("catalog", model.KindService, model.VisibilityFull, traces),
			"recommendations": node("recommendations", model.KindService, model.VisibilityFull, mesh),
			"partner-sync":    node("partner-sync", model.KindService, model.VisibilityPartial, nil),
		},
		Edges: map[string]*model.Edge{
			"web->catalog":         edge("web", "catalog", model.EdgeOK, "http"),
			"web->recommendations": edge("web", "recommendations", model.EdgeOK, "http"),
		},
		UnknownCallers: map[string]map[string]graph.UnknownCallers{
			"catalog":         {"traces": {Rate: 0.3, Sources: []string{"prometheus"}}},
			"recommendations": {"mesh": {Rate: 2, Sources: []string{"prometheus"}}},
		},
	}
	subjects := func() map[string]bool {
		out := map[string]bool{}
		for _, f := range unknownCallers(Input{Graph: g}) {
			out[f.Subject] = true
		}
		return out
	}
	// web has an SDK but no sidecar: it explains the mesh's unknown caller,
	// not the traces' one.
	if got := subjects(); !got["catalog"] || got["recommendations"] {
		t.Fatalf("before logs: %v", got)
	}
	// Access logs name partner-sync, which emits no traces: explained.
	g.Edges["partner-sync->catalog"] = edge("partner-sync", "catalog", model.EdgeGhost, "http")
	if got := subjects(); len(got) != 0 {
		t.Fatalf("after logs: %v", got)
	}
}

func TestUnmatchedReadingsAskForAnAlias(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*model.Node{}, Edges: map[string]*model.Edge{}, Unmatched: []graph.Unmatched{
		{Source: "prometheus", Origin: "redis (instance sessions-exporter:9121)", Candidates: []string{"sessions", "sessions-exporter"}},
		{Source: "prometheus", Origin: "redis (instance sessions-exporter:9121)", Candidates: []string{"sessions", "sessions-exporter"}},
	}}
	fs := unmatchedReadings(Input{Graph: g})
	if len(fs) != 1 {
		t.Fatalf("one finding per thing measured, got %d", len(fs))
	}
	f := fs[0]
	if f.Subject != "sessions" || !strings.Contains(f.Fix.Snippet, `"sessions": the-node-it-measures`) {
		t.Errorf("finding %+v", f)
	}
}
