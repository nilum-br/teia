package charts_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/nilum-br/teia/internal/config"
)

// With config.clusters the chart renders a config teia accepts: the chart's
// default top-level inventory would mix clusters, so it is dropped, and the
// shared flows stay. The kubeconfig Secret is mounted read-only.
func TestClustersRenderAValidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "values.yaml")
	values := `
kubeconfigSecret: teia-kubeconfigs
config:
  clusters:
    - name: east
      sources:
        kubernetes: {kubeconfig: /etc/teia/kube/east.yaml}
        prometheus: [{url: http://prom-east:9090}]
    - name: west
      sources:
        kubernetes: {}
        tempo: [{url: http://tempo-west:3200}]
`
	if err := os.WriteFile(path, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	out := helm(t, "template", "teia", "../teia", "--namespace", "teia", "-f", path)
	var teiaYAML string
	var deployment map[string]any
	dec := yaml.NewDecoder(bytes.NewReader(out))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if s, ok := nested(doc, "data", "teia.yaml").(string); ok {
			teiaYAML = s
		}
		if doc["kind"] == "Deployment" {
			deployment = doc
		}
	}
	file := filepath.Join(t.TempDir(), "teia.yaml")
	if err := os.WriteFile(file, []byte(teiaYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEIA_TOKEN", "")
	cfg, err := config.Load(file)
	if err != nil {
		t.Fatalf("rendered config rejected: %v\n%s", err, teiaYAML)
	}
	scopes := cfg.BuildScopes()
	if len(scopes) != 2 || scopes[0].Cluster != "east" || scopes[1].Cluster != "west" {
		t.Fatalf("scopes = %+v", scopes)
	}
	var names []string
	for _, s := range scopes[1].Sources {
		names = append(names, s.Name())
	}
	if !slices.Equal(names, []string{"declared", "tempo", "kubernetes"}) {
		t.Fatalf("west sources = %v: the shared flows and its own sources only", names)
	}

	spec := nested(deployment, "spec", "template", "spec").(map[string]any)
	mounted := false
	for _, m := range nested(spec["containers"].([]any)[0].(map[string]any), "volumeMounts").([]any) {
		mount := m.(map[string]any)
		if mount["mountPath"] == "/etc/teia/kube" && mount["readOnly"] == true {
			mounted = true
		}
	}
	found := false
	for _, v := range spec["volumes"].([]any) {
		if nested(v.(map[string]any), "secret", "secretName") == "teia-kubeconfigs" {
			found = true
		}
	}
	if !mounted || !found {
		t.Fatalf("kubeconfig Secret mounted read-only: mount %v, volume %v", mounted, found)
	}
}
