package config

import (
	"testing"
	"time"
)

func TestHistoryIsExplicitAndBounded(t *testing.T) {
	cfg, err := Parse([]byte("history:\n  directory: ./history\n  retention: 24h\n  maxBytes: 1048576\n  maxSnapshots: 100\n"))
	if err != nil || cfg.History.Retention != 24*time.Hour || cfg.History.MaxBytes != 1048576 || cfg.History.MaxSnapshots != 100 {
		t.Fatalf("config = %+v (%v)", cfg, err)
	}
	for _, raw := range []string{"history:\n  retention: 24h", "history:\n  directory: x\n  retention: -1h", "history:\n  directory: x\n  maxBytes: -1", "history:\n  directory: x\n  maxSnapshots: 100001"} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("accepted invalid history: %s", raw)
		}
	}
}
