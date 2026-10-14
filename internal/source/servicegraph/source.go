// Package servicegraph discovers communication from service graph metrics
// (traces_service_graph_request_total and friends) stored in Prometheus.
//
// Those metrics are produced from traces by the OpenTelemetry Collector
// servicegraph connector or by the Tempo metrics-generator, so this source
// works with anything that emits OpenTelemetry traces, Beyla included.
package servicegraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

const (
	DefaultRequestMetric = "traces_service_graph_request_total"
	DefaultFailedMetric  = "traces_service_graph_request_failed_total"
	DefaultWindow        = 5 * time.Minute
	// VirtualClient is the node name used for callers without telemetry.
	VirtualClient = "user"
	// UnnamedServer is the virtual node for calls whose client span carries
	// no attribute naming the callee.
	UnnamedServer = "unknown"
)

type Config struct {
	PrometheusURL string
	Window        time.Duration
	RequestMetric string
	FailedMetric  string
	Headers       map[string]string
}

type Source struct {
	name   string
	cfg    Config
	client *http.Client
}

func New(name string, cfg Config) *Source {
	if cfg.Window == 0 {
		cfg.Window = DefaultWindow
	}
	if cfg.RequestMetric == "" {
		cfg.RequestMetric = DefaultRequestMetric
	}
	if cfg.FailedMetric == "" {
		cfg.FailedMetric = DefaultFailedMetric
	}
	return &Source{name: name, cfg: cfg, client: &http.Client{Timeout: 15 * time.Second}}
}

func (s *Source) Name() string { return s.name }
func (s *Source) Type() string { return "servicegraph" }

const labels = "client, server, connection_type, client_db_system, server_db_system"

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	var obs source.Observation
	window := promDuration(s.cfg.Window)

	requests, err := s.query(ctx, fmt.Sprintf("sum by (%s) (rate(%s[%s]))", labels, s.cfg.RequestMetric, window))
	if err != nil {
		var denied *deniedError
		if errors.As(err, &denied) {
			obs.Degraded = true
			obs.Message = denied.Error()
			obs.Findings = append(obs.Findings, s.deniedFinding(denied))
			return obs, nil
		}
		return obs, err
	}
	failed, err := s.query(ctx, fmt.Sprintf("sum by (%s) (rate(%s[%s]))", labels, s.cfg.FailedMetric, window))
	if err != nil {
		return obs, err
	}

	if len(requests) == 0 {
		exists, err := s.query(ctx, fmt.Sprintf("count(%s)", s.cfg.RequestMetric))
		if err != nil {
			return obs, err
		}
		obs.Degraded = true
		if len(exists) == 0 {
			obs.Message = "no service graph metrics in Prometheus"
			obs.Findings = append(obs.Findings, s.noMetricsFinding())
		} else {
			obs.Message = "service graph metrics exist but saw no traffic in the window"
		}
		return obs, nil
	}

	failedBy := map[string]float64{}
	for _, f := range failed {
		failedBy[f.metric["client"]+"->"+f.metric["server"]] += f.value
	}

	unnamed := map[string]float64{}
	for _, r := range requests {
		client, server := r.metric["client"], r.metric["server"]
		if client == "" || server == "" {
			continue
		}
		ctype := r.metric["connection_type"]
		virtualClient := client == VirtualClient
		virtualServer := ctype == "virtual_node" && !virtualClient
		if virtualServer && server == UnnamedServer {
			unnamed[client] += r.value
			continue
		}
		// db.system is set on the client span and describes the callee.
		dbSystem := r.metric["client_db_system"]
		if dbSystem == "" {
			dbSystem = r.metric["server_db_system"]
		}

		obs.Nodes = append(obs.Nodes,
			source.NodeObservation{
				ID:           client,
				Kind:         kindOf("", "", virtualClient),
				SelfReported: !virtualClient,
			},
			source.NodeObservation{
				ID:           server,
				Kind:         kindOf(dbSystem, ctype, false),
				SelfReported: !virtualServer,
			},
		)
		edge := source.EdgeObservation{
			From:        client,
			To:          server,
			Protocol:    protocolOf(dbSystem, ctype),
			RequestRate: r.value,
		}
		if r.value > 0 {
			edge.ErrorRate = failedBy[client+"->"+server] / r.value
		}
		obs.Edges = append(obs.Edges, edge)
	}
	for client, rate := range unnamed {
		obs.Findings = append(obs.Findings, unnamedFinding(client, rate))
	}
	return obs, nil
}

func unnamedFinding(client string, rate float64) model.Finding {
	return model.Finding{
		Rule:     "servicegraph-unnamed-target",
		Kind:     model.FindingBlindSpot,
		Severity: model.SeverityInfo,
		Subject:  client,
		Title:    fmt.Sprintf("%s calls something teia cannot name", client),
		Detail: fmt.Sprintf("%.2f req/s from %s end in a client span with no matching server span and no attribute naming the callee. "+
			"Either the callee has no telemetry, or its spans arrive after the collector gave up pairing them (servicegraph store.ttl).", rate, client),
		Fix: &model.Fix{
			Summary:  "Set peer.service (or server.address) on the client spans, and raise store.ttl if your SDKs batch spans for several seconds.",
			Language: "yaml",
			Snippet: `connectors:
  servicegraph:
    store:
      ttl: 10s`,
		},
	}
}

func kindOf(dbSystem, connectionType string, virtualClient bool) model.NodeKind {
	if virtualClient {
		return model.KindExternal
	}
	switch strings.ToLower(dbSystem) {
	case "":
	case "redis", "memcached", "valkey":
		return model.KindCache
	default:
		return model.KindDatabase
	}
	switch connectionType {
	case "database":
		return model.KindDatabase
	case "messaging_system":
		return model.KindQueue
	}
	return model.KindUnknown
}

func protocolOf(dbSystem, connectionType string) string {
	if dbSystem != "" {
		return strings.ToLower(dbSystem)
	}
	if connectionType == "messaging_system" {
		return "messaging"
	}
	return ""
}

type sample struct {
	metric map[string]string
	value  float64
}

type deniedError struct{ status int }

func (e *deniedError) Error() string {
	return fmt.Sprintf("Prometheus answered HTTP %d", e.status)
}

func (s *Source) query(ctx context.Context, promql string) ([]sample, error) {
	u := strings.TrimRight(s.cfg.PrometheusURL, "/") + "/api/v1/query?query=" + url.QueryEscape(promql)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range s.cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying Prometheus: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &deniedError{status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Prometheus answered HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var parsed struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]any            `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding Prometheus answer: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("Prometheus query failed: %s", parsed.Error)
	}
	out := make([]sample, 0, len(parsed.Data.Result))
	for _, r := range parsed.Data.Result {
		str, _ := r.Value[1].(string)
		v, err := strconv.ParseFloat(str, 64)
		if err != nil {
			continue
		}
		out = append(out, sample{metric: r.Metric, value: v})
	}
	return out, nil
}

func (s *Source) noMetricsFinding() model.Finding {
	return model.Finding{
		Rule:     "servicegraph-no-metrics",
		Kind:     model.FindingSource,
		Severity: model.SeverityWarning,
		Subject:  s.name,
		Title:    "Prometheus has no service graph metrics",
		Detail: fmt.Sprintf("teia reached %s but found no %s series. "+
			"Traces are not being turned into service graph metrics yet.", s.cfg.PrometheusURL, s.cfg.RequestMetric),
		Fix: &model.Fix{
			Summary:  "Enable the servicegraph connector in the OpenTelemetry Collector (or the service-graphs processor in the Tempo metrics-generator).",
			Language: "yaml",
			Snippet: `connectors:
  servicegraph:
    dimensions: [db.system]
    virtual_node_peer_attributes: [peer.service, db.name, db.system]
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [servicegraph]
    metrics/servicegraph:
      receivers: [servicegraph]
      exporters: [prometheus]`,
		},
	}
}

func (s *Source) deniedFinding(err *deniedError) model.Finding {
	return model.Finding{
		Rule:     "servicegraph-denied",
		Kind:     model.FindingPermission,
		Severity: model.SeverityCritical,
		Subject:  s.name,
		Title:    "Prometheus refused teia's queries",
		Detail:   fmt.Sprintf("%s at %s. teia only needs read access to the query API.", err, s.cfg.PrometheusURL),
		Fix: &model.Fix{
			Summary:  "Give teia a read-only credential and pass it as a header.",
			Language: "yaml",
			Snippet: `sources:
  servicegraph:
    prometheusURL: ` + s.cfg.PrometheusURL + `
    headers:
      Authorization: Bearer ${TEIA_PROMETHEUS_TOKEN}`,
		},
	}
}

func promDuration(d time.Duration) string {
	if d%time.Minute == 0 {
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(int(d/time.Second)) + "s"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
