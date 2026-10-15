package evidencelinks

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

func TestLinksPreserveIdentityEncodingAndEvidencePeriod(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	id := "orders & payments/東京\"\\?"
	s := model.Snapshot{Nodes: []model.Node{{ID: id, Tech: "redis", Labels: map[string]string{"k8s.namespace": "prod & blue"}}, {ID: "external"}}, Edges: []model.Edge{{From: id, To: "external", Protocol: "http", Evidence: []model.Evidence{{Source: "metrics", EvaluatedAt: now, CollectedAt: now.Add(time.Hour), WindowSeconds: 120}}}}}
	templates := []Template{
		{Label: "Dashboard", Kind: "dashboard", Target: "node", Source: "metrics", URL: "https://grafana.example/d/service", Params: map[string]string{"var-service": "{{id}}", "var-namespace": "{{namespace}}", "var-tech": "{{tech}}", "from": "{{period.fromMs}}", "to": "{{period.toMs}}"}},
		{Label: "Connection", Kind: "dashboard", Target: "edge", Source: "metrics", URL: "https://grafana.example/d/connection", Params: map[string]string{"var-from": "{{from}}", "var-to": "{{to}}", "var-namespace": "{{fromNamespace}}", "from": "{{period.from}}"}},
	}
	Apply(&s, templates)
	if len(s.Nodes[0].Links) != 1 || len(s.Nodes[1].Links) != 0 || len(s.Edges[0].Links) != 1 {
		t.Fatalf("missing context was not respected: %+v", s)
	}
	link := s.Nodes[0].Links[0]
	u, err := url.Parse(link.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("var-service") != id || u.Query().Get("var-namespace") != "prod & blue" || u.Query().Get("var-tech") != "redis" || len(u.Query()) != 5 {
		t.Fatalf("context injected/lost URL parameters: %s", link.URL)
	}
	if !link.PeriodFrom.Equal(now.Add(-2*time.Minute)) || !link.PeriodTo.Equal(now) {
		t.Fatalf("collection time substituted for evidence period: %+v", link)
	}
	u, _ = url.Parse(s.Edges[0].Links[0].URL)
	if u.Query().Get("var-from") != id || u.Query().Get("var-to") != "external" {
		t.Fatalf("connection identity lost: %s", u)
	}
}

func TestExploreEscapesQueryAndJSONAndRequiresActualPeriod(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	id := "service\"} || {true} \\ 東京"
	template := Template{Label: "Search traces", Target: "node", Kind: "traces", Source: "tempo", URL: "http://localhost:3480/explore", Explore: &Explore{Type: "tempo", DatasourceUID: "tempo", Query: `{ resource.service.name = {{id|json}} }`}}
	s := model.Snapshot{Nodes: []model.Node{{ID: id}}, Edges: []model.Edge{{From: id, To: "db", Evidence: []model.Evidence{{Source: "tempo", ObservedFrom: now.Add(-time.Minute), ObservedAt: now, CollectedAt: now.Add(time.Hour), WindowSeconds: 600}}}}}
	Apply(&s, []Template{template})
	if len(s.Nodes[0].Links) != 1 {
		t.Fatal("node must use the known period of incident evidence")
	}
	link := s.Nodes[0].Links[0]
	u, _ := url.Parse(link.URL)
	var panes map[string]struct {
		Queries []struct {
			Query     string `json:"query"`
			QueryType string `json:"queryType"`
		} `json:"queries"`
		Range map[string]string `json:"range"`
	}
	if err := json.Unmarshal([]byte(u.Query().Get("panes")), &panes); err != nil {
		t.Fatal(err)
	}
	quoted, _ := json.Marshal(id)
	if panes["teia"].Queries[0].Query != `{ resource.service.name = `+string(quoted)+` }` || panes["teia"].Queries[0].QueryType != "traceql" || u.Query().Get("schemaVersion") != "1" {
		t.Fatalf("wrong Explore state: %s", u.Query().Get("panes"))
	}
	if !strings.Contains(link.Description, "not an exact trace") || !link.PeriodFrom.Equal(now.Add(-time.Minute)) {
		t.Fatalf("misleading trace correlation or time: %+v", link)
	}
	s.Edges[0].Evidence[0].ObservedFrom = time.Time{}
	Apply(&s, []Template{template})
	if len(s.Nodes[0].Links) != 0 {
		t.Fatal("collection time/window must not fabricate the original search interval")
	}
}

func TestUnsafeOrIncompleteTemplatesDoNotReachPublicAPI(t *testing.T) {
	base := Template{Label: "Evidence", Target: "node", Kind: "dashboard", URL: "https://grafana.example/d/service"}
	for _, raw := range []string{"javascript:alert(1)", "data:text/html,hi", "//evil.example", "https://user:password@example.com", "https://example.com/?token=secret", "https://example.com/#secret", "https://example.com\\evil"} {
		t.Run(raw, func(t *testing.T) {
			cfg := base
			cfg.URL = raw
			if Validate(cfg) == nil {
				t.Fatal("unsafe URL accepted")
			}
			s := model.Snapshot{Nodes: []model.Node{{ID: "a"}}}
			Apply(&s, []Template{cfg})
			if len(s.Nodes[0].Links) > 0 {
				t.Fatal("unsafe URL exposed")
			}
		})
	}
	for _, name := range []string{"token", "source.url", "headers", "dsn", "unknown"} {
		cfg := base
		cfg.Params = map[string]string{"var-id": "{{" + name + "}}"}
		if Validate(cfg) == nil {
			t.Fatalf("non-whitelisted context accepted: %s", name)
		}
	}
	cfg := base
	cfg.Params = map[string]string{"api_key": "secret"}
	if Validate(cfg) == nil {
		t.Fatal("credential param allowed")
	}
	cfg = base
	cfg.Kind = "logs"
	cfg.Explore = &Explore{Type: "loki", DatasourceUID: "loki", Query: `{service="{{id}}"}`}
	if Validate(cfg) == nil {
		t.Fatal("unquoted Explore substitution allowed")
	}
	cfg = base
	cfg.Requires = []string{"namespace"}
	s := model.Snapshot{Nodes: []model.Node{{ID: "a"}}}
	Apply(&s, []Template{cfg})
	if len(s.Nodes[0].Links) != 0 {
		t.Fatal("required namespace was invented")
	}
}

func TestPeriodsAreSourceScopedAndRetainedEvidenceKeepsItsTime(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	ev := []model.Evidence{{Source: "tempo", ObservedFrom: now.Add(-time.Hour), ObservedAt: now.Add(-30 * time.Minute), Retained: true}, {Source: "logs", EvaluatedAt: now, WindowSeconds: 60}}
	from, to := period(ev, "tempo")
	if !from.Equal(now.Add(-time.Hour)) || !to.Equal(now.Add(-30*time.Minute)) {
		t.Fatal("retention or another source changed the trace period")
	}
	from, to = period(ev, "absent")
	if !from.IsZero() || !to.IsZero() {
		t.Fatal("unknown source inherited other evidence")
	}
}

func TestMillisecondSearchBoundsIncludeFractionalAndPointObservations(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 123456789, time.UTC)
	for _, end := range []time.Time{now, now.Add(time.Millisecond)} {
		link, ok := build(Template{Label: "Range", Kind: "dashboard", Target: "node", URL: "https://example.com/d/a", Params: map[string]string{"from": "{{period.fromMs}}", "to": "{{period.toMs}}"}}, map[string]string{"id": "a"}, []model.Evidence{{ObservedFrom: now, ObservedAt: end}})
		if !ok {
			t.Fatal("known observation should be queryable")
		}
		u, _ := url.Parse(link.URL)
		from, _ := strconv.ParseInt(u.Query().Get("from"), 10, 64)
		to, _ := strconv.ParseInt(u.Query().Get("to"), 10, 64)
		if from >= to || time.UnixMilli(from).After(now) || time.UnixMilli(to).Before(end) {
			t.Fatalf("range truncates evidence: %s", link.URL)
		}
		if !link.PeriodFrom.Equal(now) || !link.PeriodTo.Equal(end) {
			t.Fatal("original evidence bounds changed")
		}
	}
}
