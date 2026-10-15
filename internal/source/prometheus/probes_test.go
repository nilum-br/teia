package prometheus

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

// readings indexes an observation's readings by first candidate, then target.
func readings(obs source.Observation) map[string][]model.Measure {
	out := map[string][]model.Measure{}
	for _, r := range obs.Readings {
		key := r.Candidates[0]
		if len(r.To) > 0 {
			key += "->" + r.To[0]
		}
		out[key] = append(out[key], r.Measures...)
	}
	return out
}

func measure(ms []model.Measure, what string) (model.Measure, bool) {
	i := slices.IndexFunc(ms, func(m model.Measure) bool { return m.What == what })
	if i < 0 {
		return model.Measure{}, false
	}
	return ms[i], true
}

func TestGaugeSampleTimesRequireEveryInputAndStayPerReading(t *testing.T) {
	old := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, valueTime, capacityTime string
		failTiming                    bool
		want                          time.Time
	}{
		{"older capacity", fmt.Sprint(old.Add(time.Hour).Unix()), fmt.Sprint(old.Unix()), false, old},
		{"older value", fmt.Sprint(old.Unix()), fmt.Sprint(old.Add(time.Hour).Unix()), false, old},
		{"missing capacity time", fmt.Sprint(old.Unix()), "", false, time.Time{}},
		{"missing value time", "", fmt.Sprint(old.Unix()), false, time.Time{}},
		{"invalid capacity time", fmt.Sprint(old.Unix()), "0", false, time.Time{}},
		{"timestamp query failure", fmt.Sprint(old.Unix()), fmt.Sprint(old.Unix()), true, time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vector := func(v string) string {
				if v == "" {
					return ""
				}
				return fmt.Sprintf(`{"metric":{"instance":"cache-exporter:9121"},"value":[1792065600,%q]}`, v)
			}
			answers := map[string]string{
				"redis_memory_used_bytes": vector("48"), "redis_memory_max_bytes > 0": vector("64"),
				"redis_connected_clients": vector("10"), "redis_config_maxclients > 0": vector("100"),
				redisProbe.Readings[0].SampleTimes[0]: vector(tc.valueTime), redisProbe.Readings[0].SampleTimes[1]: vector(tc.capacityTime),
				redisProbe.Readings[1].SampleTimes[0]: vector(fmt.Sprint(old.Add(2 * time.Hour).Unix())),
				redisProbe.Readings[1].SampleTimes[1]: vector(fmt.Sprint(old.Add(3 * time.Hour).Unix())),
			}
			var failures []string
			if tc.failTiming {
				failures = append(failures, redisProbe.Readings[0].SampleTimes[1])
			}
			srv := fakeProm(t, answers, 0, failures...)
			defer srv.Close()
			obs, err := New("metrics", Config{URL: srv.URL, Presets: []string{"servicegraph"}, Probes: []string{"redis"}}).Discover(context.Background())
			if err != nil || len(obs.Readings) != 2 {
				t.Fatalf("missing timing must preserve both measured readings: %+v, %v", obs, err)
			}
			memory, clients := obs.Readings[0], obs.Readings[1]
			if !memory.OldestSampleAt.Equal(tc.want) || !clients.OldestSampleAt.Equal(old.Add(2*time.Hour)) {
				t.Errorf("sample times crossed readings or ignored missing capacity: %v, %v", memory.OldestSampleAt, clients.OldestSampleAt)
			}
			if memory.Measures[0].Value != 48 || memory.Measures[0].Max != 64 || memory.Window != 0 {
				t.Errorf("a gauge's value/capacity/window changed: %+v", memory)
			}
			if tc.failTiming && !strings.Contains(obs.Message, "sample timing unavailable") {
				t.Errorf("timestamp query failure was hidden: %s", obs.Message)
			}
		})
	}
}

func TestPresetAndProbeQueriesShareOneEvaluation(t *testing.T) {
	var times []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		times = append(times, r.URL.Query().Get("time"))
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"client":"web","server":"orders","instance":"cache-exporter:9121"},"value":[1700000000,"100"]}]}}`)
	}))
	defer srv.Close()
	obs, err := New("metrics", Config{URL: srv.URL, Presets: []string{"servicegraph"}, Probes: []string{"redis"}}).Discover(context.Background())
	if err != nil || len(obs.Edges) != 1 || len(obs.Readings) != 4 || len(times) != 14 {
		t.Fatalf("expected rate, errors, p50/p95, and two gauges with input timestamps: %v %+v, %v", times, obs, err)
	}
	for _, at := range times {
		if at == "" || at != obs.EvaluatedAt.Format(time.RFC3339Nano) {
			t.Fatalf("one source round must use one evaluation time: %v", times)
		}
	}
	if !obs.Edges[0].ObservedAt.IsZero() {
		t.Fatal("gauge timing must not claim the original time of unrelated RED metrics")
	}
}

func TestTimingCancellationKeepsMeasurementsFromEveryProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if strings.Contains(query, "timestamp(") {
			// Cancel while the optional query is in flight. Queries for later
			// readings/probes would fail if their values had not been fetched.
			cancel()
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"client":"web","server":"orders","instance":"cache-exporter:9121"},"value":[0,"10"]}]}}`)
	}))
	defer srv.Close()
	obs, err := New("metrics", Config{URL: srv.URL, Presets: []string{"servicegraph"}, Probes: []string{"redis", "mysql"}}).Discover(ctx)
	if err != nil || len(obs.Edges) != 1 || len(obs.Readings) != 6 {
		t.Fatalf("timing cancellation discarded measured traffic or probe values: %+v, %v", obs, err)
	}
	for _, reading := range obs.Readings {
		if reading.Measures[0].Value != 10 || !reading.OldestSampleAt.IsZero() {
			t.Errorf("timing failure changed a value or supplied a sample time: %+v", reading)
		}
	}
	if !obs.Degraded || !strings.Contains(obs.Message, "sample timing unavailable: redis:") || !strings.Contains(obs.Message, "mysql:") {
		t.Errorf("both probes must report unknown timing: %+v", obs)
	}
}

func TestInvalidSampleTimeDoesNotBreakJSON(t *testing.T) {
	for _, v := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1), 253402300800} {
		if !sampleTime(v).IsZero() {
			t.Errorf("invalid sample time accepted: %v", v)
		}
	}
	if got := sampleTime(1700000000.5); got.UnixNano() != 1700000000500000000 {
		t.Fatalf("fractional Unix seconds lost precision: %v", got)
	}
}

func TestProbesReadExporters(t *testing.T) {
	srv := fakeProm(t, map[string]string{
		"count(redis_memory_used_bytes)": `{"metric":{},"value":[0,"1"]}`,
		"count(kafka_consumergroup_lag)": `{"metric":{},"value":[0,"4"]}`,
		"server) (redis_memory_used_bytes)": `{"metric":{"instance":"catalog-cache-exporter:9121","job":"redis"},"value":[0,"48000000"]},
			{"metric":{"instance":"10.0.0.7:9121","job":"redis"},"value":[0,"1000"]}`,
		"redis_memory_max_bytes > 0": `{"metric":{"instance":"catalog-cache-exporter:9121","job":"redis"},"value":[0,"64000000"]}`,
		// sessions-cache has no maxmemory: no capacity, so no saturation.
		"sum by (consumergroup, topic) (kafka_consumergroup_lag) / ": `{"metric":{"consumergroup":"email-worker","topic":"order-events"},"value":[0,"750"]}`,
		kafkaStalled(window{promql: "5m", seconds: 300}):             `{"metric":{"consumergroup":"billing","topic":"invoices"},"value":[0,"300"]}`,
		"sum by (consumergroup, topic) (kafka_consumergroup_lag)": `{"metric":{"consumergroup":"email-worker","topic":"order-events"},"value":[0,"45210"]},
			{"metric":{"consumergroup":"billing","topic":"invoices"},"value":[0,"12"]}`,
		"sum by (topic) (kafka_consumergroup_lag)": `{"metric":{"topic":"order-events"},"value":[0,"45210"]}`,
	}, 0)
	defer srv.Close()

	obs, err := New("prometheus", Config{URL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := readings(obs)

	if m, ok := measure(got["catalog-cache"], "memory"); !ok || m.Value != 48e6 || m.Max != 64e6 || m.Cause != model.CauseSaturation {
		t.Errorf("redis memory on catalog-cache (not its exporter): %+v (%v); all: %v", m, ok, got)
	}
	if _, ok := got[source.AddressID("10.0.0.7")]; ok {
		t.Error("a redis without maxmemory has no capacity to be saturated against")
	}
	if m, ok := measure(got["email-worker->order-events"], "messages waiting"); !ok || m.Value != 45210 || m.Cause != model.CauseBacklog {
		t.Errorf("consumer backlog on its line to the topic: %+v (%v)", m, ok)
	}
	if m, ok := measure(got["email-worker->order-events"], "estimated drain time with no new arrivals"); !ok || m.Value != 750 || m.Cause != model.CauseLag {
		t.Errorf("time behind: %+v (%v)", m, ok)
	}
	if m, ok := measure(got["billing->invoices"], "without progress"); !ok || m.Value != 300 {
		t.Errorf("a stalled group is a window without progress: %+v (%v)", m, ok)
	}
	if m, ok := measure(got["order-events"], "messages waiting"); !ok || m.Value != 45210 {
		t.Errorf("the topic carries every group's lag: %+v (%v)", m, ok)
	}
	if !strings.Contains(obs.Message, "probes: redis (1), kafka (5)") {
		t.Errorf("message %q should say which probes read what", obs.Message)
	}
}

func TestFailingProbeKeepsTheRest(t *testing.T) {
	srv := fakeProm(t, map[string]string{
		"count(traces_service_graph_request_total)": `{"metric":{},"value":[0,"1"]}`,
		"count(redis_memory_used_bytes)":            `{"metric":{},"value":[0,"1"]}`,
		"rate(traces_service_graph_request_total":   `{"metric":{"client":"web","server":"orders"},"value":[0,"4"]}`,
	}, 0, "server) (redis_memory_used_bytes)")
	defer srv.Close()
	obs, err := New("prometheus", Config{URL: srv.URL}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := edges(obs)["web->orders"]; !ok {
		t.Error("a failing probe must not hide edges")
	}
	if !obs.Degraded || !strings.Contains(obs.Message, "probe failed: redis") {
		t.Errorf("degraded=%v message=%q", obs.Degraded, obs.Message)
	}
}

func TestProbesCanBeTurnedOff(t *testing.T) {
	srv := fakeProm(t, map[string]string{
		"count(redis_memory_used_bytes)":    `{"metric":{},"value":[0,"1"]}`,
		"server) (redis_memory_used_bytes)": `{"metric":{"instance":"cache:9121"},"value":[0,"1"]}`,
		"redis_memory_max_bytes > 0":        `{"metric":{"instance":"cache:9121"},"value":[0,"2"]}`,
	}, 0)
	defer srv.Close()
	obs, err := New("prometheus", Config{URL: srv.URL, Probes: []string{"none"}}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Readings) != 0 {
		t.Errorf("probes: [none] reads nothing, got %v", obs.Readings)
	}
}

func TestExporterSubjectPrefersWhatIsMeasured(t *testing.T) {
	cases := []struct {
		labels map[string]string
		want   []string
	}{
		{map[string]string{"instance": "orders-db-exporter:9187", "job": "postgres"},
			[]string{"orders-db", "postgres", "orders-db-exporter"}},
		{map[string]string{"service": "prometheus-redis-exporter", "pod": "prometheus-redis-exporter-6d5f7b8c9d-x2k4p", "instance": "10.1.2.3:9121", "job": "redis"},
			[]string{"redis", "redis", "prometheus-redis-exporter", "ip:10.1.2.3"}},
		{map[string]string{"instance": "10.1.2.3:9121"}, []string{"ip:10.1.2.3"}},
	}
	for _, c := range cases {
		got, _ := exporterSubject(c.labels)
		want := nonEmpty(c.want...)
		if !slices.Equal(got, want) {
			t.Errorf("exporterSubject(%v) = %v, want %v", c.labels, got, want)
		}
	}
}

func TestWorkloadOf(t *testing.T) {
	for pod, want := range map[string]string{
		"orders-7d9f8c6b5-x2k4p": "orders",
		"orders-db-0":            "orders-db",
		"node-exporter-h2k9x":    "node-exporter",
		"plain":                  "plain",
	} {
		if got := workloadOf(pod); got != want {
			t.Errorf("workloadOf(%q) = %q, want %q", pod, got, want)
		}
	}
	got, _ := workloadSubject(map[string]string{"container_label_com_docker_compose_service": "orders", "name": "teia-lab-orders-1"})
	if got[0] != "orders" {
		t.Errorf("a Compose service names the workload first: %v", got)
	}
}

func TestEveryProbeIsUsable(t *testing.T) {
	w := window{promql: "5m", seconds: 300}
	for _, p := range Probes {
		if p.Name == "" || p.Detect == "" || p.Description == "" || len(p.Readings) == 0 {
			t.Errorf("%+v is incomplete", p)
		}
		for _, r := range p.Readings {
			if r.Query == nil || r.Subject == nil || r.What == "" || r.Query(w) == "" {
				t.Errorf("%s: incomplete reading %+v", p.Name, r)
			}
			if r.Cause == model.CauseSaturation && r.Max == nil && !r.Share {
				t.Errorf("%s %s: saturation needs a capacity", p.Name, r.What)
			}
		}
	}
}
