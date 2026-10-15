package charts_test

import (
	"strings"
	"testing"
)

func TestHistoryClaimKeepsOneWriterAndWritableMount(t *testing.T) {
	docs := render(t, "persistence:\n  enabled: true\n  size: 2Gi\nflowsConfigMap: externally-managed\nconfig:\n  history:\n    retention: 24h\n")
	if nested(docs["PersistentVolumeClaim"], "spec", "resources", "requests", "storage") != "2Gi" {
		t.Fatal("history claim storage not rendered")
	}
	if nested(docs["PersistentVolumeClaim"], "metadata", "annotations", "helm.sh/resource-policy") != "keep" {
		t.Fatal("uninstall must preserve retained observations")
	}
	if nested(docs["Deployment"], "spec", "strategy", "type") != "Recreate" {
		t.Fatal("rollout would allow two archive writers")
	}
	if nested(docs["Deployment"], "spec", "template", "spec", "securityContext", "fsGroup") != 65532 {
		t.Fatal("history claim is not writable by nonroot image")
	}
	config := nested(docs["ConfigMap"], "data", "teia.yaml").(string)
	if !strings.Contains(config, "directory: /var/lib/teia/history") || !strings.Contains(config, "retention: 24h") {
		t.Fatalf("history config lost: %s", config)
	}
	containers := nested(docs["Deployment"], "spec", "template", "spec", "containers").([]any)
	mounts := containers[0].(map[string]any)["volumeMounts"].([]any)
	found := false
	for _, mount := range mounts {
		m := mount.(map[string]any)
		if m["name"] == "history" {
			found = m["mountPath"] == "/var/lib/teia/history" && m["readOnly"] != true
		}
	}
	if !found {
		t.Fatal("history volume missing or read-only")
	}
	disabled := render(t, "{}")
	if _, exists := disabled["PersistentVolumeClaim"]; exists {
		t.Fatal("history must be opt-in")
	}
	external := render(t, "persistence:\n  enabled: true\n  existingClaim: existing-history\npodSecurityContext:\n  fsGroup: 12345\n")
	if _, exists := external["PersistentVolumeClaim"]; exists {
		t.Fatal("must not create external claim")
	}
	if nested(external["Deployment"], "spec", "template", "spec", "securityContext", "fsGroup") != 12345 {
		t.Fatal("explicit storage group ignored")
	}
}
