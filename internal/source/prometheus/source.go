// Package prometheus discovers communication from metrics a platform already
// collects. Each preset knows one family of metrics (service graph metrics
// from OpenTelemetry or Tempo, Istio, Linkerd, Beyla, Hubble...). In auto
// mode teia asks Prometheus which families exist and uses all of them, so the
// same config works on a mesh, on plain OpenTelemetry, or on a mix of both.
package prometheus

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
	"github.com/nilum-br/teia/internal/source/promapi"
)

type Config struct {
	URL     string
	Headers map[string]string
	Window  time.Duration
	// Presets to use; empty or ["auto"] detects them.
	Presets []string
}

type Source struct {
	name   string
	cfg    Config
	client *promapi.Client

	mu         sync.Mutex
	detected   []*Preset
	detectedAt time.Time
}

// detectEvery bounds how often auto mode re-checks which metrics exist. An
// empty answer is never cached: a fresh Prometheus fills up in seconds.
const detectEvery = 5 * time.Minute

func New(name string, cfg Config) *Source {
	if cfg.Window == 0 {
		cfg.Window = 5 * time.Minute
	}
	return &Source{name: name, cfg: cfg, client: promapi.New(cfg.URL, "/api/v1/query", cfg.Headers)}
}

func (s *Source) Name() string { return s.name }
func (s *Source) Type() string { return "prometheus" }

func (s *Source) auto() bool {
	return len(s.cfg.Presets) == 0 || (len(s.cfg.Presets) == 1 && s.cfg.Presets[0] == "auto")
}

// Detect returns the presets whose metrics exist in this Prometheus.
func (s *Source) Detect(ctx context.Context) ([]*Preset, error) {
	var out []*Preset
	for _, p := range Presets {
		samples, err := s.client.Query(ctx, fmt.Sprintf("count(%s)", p.Detect))
		if err != nil {
			return nil, err
		}
		if len(samples) > 0 && samples[0].Value > 0 {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Source) presets(ctx context.Context) ([]*Preset, error) {
	if !s.auto() {
		var out []*Preset
		for _, name := range s.cfg.Presets {
			p := Lookup(name)
			if p == nil {
				return nil, fmt.Errorf("unknown preset %q (known: %s)", name, strings.Join(Names(), ", "))
			}
			out = append(out, p)
		}
		return out, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.detected != nil && time.Since(s.detectedAt) < detectEvery {
		return s.detected, nil
	}
	found, err := s.Detect(ctx)
	if err != nil {
		return nil, err
	}
	if len(found) > 0 {
		s.detected, s.detectedAt = found, time.Now()
	}
	return found, nil
}

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	var obs source.Observation
	presets, err := s.presets(ctx)
	if err != nil {
		var denied *promapi.DeniedError
		if errors.As(err, &denied) {
			obs.Degraded = true
			obs.Message = "access denied"
			obs.Findings = append(obs.Findings, s.deniedFinding(denied))
			return obs, nil
		}
		return obs, fmt.Errorf("prometheus at %s: %w", s.cfg.URL, err)
	}
	if len(presets) == 0 {
		obs.Degraded = true
		obs.Message = "no known communication metrics"
		obs.Findings = append(obs.Findings, s.nothingFoundFinding())
		return obs, nil
	}

	window := promapi.Duration(s.cfg.Window)
	var used, empty []string
	for _, p := range presets {
		n, err := s.runPreset(ctx, p, window, &obs)
		if err != nil {
			return obs, fmt.Errorf("preset %s: %w", p.Name, err)
		}
		if n == 0 {
			empty = append(empty, p.Name)
		} else {
			used = append(used, fmt.Sprintf("%s (%d)", p.Name, n))
		}
	}
	obs.Message = "presets: " + strings.Join(used, ", ")
	if len(empty) > 0 {
		obs.Message += "; no traffic in window: " + strings.Join(empty, ", ")
		if len(used) == 0 {
			obs.Degraded = true
		}
	}
	return obs, nil
}

// runPreset adds one preset's edges to obs and returns how many it found.
func (s *Source) runPreset(ctx context.Context, p *Preset, window string, obs *source.Observation) (int, error) {
	requests, err := s.client.Query(ctx, p.Requests(window))
	if err != nil {
		return 0, err
	}
	failedBy := map[string]float64{}
	if p.Failures != nil {
		failed, err := s.client.Query(ctx, p.Failures(window))
		if err != nil {
			return 0, err
		}
		for _, f := range failed {
			failedBy[seriesKey(f.Metric)] += f.Value
		}
	}

	type acc struct {
		edge          Edge
		rate, failure float64
	}
	edges := map[string]*acc{}
	unnamed := map[string]float64{}
	unknown := map[string]float64{}
	for _, r := range requests {
		res := p.Map(r.Metric)
		switch {
		case res.UnknownCallerOf != "":
			unknown[res.UnknownCallerOf] += r.Value
			continue
		case res.UnnamedTargetOf != "":
			unnamed[res.UnnamedTargetOf] += r.Value
			continue
		case res.Edge == nil:
			continue
		}
		e := res.Edge
		key := e.From + "->" + e.To
		a, ok := edges[key]
		if !ok {
			a = &acc{edge: *e}
			edges[key] = a
		}
		// Several series can describe one edge (both proxies of a mesh
		// report it): keep the largest view, not the sum.
		if r.Value > a.rate {
			a.rate = r.Value
			a.failure = failedBy[seriesKey(r.Metric)]
		}
		a.edge.FromSelf = a.edge.FromSelf || e.FromSelf
		a.edge.ToSelf = a.edge.ToSelf || e.ToSelf
	}

	keys := make([]string, 0, len(edges))
	for k := range edges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := edges[k]
		e := a.edge
		obs.Nodes = append(obs.Nodes,
			source.NodeObservation{ID: e.From, Kind: kindOr(e.FromKind), SelfReported: e.FromSelf, Labels: nodeLabels(p, e.FromSelf, e.FromNS)},
			source.NodeObservation{ID: e.To, Kind: kindOr(e.ToKind), SelfReported: e.ToSelf, Labels: nodeLabels(p, e.ToSelf, e.ToNS)},
		)
		edge := source.EdgeObservation{From: e.From, To: e.To, Protocol: e.Protocol, Signal: p.Signal}
		if !p.NoRate {
			edge.RequestRate = a.rate
			if a.rate > 0 {
				edge.ErrorRate = a.failure / a.rate
			}
		}
		obs.Edges = append(obs.Edges, edge)
	}
	for client, rate := range unnamed {
		obs.Findings = append(obs.Findings, unnamedFinding(client, rate))
	}
	callees := make([]string, 0, len(unknown))
	for c := range unknown {
		callees = append(callees, c)
	}
	sort.Strings(callees)
	for _, c := range callees {
		obs.UnknownCallers = append(obs.UnknownCallers, source.UnknownCaller{Callee: c, Signal: p.Signal, Rate: unknown[c]})
		// The callee reported the traffic itself, so its signal saw it.
		obs.Nodes = append(obs.Nodes, source.NodeObservation{ID: c, Kind: model.KindUnknown, SelfReported: true, Labels: nodeLabels(p, true, "")})
	}
	return len(edges), nil
}

// nodeLabels records which signal saw a node first-hand and, when the
// metrics carry it, its Kubernetes namespace.
func nodeLabels(p *Preset, self bool, namespace string) map[string]string {
	labels := map[string]string{}
	if self && p.Signal != "" {
		labels[source.SignalLabel(p.Signal)] = "true"
	}
	if namespace != "" && namespace != "unknown" {
		labels["k8s.namespace"] = namespace
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}

// seriesKey identifies a series by its labels, so request and failure
// queries grouped the same way can be joined.
func seriesKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k != "__name__" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + m[k] + ",")
	}
	return b.String()
}

func kindOr(k model.NodeKind) model.NodeKind {
	if k == "" {
		return model.KindUnknown
	}
	return k
}

func (s *Source) deniedFinding(err *promapi.DeniedError) model.Finding {
	return model.Finding{
		Rule:     "prometheus-denied",
		Kind:     model.FindingPermission,
		Severity: model.SeverityCritical,
		Subject:  s.name,
		Title:    fmt.Sprintf("Prometheus refused teia's queries (%s)", err),
		Detail:   fmt.Sprintf("teia only needs read access to the query API of %s.", s.cfg.URL),
		Fix: &model.Fix{
			Summary:  "Give teia a read-only credential and pass it as a header (plus X-Scope-OrgID on multi-tenant Mimir/Cortex).",
			Language: "yaml",
			Snippet: `sources:
  prometheus:
    - url: ` + s.cfg.URL + `
      headers:
        Authorization: Bearer ${TEIA_PROMETHEUS_TOKEN}`,
		},
	}
}

func (s *Source) nothingFoundFinding() model.Finding {
	var looked []string
	for _, p := range Presets {
		looked = append(looked, fmt.Sprintf("%s (%s)", p.Detect, p.Name))
	}
	return model.Finding{
		Rule:     "prometheus-no-signals",
		Kind:     model.FindingSource,
		Severity: model.SeverityWarning,
		Subject:  s.name,
		Title:    "Prometheus has none of the communication metrics teia knows",
		Detail:   "teia looked for: " + strings.Join(looked, ", ") + ".",
		Fix: &model.Fix{
			Summary:  "The cheapest signal is usually the one you already have traces for: turn traces into service graph metrics with the OpenTelemetry Collector servicegraph connector (or Tempo's metrics-generator). Without traces, Grafana Beyla adds them with eBPF and no code changes. A mesh is not required.",
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
