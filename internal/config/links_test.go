package config

import (
	"strings"
	"testing"
)

func TestEvidenceLinkConfigurationIsValidatedWithoutLeakingCredentials(t *testing.T) {
	for _, raw := range []string{"https://user:super-secret@example.com", "javascript:alert(1)", "https://example.com/?token=super-secret"} {
		_, err := Parse([]byte("evidenceLinks:\n  - label: Logs\n    target: node\n    kind: logs\n    url: " + raw + "\n"))
		if err == nil || !strings.Contains(err.Error(), "evidenceLinks[0]") || strings.Contains(err.Error(), "super-secret") {
			t.Fatalf("unsafe config/error: %v", err)
		}
	}
	cfg, err := Parse([]byte(`evidenceLinks:
  - label: Traces
    target: edge
    kind: traces
    url: https://grafana.example/explore
    source: tempo
    explore:
      type: tempo
      datasourceUID: tempo
      query: '{ resource.service.name = {{from|json}} } && { resource.service.name = {{to|json}} }'
`))
	if err != nil || len(cfg.EvidenceLinks) != 1 {
		t.Fatalf("valid template rejected: %v", err)
	}
}
