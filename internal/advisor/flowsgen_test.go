package advisor

import (
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source/declared"
)

func TestFlowsFileLayout(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*model.Node{
		"web":       node("web", model.KindService, model.VisibilityFull, nil),
		"orders":    node("orders", model.KindService, model.VisibilityFull, nil),
		"orders-db": node("orders-db", model.KindDatabase, model.VisibilityPartial, nil),
	}}
	got := FlowsFile(g, "web", []model.Edge{
		{From: "web", To: "orders", Protocol: "http"},
		{From: "web", To: "orders-db", Protocol: "postgresql"},
	})
	want := `apiVersion: teia.nilum.io/v1alpha1
kind: Flows
metadata:
  name: web
spec:
  owner: component:default/web
  egress:
    - to: component:default/orders
      protocol: http
      reason: TODO why this call exists
    - to: resource:default/orders-db
      kind: database
      protocol: postgresql
      reason: TODO why this call exists
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFlowsFileKeepsObservedPortAndExistingRationale(t *testing.T) {
	g := &graph.Graph{Nodes: map[string]*model.Node{"orders": node("orders", model.KindService, model.VisibilityFull, nil), "db": node("db", model.KindDatabase, model.VisibilityPartial, nil)}}
	text := FlowsFile(g, "orders", []model.Edge{{From: "orders", To: "db", Protocol: "postgresql", Port: 5432, Reason: "persist orders"}})
	var doc declared.Document
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	if flow := doc.Spec.Egress[0]; flow.Port != 5432 || flow.Protocol != "postgresql" || flow.Reason != "persist orders" {
		t.Fatalf("lost contract metadata: %+v", flow)
	}
}

// Names come from telemetry: whatever they hold, the generated file must
// stay one valid Flows document carrying exactly those names.
func TestFlowsFileSurvivesHostileNames(t *testing.T) {
	names := []string{
		"evil: name",
		"a #comment",
		"web\nkind: Other",
		"x\n---\napiVersion: v1\nkind: Secret",
		"- item",
		`'single' "double"`,
		"{flow: map}",
		"[seq]",
		"*alias",
		"&anchor",
		"!!binary",
		"%directive",
		"@at",
		"`tick`",
		"null",
		"true",
		"0x1F",
		"~",
		" padded ",
		"trailing \nspace",
		"---",
		"...",
		"tab\there",
		"çãé 漢字",
	}
	for _, name := range names {
		g := &graph.Graph{Nodes: map[string]*model.Node{
			name:        node(name, model.KindService, model.VisibilityFull, nil),
			"db" + name: node("db"+name, model.KindDatabase, model.VisibilityPartial, nil),
		}}
		edges := []model.Edge{
			{From: name, To: name + "-peer", Protocol: name},
			{From: name, To: "db" + name, Protocol: "postgresql"},
		}
		file := FlowsFile(g, name, edges)
		docs, err := declared.Parse([]byte(file))
		if err != nil || len(docs) != 1 {
			t.Errorf("%q: Parse = %d docs, %v\n%s", name, len(docs), err, file)
			continue
		}
		doc := docs[0]
		if errs := declared.Validate(doc); len(errs) > 0 {
			t.Errorf("%q: Validate = %v\n%s", name, errs, file)
		}
		if doc.Metadata.Name != name || doc.Spec.Owner != ref(g, name) || len(doc.Spec.Egress) != 2 {
			t.Errorf("%q: names changed on the way: %+v\n%s", name, doc, file)
			continue
		}
		first, second := doc.Spec.Egress[0], doc.Spec.Egress[1]
		if first.To != ref(g, name+"-peer") || first.Protocol != name || first.Reason != todoReason {
			t.Errorf("%q: first egress = %+v", name, first)
		}
		if second.To != ref(g, "db"+name) || second.Kind != string(model.KindDatabase) {
			t.Errorf("%q: second egress = %+v", name, second)
		}

		// The single entry suggested for an existing file is a valid list
		// item with the same content.
		var entry []declared.Flow
		if err := yaml.Unmarshal([]byte(egressEntry(g, edges[0])), &entry); err != nil || len(entry) != 1 || entry[0] != first {
			t.Errorf("%q: egress entry = %+v, %v", name, entry, err)
		}
	}
}
