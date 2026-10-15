package prometheus

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
	"go.yaml.in/yaml/v3"
)

func TestResourceCapabilitiesDistinguishMissingZeroAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		fail        bool
	}{{"missing", "", false}, {"measured zero", "0", false}, {"query failed", "", true}} {
		t.Run(tc.name, func(t *testing.T) {
			vector := func(v string) string {
				if v == "" {
					return ""
				}
				return fmt.Sprintf(`{"metric":{"instance":"cache-exporter:9121"},"value":[0,%q]}`, v)
			}
			answers := map[string]string{"redis_memory_used_bytes": vector("90"), "redis_memory_max_bytes > 0": vector("100"), "redis_evicted_keys_total": vector(tc.value), "redis_rejected_connections_total": vector(tc.value)}
			var fail []string
			if tc.fail {
				fail = []string{"redis_evicted_keys_total"}
			}
			srv := fakeProm(t, answers, 0, fail...)
			defer srv.Close()
			var obs source.Observation
			_, _, err := New("metrics", Config{URL: srv.URL}).runProbe(context.Background(), redisProbe, window{promql: "5m", seconds: 300, at: time.Now()}, &obs)
			if err != nil || len(obs.Readings) == 0 {
				t.Fatalf("base pressure disappeared: %+v %v", obs, err)
			}
			if tc.value == "0" {
				if len(obs.Readings) != 3 || len(obs.Findings) != 0 {
					t.Fatalf("measured zero became absent: %+v", obs)
				}
			} else if len(obs.Readings) != 1 || len(obs.Findings) != 1 || obs.Findings[0].Rule != "probe-capability" {
				t.Fatalf("absence became zero or lost diagnosis: %+v", obs)
			}
			if obs.Degraded != tc.fail {
				t.Fatalf("absence and failure conflated: %+v", obs)
			}
		})
	}
}

func TestCachePressureReasonsKeepCountsRulesAndSourceWindow(t *testing.T) {
	now := time.Now()
	src := New("metrics", Config{})
	obs := source.Observation{EvaluatedAt: now, Nodes: []source.NodeObservation{{ID: "cache", Kind: model.KindCache}}, Readings: []source.Reading{
		{Candidates: []string{"cache"}, Window: 5 * time.Minute, Origin: "redis (instance cache-exporter:9121)", Measures: []model.Measure{{Cause: model.CauseEvictions, What: "cache keys evicted in the last 5m", Value: 150}}},
		{Candidates: []string{"cache"}, Window: 5 * time.Minute, Origin: "redis (instance cache-exporter:9121)", Measures: []model.Measure{{Cause: model.CauseRejections, What: "client connections rejected in the last 5m", Value: 2}}},
	}}
	g := graph.BuildAt([]graph.Result{{Source: src, Observation: obs, CollectedAt: now}}, nil, nil, now)
	graph.ApplyHealth(g, graph.DefaultHealth)
	n := g.Nodes["cache"]
	if n.Health != model.HealthFailing || len(n.Reasons) != 2 {
		t.Fatalf("count signals not judged: %+v", n)
	}
	for _, r := range n.Reasons {
		if r.Unit != "count" || r.Rule != "global" || len(r.Evidence) != 1 || r.Evidence[0].WindowSeconds != 300 || !r.Evidence[0].OldestSampleAt.IsZero() {
			t.Fatalf("lost count/window evidence: %+v", r)
		}
		if r.Cause == model.CauseEvictions && !strings.Contains(r.Summary, "may be normal") {
			t.Fatalf("eviction misrepresented as failed requests: %+v", r)
		}
	}
}

func TestRabbitGaugeTimeAndVirtualHostScope(t *testing.T) {
	for _, suffix := range []string{"messages_ready", "messages_unacked", "head_message_timestamp"} {
		if strings.Contains(rabbitTime(suffix), "__name__") || strings.Count(rabbitTime(suffix), "timestamp(") != 2 {
			t.Fatal("metric-name collapse reintroduced")
		}
	}
	if candidates, _ := rabbitSubject(map[string]string{"vhost": "tenant-a", "queue": "events"}); len(candidates) != 1 || candidates[0] != "tenant-a/events" {
		t.Fatalf("queue crossed tenant scope: %v", candidates)
	}
	if candidates, _ := rabbitSubject(map[string]string{"vhost": "/", "queue": "events"}); candidates[0] != "events" {
		t.Fatalf("default queue name changed: %v", candidates)
	}
}

// Run with PROMTOOL=/path/to/promtool. This evaluates the production queries
// against controlled time series rather than asserting only their text.
func TestResourcePromQL(t *testing.T) {
	tool := os.Getenv("PROMTOOL")
	if tool == "" {
		t.Skip("set PROMTOOL to run controlled resource time-series scenarios")
	}
	w := window{promql: "5m", seconds: 300}
	series := func(name, values string) map[string]any { return map[string]any{"series": name, "values": values} }
	inputs := []map[string]any{
		series(`kafka_consumergroup_lag{consumergroup="growing",topic="events"}`, "120+60x5"),
		series(`kafka_consumergroup_current_offset{consumergroup="growing",topic="events",partition="0"}`, "100+60x5"),
		series(`kafka_consumergroup_lag{consumergroup="stopped",topic="events"}`, "600+0x5"),
		series(`kafka_consumergroup_current_offset{consumergroup="stopped",topic="events",partition="0"}`, "100+0x5"),
		series(`kafka_consumergroup_lag{consumergroup="rewound",topic="events"}`, "600+0x5"),
		series(`kafka_consumergroup_current_offset{consumergroup="rewound",topic="events",partition="0"}`, "200 260 20 80 140 200"),
		series(`kafka_consumergroup_lag{consumergroup="unset",topic="events"}`, "600+0x5"),
		series(`kafka_consumergroup_current_offset{consumergroup="unset",topic="events",partition="0"}`, "-1+0x5"),
		series(`rabbitmq_queue_messages_ready{vhost="/",queue="work"}`, "10+0x5"),
		series(`rabbitmq_detailed_queue_messages_ready{vhost="/",queue="work"}`, "12+0x5"),
		series(`rabbitmq_queue_messages_unacked{vhost="/",queue="work"}`, "3+0x5"),
		series(`rabbitmq_detailed_queue_head_message_timestamp{vhost="/",queue="work"}`, "60+0x5"),
		series(`rabbitmq_detailed_queue_head_message_timestamp{vhost="/",queue="empty"}`, "0+0x5"),
		series(`rabbitmq_detailed_queue_head_message_timestamp{vhost="/",queue="future"}`, "600+0x5"),
		series(`redis_evicted_keys_total{instance="cache:9121"}`, "100+2x5"),
		series(`redis_rejected_connections_total{instance="cache:9121"}`, "0+0x5"),
		series(`pg_stat_activity_max_tx_duration{instance="db:9187",state="active"}`, "310+0x5"),
		series(`pg_stat_activity_max_tx_duration{instance="db:9187",state="idle in transaction"}`, "25+0x5"),
		series(`pg_replication_lag_seconds{instance="primary:9187"}`, "500+0x5"),
		series(`pg_replication_is_replica{instance="primary:9187"}`, "0+0x5"),
		series(`pg_replication_lag_seconds{instance="replica:9187"}`, "30+0x5"),
		series(`pg_replication_is_replica{instance="replica:9187"}`, "1+0x5"),
	}
	expected := func(expr, labels string, value float64) map[string]any {
		return map[string]any{"expr": expr, "eval_time": "5m", "exp_samples": []map[string]any{{"labels": labels, "value": value}}}
	}
	cases := []map[string]any{
		expected(kafkaDrain(w), `{consumergroup="growing",topic="events"}`, 420),
		expected(kafkaStalled(w), `{consumergroup="stopped",topic="events"}`, 300),
		expected(rabbitmqProbe.Readings[0].Query(w), `{vhost="/",queue="work"}`, 12),
		expected(rabbitmqProbe.Readings[1].Query(w), `{vhost="/",queue="work"}`, 3),
		expected(rabbitmqProbe.Readings[2].Query(w), `{vhost="/",queue="work"}`, 240),
		expected(rabbitTime("messages_ready"), `{vhost="/",queue="work"}`, 300),
		expected(redisProbe.Readings[2].Query(w), `{instance="cache:9121"}`, 10),
		expected(redisProbe.Readings[3].Query(w), `{instance="cache:9121"}`, 0),
		expected(postgresExporterProbe.Readings[1].Query(w), `{instance="db:9187"}`, 310),
		expected(postgresExporterProbe.Readings[2].Query(w), `{instance="replica:9187"}`, 30),
	}
	fixture := map[string]any{"evaluation_interval": "1m", "tests": []map[string]any{{"interval": "1m", "input_series": inputs, "promql_expr_test": cases}}}
	data, err := yaml.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "resources.yml")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), tool, "test", "rules", file)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("controlled resource queries: %v\n%s", err, output)
	}
}
