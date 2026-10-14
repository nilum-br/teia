package declared

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// bad is a Flows document that fails validation (no owner).
const bad = "apiVersion: teia.nilum.io/v1alpha1\nkind: Flows\nspec:\n  egress:\n    - to: x\n"

func TestInvalidFileBecomesFinding(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "flows.yaml"), bad)
	obs, err := New("declared", []string{dir}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Degraded || len(obs.Findings) != 1 {
		t.Fatalf("want a degraded observation with one finding, got %+v", obs)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		owners  string // owners of the Flows documents read, space separated
		wantErr string
	}{
		{"flows next to a component", flows, "component:default/orders", ""},
		{"leading marker and comments", "# header\n---\napiVersion: teia.nilum.io/v1alpha1\nkind: Flows\nspec: {owner: a}\n", "a", ""},
		{"document end marker", "kind: Component\n...\napiVersion: teia.nilum.io/v1alpha1\nkind: Flows\nspec: {owner: a}\n", "a", ""},
		{"ansible list", "- hosts: all\n  tasks:\n    - ping: {}\n", "", ""},
		{"kind Flows inside a list", "- kind: Flows\n  spec: {owner: a}\n", "", ""},
		{"helm values", "replicaCount: 1\nimage:\n  repository: nginx\n", "", ""},
		{"helm template", "{{- if .Values.enabled }}\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: {{ include \"x.fullname\" . }}\n{{- end }}\n", "", ""},
		{"template before flows", "key: {{ .Values.x }}\nother: [unclosed\n---\napiVersion: teia.nilum.io/v1alpha1\nkind: Flows\nspec: {owner: a}\n---\nkind: Flows\nspec: {owner: b}\n", "a b", ""},
		// A broken document that says it is Flows is reported, with the
		// line number of the file.
		{"broken flows", "kind: Component\n---\napiVersion: teia.nilum.io/v1alpha1\nkind: Flows\nspec:\n\towner: a\n", "", "document 2: yaml: line 6:"},
		{"flows of the wrong shape", "apiVersion: teia.nilum.io/v1alpha1\nkind: Flows\nspec: everything\n", "", "document 1:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			docs, err := Parse([]byte(c.in))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var owners []string
			for _, d := range docs {
				owners = append(owners, d.Spec.Owner)
			}
			if got := strings.Join(owners, " "); got != c.owners {
				t.Errorf("owners = %q, want %q", got, c.owners)
			}
		})
	}
}

// A repository holds much more YAML than flows files: none of it may turn
// into findings.
func TestSourceIgnoresOtherYAML(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "catalog-info.yaml"), flows)
	write(t, filepath.Join(dir, "chart", "templates", "deployment.yaml"), "{{- if .Values.enabled }}\napiVersion: apps/v1\nkind: Deployment\n{{- end }}\n")
	write(t, filepath.Join(dir, "chart", "values.yaml"), "replicaCount: 1\n")
	write(t, filepath.Join(dir, "ansible", "playbook.yml"), "- hosts: all\n  tasks: []\n")
	for _, skipped := range []string{".git", "node_modules", "vendor", "dist", ".terraform"} {
		write(t, filepath.Join(dir, "svc", skipped, "deep", "flows.yaml"), bad)
	}
	// Over the size cap, a broken Flows document is not even read.
	write(t, filepath.Join(dir, "huge.yaml"), bad+strings.Repeat("# padding\n", maxFileSize/10+1))

	// Links inside the tree are not followed, to files or directories.
	outside := t.TempDir()
	write(t, filepath.Join(outside, "flows.yaml"), bad)
	if err := os.Symlink(filepath.Join(outside, "flows.yaml"), filepath.Join(dir, "link.yaml")); err != nil {
		t.Logf("no symlinks here (%v), skipping that part", err)
	} else if err := os.Symlink(outside, filepath.Join(dir, "linked-dir")); err != nil {
		t.Fatal(err)
	}

	obs, err := New("declared", []string{dir}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if obs.Degraded || len(obs.Findings) != 0 {
		t.Errorf("want no findings, got %+v", obs.Findings)
	}
	if len(obs.Edges) != 2 {
		t.Errorf("want the 2 edges of catalog-info.yaml, got %+v", obs.Edges)
	}
}

func TestConfiguredPathMayBeALink(t *testing.T) {
	target := t.TempDir()
	write(t, filepath.Join(target, "flows.yaml"), flows)
	link := filepath.Join(t.TempDir(), "flows")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	obs, err := New("declared", []string{link}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Edges) != 2 {
		t.Errorf("want the edges behind the link, got %+v", obs.Edges)
	}
}

func TestUnreadableFileKeepsTheOthers(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", "flows.yaml"), flows)
	write(t, filepath.Join(dir, "b", "flows.yaml"), bad)
	unreadable := filepath.Join(dir, "b", "flows.yaml")
	readFile = func(name string) ([]byte, error) {
		if name == unreadable {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
		}
		return os.ReadFile(name)
	}
	t.Cleanup(func() { readFile = os.ReadFile })

	obs, err := New("declared", []string{dir}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Edges) != 2 {
		t.Errorf("the readable file was dropped: %+v", obs.Edges)
	}
	if !obs.Degraded || len(obs.Findings) != 1 || obs.Findings[0].Detail != unreadable+": permission denied" {
		t.Errorf("want one finding naming the unreadable file, got %+v", obs.Findings)
	}
}
