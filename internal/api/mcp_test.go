package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/slice"
)

// bearer adds the API token to every request an MCP client makes.
type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func mcpSession(t *testing.T, h http.Handler, token string) (*mcp.ClientSession, error) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	transport := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{token}}, MaxRetries: -1}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), transport, nil)
	if err == nil {
		t.Cleanup(func() { _ = session.Close() })
	}
	return session, err
}

// call runs a tool and decodes its structured result.
func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (map[string]any, *mcp.CallToolResult) {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		return nil, res
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: structured content %s: %v", name, raw, err)
	}
	return out, res
}

func errorText(res *mcp.CallToolResult) string {
	if res == nil || !res.IsError || len(res.Content) == 0 {
		return ""
	}
	if text, ok := res.Content[0].(*mcp.TextContent); ok {
		return text.Text
	}
	return ""
}

// mcpEngine is a shared app-b called by app-a (two journeys) and app-c
// (checkout), with Redis behind app-b.
type mcpEngine struct {
	*fakeEngine
	asked []string
}

func (m *mcpEngine) Sliced(sel slice.Selector) model.Snapshot {
	m.asked = append(m.asked, sel.String())
	snap := m.snap
	if sel["journey"] != "checkout" {
		snap.Nodes, snap.Edges = nil, nil
		snap.Slice = &model.SliceScope{Selector: sel, Matched: false, Metrics: "scoped"}
		return snap
	}
	snap.Nodes = []model.Node{{ID: "app-c", Health: model.HealthHealthy}, {ID: "app-b", Health: model.HealthHealthy}}
	snap.Edges = []model.Edge{{From: "app-c", To: "app-b", Observed: true, Declared: true, State: model.EdgeOK, Health: model.HealthHealthy, RequestRate: 0.5}}
	snap.Slice = &model.SliceScope{Selector: sel, Matched: true, Metrics: "scoped", Sources: []string{"prometheus/calls"}}
	return snap
}

func (m *mcpEngine) Changes() []model.Change {
	return []model.Change{{Subject: "first"}, {Subject: "second"}, {Subject: "third"}}
}

func newMCPEngine() *mcpEngine {
	eng := &mcpEngine{fakeEngine: newFakeEngine()}
	eng.snap.Nodes = []model.Node{
		{ID: "app-a", Kind: model.KindService, Health: model.HealthDegraded, Labels: map[string]string{"team": "messaging"},
			Reasons: []model.Reason{{Summary: "2.8% errors, over 1.0%"}}, Evidence: []model.Evidence{{Source: "prometheus/calls"}}},
		{ID: "app-b", Kind: model.KindService, Health: model.HealthHealthy, RequestRate: 3.2, ErrorCoverage: 1},
		{ID: "app-c", Kind: model.KindService, Health: model.HealthHealthy},
		{ID: "redis", Kind: model.KindCache, Health: model.HealthHealthy},
	}
	eng.snap.Edges = []model.Edge{
		{From: "app-a", To: "app-b", Declared: true, Observed: true, State: model.EdgeOK, Health: model.HealthDegraded, RequestRate: 2.7,
			ErrorRate: 0.028, LatencyP95Ms: 240, LatencyApproximate: true, Evidence: []model.Evidence{{Source: "prometheus/calls"}},
			Dimensions: map[string][]string{"journey": {"notification", "whatsapp"}}},
		{From: "app-b", To: "redis", Declared: true, Observed: true, State: model.EdgeOK, Health: model.HealthHealthy, ErrorsUnknown: true},
		{From: "app-c", To: "app-b", Declared: false, Observed: true, State: model.EdgeGhost, Health: model.HealthHealthy, RequestRate: 0.5},
	}
	eng.snap.Findings = []model.Finding{
		{ID: "1", Rule: "undeclared-flow", Severity: model.SeverityWarning, Subject: "app-c->app-b"},
		{ID: "2", Rule: "errors-unmeasured", Severity: model.SeverityInfo, Subject: "app-b->redis"},
	}
	eng.snap.Dimensions = map[string][]string{"journey": {"checkout", "notification", "whatsapp"}}
	return eng
}

func TestMCPListsReadOnlyTools(t *testing.T) {
	s, err := mcpSession(t, Handler(newMCPEngine(), Options{Version: "1.2.3"}), "")
	if err != nil {
		t.Fatal(err)
	}
	if info := s.InitializeResult(); info.ServerInfo.Name != "teia" || info.ServerInfo.Version != "1.2.3" || !strings.Contains(info.Instructions, "errorsUnknown") {
		t.Fatalf("server info = %+v", info)
	}
	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if a := tool.Annotations; a == nil || !a.ReadOnlyHint || a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("%s must be annotated read-only and closed-world: %+v", tool.Name, a)
		}
	}
	slices.Sort(names)
	want := []string{"teia_changes", "teia_component", "teia_connection", "teia_dimensions", "teia_findings", "teia_journeys", "teia_map", "teia_overview"}
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v", names)
	}
}

func TestMCPTools(t *testing.T) {
	eng := newMCPEngine()
	s, err := mcpSession(t, Handler(eng, Options{}), "")
	if err != nil {
		t.Fatal(err)
	}

	overview, _ := call(t, s, "teia_overview", nil)
	if overview["components"] != 4.0 || overview["connections"] != 3.0 {
		t.Fatalf("overview = %v", overview)
	}
	if h := overview["connectionState"].(map[string]any); h["ok"] != 2.0 || h["ghost"] != 1.0 {
		t.Fatalf("connection states = %v", h)
	}

	whole, _ := call(t, s, "teia_map", nil)
	edges := whole["edges"].([]any)
	if len(whole["nodes"].([]any)) != 4 || len(edges) != 3 {
		t.Fatalf("map = %v", whole)
	}
	ab := edges[0].(map[string]any)
	if ab["latencyApproximate"] != true || ab["requestRate"] != 2.7 || ab["evidence"] != nil {
		t.Fatalf("map edges are compact and keep the approximate mark: %v", ab)
	}
	if redis := edges[1].(map[string]any); redis["errorsUnknown"] != true || redis["errorRate"] != nil {
		t.Fatalf("unknown errors must stay unknown, never zero: %v", redis)
	}
	// A measured zero must read as zero, not as a missing value.
	if cb := edges[2].(map[string]any); cb["errorRate"] != 0.0 || cb["errorsUnknown"] != nil {
		t.Fatalf("measured zero errors must be explicit: %v", cb)
	}
	if b := whole["nodes"].([]any)[1].(map[string]any); b["errorRate"] != 0.0 {
		t.Fatalf("a component's measured zero must be explicit too: %v", b)
	}
	around, _ := call(t, s, "teia_map", map[string]any{"node": "redis"})
	if len(around["nodes"].([]any)) != 2 || len(around["edges"].([]any)) != 1 {
		t.Fatalf("redis neighbourhood = %v", around)
	}
	if _, res := call(t, s, "teia_map", map[string]any{"node": "nope"}); !strings.Contains(errorText(res), `no component "nope"`) {
		t.Fatalf("unknown node: %v", res)
	}

	comp, _ := call(t, s, "teia_component", map[string]any{"id": "app-b"})
	if len(comp["incoming"].([]any)) != 2 || len(comp["outgoing"].([]any)) != 1 || len(comp["findings"].([]any)) != 2 {
		t.Fatalf("component app-b = %v", comp)
	}
	comp, _ = call(t, s, "teia_component", map[string]any{"id": "app-a"})
	if c := comp["component"].(map[string]any); c["evidence"] == nil || c["reasons"] == nil {
		t.Fatalf("a component in full keeps its evidence and reasons: %v", c)
	}

	conn, _ := call(t, s, "teia_connection", map[string]any{"from": "app-a", "to": "app-b"})
	if c := conn["connections"].([]any)[0].(map[string]any); c["evidence"] == nil || c["dimensions"] == nil {
		t.Fatalf("connection in full = %v", c)
	}
	if _, res := call(t, s, "teia_connection", map[string]any{"from": "redis", "to": "app-a"}); errorText(res) == "" {
		t.Fatal("a missing connection is a tool error")
	}

	warn, _ := call(t, s, "teia_findings", map[string]any{"severity": "warning"})
	if f := warn["findings"].([]any); len(f) != 1 || f[0].(map[string]any)["rule"] != "undeclared-flow" {
		t.Fatalf("warnings = %v", warn)
	}
	if _, res := call(t, s, "teia_findings", map[string]any{"severity": "loud"}); errorText(res) == "" {
		t.Fatal("an unknown severity is a tool error")
	}

	changes, _ := call(t, s, "teia_changes", map[string]any{"limit": 2})
	if c := changes["changes"].([]any); len(c) != 2 || c[0].(map[string]any)["subject"] != "third" {
		t.Fatalf("newest changes first, limited: %v", changes)
	}

	dims, _ := call(t, s, "teia_dimensions", nil)
	if d := dims["dimensions"].(map[string]any)["journey"].([]any); len(d) != 3 {
		t.Fatalf("dimensions = %v", dims)
	}
	journeys, _ := call(t, s, "teia_journeys", nil)
	if _, ok := journeys["journeys"]; !ok {
		t.Fatalf("journeys = %v", journeys)
	}
}

func TestMCPSlices(t *testing.T) {
	eng := newMCPEngine()
	s, err := mcpSession(t, Handler(eng, Options{}), "")
	if err != nil {
		t.Fatal(err)
	}
	checkout, _ := call(t, s, "teia_map", map[string]any{"slice": "journey:checkout"})
	if e := checkout["edges"].([]any); len(e) != 1 || e[0].(map[string]any)["from"] != "app-c" {
		t.Fatalf("checkout slice = %v", checkout)
	}
	if sl := checkout["slice"].(map[string]any); sl["matched"] != true || sl["metrics"] != "scoped" {
		t.Fatalf("the slice scope travels with the data: %v", sl)
	}
	missing, _ := call(t, s, "teia_overview", map[string]any{"slice": "tenant:a,journey:none"})
	if missing["components"] != 0.0 || missing["slice"].(map[string]any)["matched"] != false {
		t.Fatalf("a selection no call carried is empty and says so: %v", missing)
	}
	if !slices.Equal(eng.asked, []string{"journey:checkout", "journey:none,tenant:a"}) {
		t.Fatalf("selectors = %v", eng.asked)
	}
	if _, res := call(t, s, "teia_map", map[string]any{"slice": "journey"}); !strings.Contains(errorText(res), "dimension:value") {
		t.Fatalf("a bad selector is a tool error: %v", res)
	}

	plain, err := mcpSession(t, Handler(newFakeEngine(), Options{}), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, res := call(t, plain, "teia_map", map[string]any{"slice": "journey:a"}); !strings.Contains(errorText(res), "cannot slice") {
		t.Fatalf("an engine that cannot slice must say so: %v", res)
	}
}

func TestMCPRequiresTheToken(t *testing.T) {
	h := Handler(newMCPEngine(), Options{Token: "s3cret"})
	if _, err := mcpSession(t, h, ""); err == nil {
		t.Fatal("without the token an MCP client must not connect")
	}
	if _, err := mcpSession(t, h, "wrong"); err == nil {
		t.Fatal("a wrong token must be refused")
	}
	s, err := mcpSession(t, h, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := call(t, s, "teia_overview", nil); out["components"] != 4.0 {
		t.Fatalf("overview with the token = %v", out)
	}
	// Browsers may not read /mcp from another site: CORS covers /api/v1/* only.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	Handler(newMCPEngine(), Options{AllowedOrigins: []string{"*"}}).ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("CORS must not open /mcp: %v", rec.Header())
	}
}
