package config

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The configs shipped in the repository load with the real loader, which
// rejects unknown keys and unset variables, so they cannot rot.
func TestShippedConfigsLoad(t *testing.T) {
	// Every ${NAME} without a default that these files reference.
	for name, value := range map[string]string{
		"TEIA_TOKEN":        "example-token",
		"SLACK_WEBHOOK_URL": "https://hooks.slack.com/services/T000/B000/XXXX",
		"HOOK_TOKEN":        "example-hook-token",
		"MIMIR_TOKEN":       "example-mimir-token",
		"TEIA_PG_PASSWORD":  "teia",
	} {
		t.Setenv(name, value)
	}
	tests := []struct {
		path    string
		sources []string
	}{
		{"../../examples/teia.yaml", []string{
			"declared", "prometheus/main", "prometheus/mesh", "tempo/traces", "loki/access-logs",
			"prometheus/kafka-clients", "prometheus/app-calls", "prometheus/egress-flows", "kubernetes", "docker", "postgres/orders-db",
		}},
		{"../../examples/teia-clusters.yaml", nil},
		{"../../lab/teia.yaml", []string{"declared", "prometheus", "tempo", "loki/access-logs", "docker", "postgres/orders-db"}},
		{"../../lab/teia.host.yaml", []string{"declared", "prometheus", "tempo", "loki/access-logs", "docker", "postgres/orders-db"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			cfg, err := Load(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if tt.sources == nil {
				return
			}
			var got []string
			for _, s := range cfg.BuildSources() {
				got = append(got, s.Name())
			}
			if !reflect.DeepEqual(got, tt.sources) {
				t.Errorf("sources = %v, want %v", got, tt.sources)
			}
		})
	}
}

// exampleSkips are keys examples/teia.yaml leaves out on purpose.
var exampleSkips = map[string]string{
	"sources.docker.socket": "a shorthand for sources.docker.host, which the example sets",
}

// Every key teia accepts is set in examples/teia.yaml and listed in
// docs/configuration.md: adding a key to Config means documenting it.
func TestExampleAndReferenceCoverEveryKey(t *testing.T) {
	keys := map[string]bool{}
	structKeys(reflect.TypeOf(Config{}), "", keys)
	// A cluster's sources are the sources above, documented once.
	for k := range keys {
		if strings.HasPrefix(k, "clusters[].sources.") {
			delete(keys, k)
		}
	}

	// examples/teia.yaml maps one scope; the clusters keys are set in
	// examples/teia-clusters.yaml, since a file uses one or the other.
	set := map[string]bool{}
	for _, path := range []string{"../../examples/teia.yaml", "../../examples/teia-clusters.yaml"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		yamlKeys(&doc, "", set)
	}

	reference, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatal(err)
	}

	var sorted []string
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		if _, skip := exampleSkips[k]; !skip && !set[k] {
			t.Errorf("examples/teia.yaml does not set %s", k)
		}
		if !strings.Contains(string(reference), "`"+k+"`") {
			t.Errorf("docs/configuration.md does not list `%s`", k)
		}
	}
	for k := range exampleSkips {
		if !keys[k] {
			t.Errorf("exampleSkips lists %s, which is not a key any more", k)
		}
	}
}

func keyPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// structKeys lists the YAML keys of a config type as dotted paths, with []
// for list items: sources.prometheus[].url.
func structKeys(t reflect.Type, prefix string, out map[string]bool) {
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if f.Tag.Get("yaml") == ",inline" {
			structKeys(f.Type, prefix, out)
			continue
		}
		if name == "" || name == "-" {
			continue
		}
		path := keyPath(prefix, name)
		out[path] = true
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch {
		case ft.Kind() == reflect.Struct:
			structKeys(ft, path, out)
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
			structKeys(ft.Elem(), path+"[]", out)
		}
	}
}

// yamlKeys lists the keys a YAML document sets, in the same notation.
func yamlKeys(n *yaml.Node, prefix string, out map[string]bool) {
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			yamlKeys(c, prefix, out)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			path := keyPath(prefix, n.Content[i].Value)
			out[path] = true
			yamlKeys(n.Content[i+1], path, out)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			yamlKeys(c, prefix+"[]", out)
		}
	}
}
