package config

import (
	"testing"
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
