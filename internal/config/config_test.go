package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestOnlyBracedEnvIsExpanded(t *testing.T) {
	t.Setenv("TEIA_TEST_SECRET", "s3cret")
	cfg, err := Parse([]byte(`
sources:
  postgres:
    - node: db
      dsn: postgres://teia:${TEIA_TEST_SECRET}@db/x
  queries:
    - url: http://loki:3100
      dialect: loki
      query: sum by (a, b) (rate({app=~"web$"} [$window]))
      from: a
      to: b
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Sources.Postgres[0].DSN; got != "postgres://teia:s3cret@db/x" {
		t.Errorf("dsn = %q", got)
	}
	if got := cfg.Sources.Queries[0].Query; got != `sum by (a, b) (rate({app=~"web$"} [$window]))` {
		t.Errorf("query was mangled: %q", got)
	}
}

func TestEnvDefaultsAndUnsetVariables(t *testing.T) {
	t.Setenv("TEIA_TEST_EMPTY", "")
	for _, name := range []string{"TEIA_TEST_UNSET_A", "TEIA_TEST_UNSET_B", "TEIA_TEST_IN_A_COMMENT"} {
		t.Setenv(name, "") // restored after the test
		os.Unsetenv(name)
	}

	cfg, err := Parse([]byte(`
# ${TEIA_TEST_IN_A_COMMENT} does not count
listen: ${TEIA_TEST_UNSET_A:-:9090}
auth:
  token: ${TEIA_TEST_EMPTY:-fallback}
sources:
  postgres:
    - node: db
      dsn: postgres://teia:${TEIA_TEST_UNSET_B}@db/x
    - node: db2
      dsn: postgres://teia:${TEIA_TEST_UNSET_A}@db/y${TEIA_TEST_UNSET_B}
`))
	if cfg == nil {
		t.Fatalf("config should still be returned with the error: %v", err)
	}
	if cfg.Listen != ":9090" || cfg.Auth.Token != "fallback" {
		t.Errorf("defaults not applied: listen %q, token %q", cfg.Listen, cfg.Auth.Token)
	}
	if cfg.Sources.Postgres[0].DSN != "postgres://teia:@db/x" {
		t.Errorf("an unset variable expands to empty, got %q", cfg.Sources.Postgres[0].DSN)
	}
	if err == nil || !strings.Contains(err.Error(), "environment variables not set: TEIA_TEST_UNSET_A, TEIA_TEST_UNSET_B") {
		t.Errorf("want the unset variables listed once each, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "COMMENT") {
		t.Errorf("comments must not count: %v", err)
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown top-level key", "intervall: 10s", `unknown key "intervall"`},
		{"unknown nested key", "sources:\n  prometheus:\n    - url: http://p\n      preset: [istio]", `line 4: unknown key "preset"`},
		{"negative interval", "interval: -10s", "interval must not be negative"},
		{"negative timeout", "timeout: -1s", "timeout must not be negative"},
		{"negative retention", "retention: -1m", "retention must not be negative"},
		{"negative window", "sources:\n  tempo:\n    - url: http://t\n      window: -5m", "sources.tempo[0].window must not be negative"},
		{"negative query window", "sources:\n  queries:\n    - {url: http://p, query: q, from: a, to: b, window: -1m}", "sources.queries[0].window must not be negative"},
		{"negative stream cap", "maxStreams: -1", "maxStreams must not be negative"},
		{"health out of range", "health: {degraded: 2}", "between 0 and 1"},
		{"degraded above failing", "health: {degraded: 0.2, failing: 0.1}", "health.degraded (0.2) must not be above health.failing (0.1)"},
		{"degraded above the default failing", "health: {degraded: 0.1}", "health.degraded (0.1) must not be above health.failing (0.05)"},
		{"duplicate prometheus names", "sources:\n  prometheus:\n    - {name: main, url: http://a}\n    - {name: main, url: http://b}", `two sources are named "prometheus/main"`},
		{"a query named like a prometheus source", "sources:\n  prometheus:\n    - {name: x, url: http://a}\n  queries:\n    - {name: x, url: http://p, query: q, from: a, to: b}", `two sources are named "prometheus/x"`},
		{"duplicate postgres nodes", "sources:\n  postgres:\n    - {node: db, dsn: x}\n    - {node: db, dsn: y}", `two sources are named "postgres/db"`},
		{"unknown kind", "sources:\n  queries:\n    - {url: http://p, query: q, from: a, to: b, toKind: databse}", `toKind "databse" is not a kind`},
		{"unknown event", "notify:\n  webhooks:\n    - {url: https://hooks.example.com/x, events: [edge.add]}", `unknown event "edge.add"`},
		{"webhook URL", "notify:\n  webhooks:\n    - url: hooks.slack.com/services/SECRET", "notify.webhooks[0].url must be an http or https URL"},
		{"origin with a path", "cors:\n  allowedOrigins: [https://grafana.example.com/d/x]", "cors.allowedOrigins[0] must be an origin"},
		{"origin without scheme", "cors:\n  allowedOrigins: [grafana.example.com]", "cors.allowedOrigins[0] must be an origin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Errorf("errors must not quote webhook URLs: %v", err)
			}
		})
	}
}

func TestParseAccepts(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		check func(*Config) bool
	}{
		{"empty", "", func(c *Config) bool { return c.Listen == ":8080" && c.Interval == 30*time.Second }},
		{"auth, cors and stream cap", "auth: {token: abc}\ncors:\n  allowedOrigins: [https://grafana.example.com, \"*\"]\nmaxStreams: 10",
			func(c *Config) bool {
				return c.Auth.Token == "abc" && len(c.CORS.AllowedOrigins) == 2 && c.MaxStreams == 10
			}},
		{"only failing, above the default degraded", "health: {failing: 0.2}", func(c *Config) bool { return c.HealthThresholds().Failing == 0.2 }},
		{"unnamed sources get numbers", "sources:\n  prometheus:\n    - url: http://a\n    - url: http://b",
			func(c *Config) bool {
				src := c.BuildSources()
				return len(src) == 2 && src[0].Name() == "prometheus/1" && src[1].Name() == "prometheus/2"
			}},
		{"kinds and events", "notify:\n  webhooks:\n    - {url: https://hooks.example.com/x, format: slack, events: [edge.added, finding.opened]}\nsources:\n  queries:\n    - {url: http://p, query: q, from: a, to: b, toKind: database}",
			func(c *Config) bool { return len(c.Notifiers()) == 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if !tt.check(cfg) {
				t.Errorf("unexpected config: %+v", cfg)
			}
		})
	}
}

// The lab's config is what CI's end-to-end run loads.
func TestLabConfigParses(t *testing.T) {
	t.Setenv("TEIA_PG_PASSWORD", "teia")
	cfg, err := Load("../../lab/teia.yaml")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range cfg.BuildSources() {
		names[s.Name()] = true
	}
	for _, want := range []string{"declared", "prometheus", "tempo", "loki/access-logs", "docker", "postgres/orders-db"} {
		if !names[want] {
			t.Errorf("source %s missing, got %v", want, names)
		}
	}
}
