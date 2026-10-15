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
	"math"
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
	// Probes to use; empty or ["auto"] detects them, ["none"] turns them off.
	Probes []string
}

type Source struct {
	name   string
	cfg    Config
	client *promapi.Client

	mu         sync.Mutex
	detected   []*Preset
	detectedAt time.Time
	probesSeen []*Probe
	probesAt   time.Time
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

// DetectProbes returns the probes whose metrics exist in this Prometheus.
func (s *Source) DetectProbes(ctx context.Context) ([]*Probe, error) {
	var out []*Probe
	for _, p := range Probes {
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

// probes returns the probes to run: the configured ones, none, or the ones
// detected (rechecked every detectEvery, like presets).
func (s *Source) probes(ctx context.Context) ([]*Probe, error) {
	only := func(name string) bool { return len(s.cfg.Probes) == 1 && s.cfg.Probes[0] == name }
	switch {
	case only("none"):
		return nil, nil
	case len(s.cfg.Probes) > 0 && !only("auto"):
		var out []*Probe
		for _, name := range s.cfg.Probes {
			p := LookupProbe(name)
			if p == nil {
				return nil, fmt.Errorf("unknown probe %q (known: %s)", name, strings.Join(ProbeNames(), ", "))
			}
			out = append(out, p)
		}
		return out, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probesSeen != nil && time.Since(s.probesAt) < detectEvery {
		return s.probesSeen, nil
	}
	found, err := s.DetectProbes(ctx)
	if err != nil {
		return nil, err
	}
	if len(found) > 0 {
		s.probesSeen, s.probesAt = found, time.Now()
	}
	return found, nil
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

// redactedURL is the configured URL without its password, for messages and
// findings.
func (s *Source) redactedURL() string { return promapi.RedactURL(s.cfg.URL) }

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	presets, err := s.presets(ctx)
	if err != nil {
		var denied *promapi.DeniedError
		if errors.As(err, &denied) {
			return s.denied(denied), nil
		}
		return source.Observation{}, fmt.Errorf("prometheus at %s: %w", s.redactedURL(), err)
	}
	obs := source.Observation{EvaluatedAt: time.Now().UTC()}
	var parts []string
	if len(presets) == 0 {
		obs.Degraded = true
		parts = append(parts, "no known communication metrics")
		obs.Findings = append(obs.Findings, s.nothingFoundFinding())
	}

	// Presets run independently: one family timing out or rejected must not
	// hide the edges the others found.
	w := window{promql: promapi.Duration(s.cfg.Window), seconds: s.cfg.Window.Seconds(), at: obs.EvaluatedAt}
	var used, empty, failedNames, failures, noLatency []string
	var firstErr error
	for _, p := range presets {
		n, latencyErr, err := s.runPreset(ctx, p, w, &obs)
		if err == nil && latencyErr != nil {
			noLatency = append(noLatency, p.Name)
		}
		switch {
		case err != nil:
			if firstErr == nil {
				firstErr = err
			}
			failedNames = append(failedNames, p.Name)
			failures = append(failures, fmt.Sprintf("%s: %v", p.Name, err))
		case n == 0:
			empty = append(empty, p.Name)
		default:
			used = append(used, fmt.Sprintf("%s (%d)", p.Name, n))
		}
	}
	if len(presets) > 0 && len(failedNames) == len(presets) {
		var denied *promapi.DeniedError
		if errors.As(firstErr, &denied) {
			return s.denied(denied), nil
		}
		return source.Observation{}, fmt.Errorf("prometheus at %s: every preset failed (%s); %s: %w",
			s.redactedURL(), strings.Join(failedNames, ", "), failedNames[0], firstErr)
	}

	if len(used) > 0 {
		parts = append(parts, "presets: "+strings.Join(used, ", "))
	}
	if len(empty) > 0 {
		parts = append(parts, "no traffic in window: "+strings.Join(empty, ", "))
	}
	if len(failedNames) > 0 {
		parts = append(parts, "failed: "+strings.Join(failedNames, ", "))
		obs.Findings = append(obs.Findings, s.presetsFailedFinding(failures))
	}
	if len(noLatency) > 0 {
		parts = append(parts, "latency query failed: "+strings.Join(noLatency, ", "))
	}
	obs.Degraded = obs.Degraded || len(used) == 0 || len(failedNames) > 0
	parts = append(parts, s.runProbes(ctx, w, &obs)...)
	obs.Message = strings.Join(parts, "; ")
	return obs, nil
}

// runProbes adds the readings of every probe in use to obs, and returns
// what to say about them in the source's message. A failing probe hides only
// its own readings.
func (s *Source) runProbes(ctx context.Context, w window, obs *source.Observation) []string {
	probes, err := s.probes(ctx)
	if err != nil {
		obs.Degraded = true
		return []string{"probe detection failed: " + promapi.RedactError(err).Error()}
	}
	var used, failed, timingFailed []string
	var timings []probeTiming
	for _, p := range probes {
		n, pending, err := s.runProbe(ctx, p, w, obs)
		timings = append(timings, pending...)
		switch {
		case err != nil:
			failed = append(failed, p.Name)
		case n > 0:
			used = append(used, fmt.Sprintf("%s (%d)", p.Name, n))
		}
	}
	// Fetch optional timing only after ALL probes' required measurements.
	// A timestamp query reaching the source deadline must not prevent another
	// reading from being collected or discard values already obtained.
	failedTiming := make(map[string]bool)
	for _, pending := range timings {
		times, err := s.sampleTimes(ctx, pending.queries, w.at)
		if err != nil && !failedTiming[pending.probe] {
			failedTiming[pending.probe] = true
			timingFailed = append(timingFailed, pending.probe+": "+promapi.RedactError(err).Error())
		}
		for i, key := range pending.keys {
			obs.Readings[pending.first+i].OldestSampleAt = times[key]
		}
	}
	var parts []string
	if len(used) > 0 {
		parts = append(parts, "probes: "+strings.Join(used, ", "))
	}
	if len(failed) > 0 {
		obs.Degraded = true
		parts = append(parts, "probe failed: "+strings.Join(failed, ", "))
	}
	if len(timingFailed) > 0 {
		obs.Degraded = true
		parts = append(parts, "sample timing unavailable: "+strings.Join(timingFailed, "; "))
	}
	return parts
}

// probeTiming maps optional timestamp queries back to the readings they date.
type probeTiming struct {
	probe   string
	first   int
	keys    []string
	queries []string
}

// runProbe adds readings only once every value/capacity query succeeds.
// Optional sample timing is deferred until all probes have their values.
func (s *Source) runProbe(ctx context.Context, p *Probe, w window, obs *source.Observation) (int, []probeTiming, error) {
	var readings []source.Reading
	var timings []probeTiming
	var missing []string
	for _, r := range p.Readings {
		what := r.What
		if strings.Contains(what, "%s") {
			what = fmt.Sprintf(what, w.promql)
		}
		values, err := s.client.QueryAt(ctx, r.Query(w), w.at)
		if err != nil {
			if r.Missing != "" {
				obs.Degraded = true
				missing = append(missing, what+": optional query failed: "+promapi.RedactError(err).Error())
				continue
			}
			return 0, nil, err
		}
		if len(values) == 0 && r.Missing != "" {
			missing = append(missing, what+": "+r.Missing)
		}
		var capacity map[string]float64
		if r.Max != nil {
			maxes, err := s.client.QueryAt(ctx, r.Max(w), w.at)
			if err != nil {
				return 0, nil, err
			}
			capacity = make(map[string]float64, len(maxes))
			for _, m := range maxes {
				capacity[seriesKey(m.Metric)] = m.Value
			}
		}
		first := len(readings)
		var keys []string
		for _, v := range values {
			m := model.Measure{Cause: r.Cause, What: what, Value: v.Value, Unit: r.Unit}
			switch {
			case r.Share:
				m.Max, m.Unit = 1, model.UnitShare
			case r.Max != nil:
				if m.Max = capacity[seriesKey(v.Metric)]; m.Max <= 0 {
					continue // no known capacity to compare with
				}
			}
			candidates, to := r.Subject(v.Metric)
			if len(candidates) == 0 {
				continue
			}
			rd := source.Reading{Candidates: candidates, To: to, Measures: []model.Measure{m}, Origin: origin(p, v.Metric)}
			if r.Windowed {
				rd.Window = s.cfg.Window
			}
			readings = append(readings, rd)
			keys = append(keys, seriesKey(v.Metric))
		}
		if len(keys) > 0 && len(r.SampleTimes) > 0 {
			timings = append(timings, probeTiming{probe: p.Name, first: len(obs.Readings) + first, keys: keys, queries: r.SampleTimes})
		}
	}
	obs.Readings = append(obs.Readings, readings...)
	if len(missing) > 0 {
		obs.Findings = append(obs.Findings, model.Finding{Rule: "probe-capability", Kind: model.FindingBlindSpot, Severity: model.SeverityInfo, Subject: s.name + "/" + p.Name,
			Title: "Some " + p.Name + " diagnostics are unavailable or not applicable", Detail: strings.Join(missing, "; "),
			Fix: &model.Fix{Summary: "Check exporter collectors, labels, privileges and lookback coverage. Missing metrics are not measured zero; see docs/resource-diagnostics.md."}})
	}
	return len(readings), timings, nil
}

// sampleTimes intersects the inputs for each reading: a recent value must
// not hide an older or undated capacity. The vector timestamp is evaluation
// time; only the timestamp() result VALUE establishes input sample age.
func (s *Source) sampleTimes(ctx context.Context, queries []string, at time.Time) (map[string]time.Time, error) {
	var oldest map[string]time.Time
	for i, query := range queries {
		samples, err := s.client.QueryAt(ctx, query, at)
		if err != nil {
			return nil, err
		}
		current := make(map[string]time.Time, len(samples))
		for _, sample := range samples {
			current[seriesKey(sample.Metric)] = sampleTime(sample.Value)
		}
		if i == 0 {
			oldest = current
			continue
		}
		for key, previous := range oldest {
			next := current[key]
			switch {
			case previous.IsZero() || next.IsZero():
				delete(oldest, key)
			case next.Before(previous):
				oldest[key] = next
			}
		}
	}
	return oldest, nil
}

func sampleTime(seconds float64) time.Time {
	// Stay within JSON's RFC3339 year range; invalid values remain unknown.
	if math.IsNaN(seconds) || seconds <= 0 || seconds >= 253402300800 {
		return time.Time{}
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(math.Round(fraction*1e9))).UTC()
}

// origin names where a reading came from, for findings about readings that
// fit no node: "redis (instance catalog-cache-exporter:9121)".
func origin(p *Probe, m map[string]string) string {
	for _, key := range []string{"consumergroup", "queue", "topic", "service", "instance", "pod", "job"} {
		if v := m[key]; v != "" {
			return fmt.Sprintf("%s (%s %s)", p.Name, key, v)
		}
	}
	return p.Name
}

func (s *Source) denied(err *promapi.DeniedError) source.Observation {
	return source.Observation{Degraded: true, Message: "access denied", Findings: []model.Finding{s.deniedFinding(err)}}
}

// runPreset adds one preset's edges to obs and returns how many it found.
// obs is only touched once the request and failure queries have succeeded;
// latency is optional, so its failure (latencyErr) keeps the edges.
func (s *Source) runPreset(ctx context.Context, p *Preset, w window, obs *source.Observation) (found int, latencyErr error, err error) {
	requests, err := s.client.QueryAt(ctx, p.Requests(w.promql), w.at)
	if err != nil {
		return 0, nil, err
	}
	failedBy := map[string]float64{}
	if p.Failures != nil {
		failed, err := s.client.QueryAt(ctx, p.Failures(w.promql), w.at)
		if err != nil {
			return 0, nil, err
		}
		for _, f := range failed {
			failedBy[seriesKey(f.Metric)] += f.Value
		}
	}
	rateBy := make(map[string]float64, len(requests))
	for _, r := range requests {
		rateBy[seriesKey(r.Metric)] += r.Value
	}
	var p50, p95 map[string]latencyEstimate
	if p.Latency != nil && !p.NoRate {
		if p50, latencyErr = s.latency(ctx, p, w, 0.5, rateBy); latencyErr == nil {
			p95, latencyErr = s.latency(ctx, p, w, 0.95, rateBy)
		}
		if latencyErr != nil {
			p50, p95 = nil, nil
		}
	}

	type acc struct {
		edge          Edge
		rate, failure float64
		busiest       float64 // rate of the series the protocol comes from
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
			a = &acc{edge: *e, busiest: r.Value}
			edges[key] = a
		}
		// Several series can make up one edge (HTTP and gRPC, two
		// destination services). Each request is in only one of them, since
		// the preset queries already drop the second reporter of a mesh
		// with "or", so the edge's rates are their sum. The protocol is the
		// one carrying most of the traffic.
		a.rate += r.Value
		a.failure += failedBy[seriesKey(r.Metric)]
		if r.Value > a.busiest {
			a.busiest = r.Value
			a.edge.Protocol = e.Protocol
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
		edge := source.EdgeObservation{From: e.From, To: e.To, Protocol: e.Protocol, Signal: p.Signal, Window: s.cfg.Window,
			LatencyP50Ms: p50[k].value, LatencyP95Ms: p95[k].value,
			LatencyApproximate: p50[k].approximate || p95[k].approximate}
		if !p.NoRate {
			edge.RequestRate = a.rate
			edge.NoErrors = p.Failures == nil // connections counted, not their failures
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
	return len(edges), latencyErr, nil
}

type latencyEstimate struct {
	value       float64
	approximate bool
}

// latency returns the q-quantile of call durations per edge key, in
// milliseconds. An edge made of several series (HTTP and gRPC, two
// destination services) gets their quantiles weighted by each series'
// request rate: an approximation, since quantiles do not add up.
func (s *Source) latency(ctx context.Context, p *Preset, w window, q float64, rateBy map[string]float64) (map[string]latencyEstimate, error) {
	series, err := s.client.QueryAt(ctx, p.Latency(w.promql, q), w.at)
	if err != nil {
		return nil, err
	}
	type weighted struct {
		sum, weight float64
		count       int
	}
	by := map[string]*weighted{}
	for _, r := range series {
		e := p.Map(r.Metric).Edge
		if e == nil {
			continue
		}
		key := e.From + "->" + e.To
		w, ok := by[key]
		if !ok {
			w = &weighted{}
			by[key] = w
		}
		// A series with no request rate of its own still counts, barely.
		weight := math.Max(rateBy[seriesKey(r.Metric)], 1e-9)
		w.sum += r.Value * weight
		w.weight += weight
		w.count++
	}
	out := make(map[string]latencyEstimate, len(by))
	for key, w := range by {
		out[key] = latencyEstimate{value: w.sum / w.weight * 1000, approximate: w.count > 1}
	}
	return out, nil
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
		Detail:   fmt.Sprintf("teia only needs read access to the query API of %s.", s.redactedURL()),
		Fix: &model.Fix{
			Summary:  "Give teia a read-only credential and pass it as a header (plus X-Scope-OrgID on multi-tenant Mimir/Cortex).",
			Language: "yaml",
			Snippet: `sources:
  prometheus:
    - url: ` + s.redactedURL() + `
      headers:
        Authorization: Bearer ${TEIA_PROMETHEUS_TOKEN}`,
		},
	}
}

// presetsFailedFinding explains the edges missing from a round where some
// presets failed and others worked.
func (s *Source) presetsFailedFinding(failures []string) model.Finding {
	return model.Finding{
		Rule:     "prometheus-preset-failed",
		Kind:     model.FindingSource,
		Severity: model.SeverityWarning,
		Subject:  s.name,
		Title:    "Some Prometheus queries failed, so the edges they cover are missing",
		Detail:   "The other presets worked. Failed:\n" + strings.Join(failures, "\n"),
		Fix: &model.Fix{
			Summary: "On large setups the usual cause is the query timeout or sample limit (--query.timeout and --query.max-samples on Prometheus, the query limits on Thanos or Mimir); a shorter window makes the queries cheaper.",
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
