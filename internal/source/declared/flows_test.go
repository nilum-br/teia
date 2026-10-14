package declared

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		ref  string
		id   string
		kind model.NodeKind
	}{
		{"component:default/Orders", "orders", model.KindService},
		{"resource:orders-db", "orders-db", model.KindUnknown},
		{"external:api.stripe.com", "api.stripe.com", model.KindExternal},
		{"payments", "payments", model.KindUnknown},
	}
	for _, c := range cases {
		id, kind := ParseRef(c.ref)
		if id != c.id || kind != c.kind {
			t.Errorf("ParseRef(%q) = %q, %q; want %q, %q", c.ref, id, kind, c.id, c.kind)
		}
	}
}

const flows = `apiVersion: backstage.io/v1alpha1
kind: Component
metadata:
  name: orders
---
apiVersion: teia.nilum.io/v1alpha1
kind: Flows
metadata:
  name: orders
spec:
  owner: component:default/orders
  egress:
    - to: component:default/payments
      protocol: http
      reason: charge the customer
    - to: resource:default/orders-db
      kind: database
      protocol: postgresql
`

func TestSourceReadsFlowsAndSkipsOtherKinds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "catalog-info.yaml"), []byte(flows), 0o644); err != nil {
		t.Fatal(err)
	}
	obs, err := New("declared", []string{dir}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Edges) != 2 || !obs.Edges[0].Declared || obs.Edges[0].Reason != "charge the customer" {
		t.Fatalf("unexpected edges: %+v", obs.Edges)
	}
	if obs.Nodes[0].Kind != model.KindService || obs.Nodes[0].Labels["teia.declared"] != "true" {
		t.Errorf("owner node wrong: %+v", obs.Nodes[0])
	}
	if obs.Nodes[2].ID != "orders-db" || obs.Nodes[2].Kind != model.KindDatabase {
		t.Errorf("explicit kind not applied: %+v", obs.Nodes[2])
	}
}

func TestValidate(t *testing.T) {
	doc := Document{APIVersion: "v1", Kind: KindFlows, Spec: Spec{Egress: []Flow{{Kind: "planet", Port: 70000}}}}
	if errs := Validate(doc); len(errs) != 5 {
		t.Errorf("want 5 errors (apiVersion, owner, to, kind, port), got %d: %v", len(errs), errs)
	}
}

func TestInvalidFileBecomesFinding(t *testing.T) {
	dir := t.TempDir()
	bad := "apiVersion: teia.nilum.io/v1alpha1\nkind: Flows\nspec:\n  egress:\n    - to: x\n"
	if err := os.WriteFile(filepath.Join(dir, "flows.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	obs, err := New("declared", []string{dir}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Degraded || len(obs.Findings) != 1 {
		t.Fatalf("want a degraded observation with one finding, got %+v", obs)
	}
}
