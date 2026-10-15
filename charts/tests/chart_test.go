package charts_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

// These checks render the real chart. CI installs Helm; ordinary Go-only
// environments skip them rather than requiring Kubernetes or Docker.
func helm(t *testing.T, args ...string) []byte {
	t.Helper()
	bin, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("Helm is required for chart integration tests")
	}
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm %v: %v\n%s", args, err, out)
	}
	return out
}

func render(t *testing.T, values string) map[string]map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(path, []byte(values), 0600); err != nil {
		t.Fatal(err)
	}
	out := helm(t, "template", "teia", "../teia", "--namespace", "teia", "-f", path)
	dec := yaml.NewDecoder(bytes.NewReader(out))
	docs := map[string]map[string]any{}
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if kind, ok := doc["kind"].(string); ok {
			docs[kind] = doc
		}
	}
	return docs
}

func nested(doc map[string]any, keys ...string) any {
	var value any = doc
	for _, key := range keys {
		m, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = m[key]
	}
	return value
}

func TestExistingServiceAccountKeepsItsCredential(t *testing.T) {
	docs := render(t, "rbac:\n  create: false\nserviceAccount:\n  create: false\n  name: platform-reader\n")
	if got := nested(docs["Deployment"], "spec", "template", "spec", "automountServiceAccountToken"); got != true {
		t.Fatalf("existing inventory account lost its credential: automount = %v", got)
	}
	for _, kind := range []string{"ServiceAccount", "ClusterRole", "ClusterRoleBinding"} {
		if _, ok := docs[kind]; ok {
			t.Errorf("chart must not create %s when it is externally managed", kind)
		}
	}
}

func TestTokenRotationRollsOutThePod(t *testing.T) {
	old := render(t, "auth:\n  token: first-token\n")
	updated := render(t, "auth:\n  token: rotated-token\n")
	checksum := func(docs map[string]map[string]any) any {
		return nested(docs["Deployment"], "spec", "template", "metadata", "annotations", "checksum/auth")
	}
	if checksum(old) == nil || checksum(old) == checksum(updated) {
		t.Fatal("changing the chart-managed token must change the pod template")
	}
	external := render(t, "auth:\n  existingSecret: external-auth\n")
	if _, ok := external["Secret"]; ok {
		t.Fatal("must not manage an external Secret")
	}
}

func TestFlowsReloadDoesNotRollOutThePod(t *testing.T) {
	empty := render(t, "{}")
	flows := render(t, "flows:\n  orders.yaml: example\n")
	for _, key := range []string{"checksum/config", "checksum/auth"} {
		before := nested(empty["Deployment"], "spec", "template", "metadata", "annotations", key)
		after := nested(flows["Deployment"], "spec", "template", "metadata", "annotations", key)
		if before != after {
			t.Fatalf("flows should reload without restarting the pod: %s changed", key)
		}
	}
}

func TestCredentialMountCanBeDisabledExplicitly(t *testing.T) {
	docs := render(t, "serviceAccount:\n  automountServiceAccountToken: false\n")
	if got := nested(docs["Deployment"], "spec", "template", "spec", "automountServiceAccountToken"); got != false {
		t.Fatalf("automount = %v, want false", got)
	}
	if got := docs["ServiceAccount"]["automountServiceAccountToken"]; got != false {
		t.Fatalf("ServiceAccount automount = %v, want false", got)
	}
}

func TestChartRejectsIgnoredConfigToken(t *testing.T) {
	bin, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("Helm is required for chart integration tests")
	}
	out, err := exec.Command(bin, "template", "teia", "../teia", "--set", "config.auth.token=ignored-token").CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("token")) {
		t.Fatalf("expected validation to reject a silently ignored token, got %v: %s", err, out)
	}
}
