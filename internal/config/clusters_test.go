package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func loadYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "teia.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestClustersBuildOneScopeEach(t *testing.T) {
	cfg, err := loadYAML(t, `
clusters:
  - name: east
    sources:
      kubernetes: {kubeconfig: /k/east.yaml}
      prometheus: [{url: http://prom-east:9090}]
      tempo: [{url: http://tempo-east:3200}]
  - name: west
    sources:
      queries: [{name: calls, url: http://prom-west:9090, query: up, from: a, to: b}]
sources:
  declared: {paths: [./flows]}
`)
	if err != nil {
		t.Fatal(err)
	}
	scopes := cfg.BuildScopes()
	if len(scopes) != 2 || scopes[0].Cluster != "east" || scopes[1].Cluster != "west" {
		t.Fatalf("scopes = %+v", scopes)
	}
	names := func(i int) []string {
		var out []string
		for _, s := range scopes[i].Sources {
			out = append(out, s.Name())
		}
		return out
	}
	// Each cluster reads its own sources plus the shared declarations, never
	// another cluster's.
	if got := names(0); !slices.Equal(got, []string{"declared", "prometheus", "tempo", "kubernetes"}) {
		t.Fatalf("east sources = %v", got)
	}
	if got := names(1); !slices.Equal(got, []string{"declared", "prometheus/calls"}) {
		t.Fatalf("west sources = %v", got)
	}
	if scopes[0].Sources[0] == scopes[1].Sources[0] {
		t.Fatal("each cluster gets its own copy of the shared sources")
	}
}

func TestWithoutClustersOneUnnamedScope(t *testing.T) {
	cfg, err := loadYAML(t, "sources:\n  prometheus: [{url: http://prom:9090}]\n")
	if err != nil {
		t.Fatal(err)
	}
	if scopes := cfg.BuildScopes(); len(scopes) != 1 || scopes[0].Cluster != "" || len(scopes[0].Sources) != 1 {
		t.Fatalf("scopes = %+v", scopes)
	}
}

func TestClustersValidation(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"telemetry at the top would mix clusters", `
clusters: [{name: east, sources: {prometheus: [{url: http://p:9090}]}}]
sources: {tempo: [{url: http://t:3200}]}`, "sources.tempo describes one cluster: with clusters, move it under clusters[].sources"},
		{"inventory at the top would mix clusters", `
clusters: [{name: east, sources: {prometheus: [{url: http://p:9090}]}}]
sources: {kubernetes: {}}`, "sources.kubernetes describes one cluster"},
		{"declarations belong at the top", `
clusters: [{name: east, sources: {prometheus: [{url: http://p:9090}], declared: {paths: [x]}}}]`, "clusters[0].sources.declared: declarations apply to every cluster"},
		{"names are unique", `
clusters:
  - {name: east, sources: {prometheus: [{url: http://p:9090}]}}
  - {name: east, sources: {prometheus: [{url: http://q:9090}]}}`, `two clusters are named "east"`},
		{"names fit selectors", `
clusters: [{name: "a:b", sources: {prometheus: [{url: http://p:9090}]}}]`, "clusters[0].name"},
		{"names have no slash", `
clusters: [{name: a/b, sources: {prometheus: [{url: http://p:9090}]}}]`, "must not contain a slash"},
		{"a cluster needs sources", `
clusters: [{name: east}]`, "clusters[0] (east) has no sources"},
		{"a cluster's sources are validated where they sit", `
clusters: [{name: east, sources: {prometheus: [{}]}}]`, "clusters[0].sources.prometheus[0].url is required"},
		{"source names are unique within a cluster", `
clusters: [{name: east, sources: {prometheus: [{name: a, url: http://p:9090}, {name: a, url: http://q:9090}]}}]`,
			`cluster "east": two sources are named "prometheus/a"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadYAML(t, tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
	// The same source name in two clusters is fine: they never share a graph.
	if _, err := loadYAML(t, `
clusters:
  - {name: east, sources: {prometheus: [{url: http://p:9090}]}}
  - {name: west, sources: {prometheus: [{url: http://q:9090}]}}`); err != nil {
		t.Fatal(err)
	}
}

// Clusters spread their rounds across the interval, in file order.
func TestClustersStartAtTheirOwnPhase(t *testing.T) {
	cfg, err := loadYAML(t, `
interval: 30s
clusters:
  - {name: a, sources: {prometheus: [{url: http://p:9090}]}}
  - {name: b, sources: {prometheus: [{url: http://p:9090}]}}
  - {name: c, sources: {prometheus: [{url: http://p:9090}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]time.Duration{"a": 0, "b": 10 * time.Second, "c": 20 * time.Second, "": 0} {
		if got := cfg.EngineOptions(name, nil).Phase; got != want {
			t.Errorf("phase of %q = %s, want %s", name, got, want)
		}
	}
}

// One entry under clusterSources covers every cluster of a shared backend:
// each cluster gets its own copy, with $cluster replaced by its name.
func TestClusterSourcesAreReadByEveryCluster(t *testing.T) {
	cfg, err := loadYAML(t, `
clusters:
  - name: east
  - name: west
    sources:
      queries: [{name: batch, url: http://mimir:9009, query: up, from: a, to: b, selector: {cluster: $cluster}}]
      kubernetes: {kubeconfig: /k/west-special.yaml}
clusterSources:
  prometheus: [{url: http://mimir:9009/prometheus, selector: {cluster: $cluster}, headers: {X-Scope-OrgID: tenant-$cluster}}]
  tempo: [{url: http://tempo:3200, selector: {resource.k8s.cluster.name: $cluster}}]
  kubernetes: {kubeconfig: /k/$cluster.yaml}
`)
	if err != nil {
		t.Fatal(err)
	}
	east, west := cfg.SourcesOf(cfg.Clusters[0]), cfg.SourcesOf(cfg.Clusters[1])
	if east.Prometheus[0].Selector["cluster"] != "east" || west.Prometheus[0].Selector["cluster"] != "west" ||
		east.Prometheus[0].Headers["X-Scope-OrgID"] != "tenant-east" || east.Tempo[0].Selector["resource.k8s.cluster.name"] != "east" {
		t.Errorf("$cluster not replaced: east %+v / %+v, west %+v", east.Prometheus, east.Tempo, west.Prometheus)
	}
	if east.Kubernetes.Kubeconfig != "/k/east.yaml" || west.Kubernetes.Kubeconfig != "/k/west-special.yaml" {
		t.Errorf("kubeconfigs: east %q, west %q; a cluster's own inventory replaces the shared one", east.Kubernetes.Kubeconfig, west.Kubernetes.Kubeconfig)
	}
	if len(west.Queries) != 1 || west.Queries[0].Selector["cluster"] != "west" || len(east.Queries) != 0 {
		t.Errorf("a cluster's own lists are added to the shared ones: east %+v, west %+v", east.Queries, west.Queries)
	}
	// The shared entry stays a template: no cluster's name leaks into it.
	if cfg.ClusterSources.Prometheus[0].Selector["cluster"] != "$cluster" {
		t.Errorf("clusterSources changed: %+v", cfg.ClusterSources.Prometheus[0])
	}
	// Unset lists stay unset: the inventory tells them from empty ones (its
	// default excluded namespaces).
	if east.Kubernetes.ExcludeNamespaces != nil {
		t.Errorf("excludeNamespaces became %v", east.Kubernetes.ExcludeNamespaces)
	}
	var names []string
	for _, s := range cfg.BuildScopes()[1].Sources {
		names = append(names, s.Name())
	}
	if !slices.Equal(names, []string{"prometheus", "tempo", "prometheus/batch", "kubernetes"}) {
		t.Errorf("west reads %v", names)
	}
}

func TestClusterSourcesValidation(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"without clusters", `
clusterSources: {prometheus: [{url: http://mimir:9009}]}
sources: {prometheus: [{url: http://prom:9090}]}`, "clusterSources needs clusters"},
		{"declarations", `
clusters: [{name: east}]
clusterSources: {declared: {paths: [./flows]}, prometheus: [{url: http://mimir:9009}]}`, "clusterSources.declared"},
		{"same name as a cluster's own", `
clusters: [{name: east, sources: {prometheus: [{name: main, url: http://prom-east:9090}]}}]
clusterSources: {prometheus: [{name: main, url: http://mimir:9009}]}`, `two sources are named "prometheus/main"`},
		{"no sources at all", `
clusters: [{name: east}]`, "has no sources"},
		{"bad template", `
clusters: [{name: east}]
clusterSources: {tempo: [{url: http://tempo:3200, selector: {"k8s.cluster.name": east}}]}`, "clusterSources.tempo[0].selector"},
		{"placeholder outside a cluster", `
sources: {prometheus: [{url: http://mimir:9009, selector: {cluster: $cluster}}]}`, "$cluster stands for a cluster's name"},
		{"empty selector value", `
clusters: [{name: east, sources: {prometheus: [{url: http://mimir:9009, selector: {cluster: ""}}]}}]`, "clusters[0].sources.prometheus[0].selector"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := loadYAML(t, c.yaml); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v; want %q", err, c.want)
			}
		})
	}
}
