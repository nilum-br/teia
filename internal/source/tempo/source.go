// Package tempo discovers communication straight from traces stored in
// Grafana Tempo, for platforms that keep traces but do not generate service
// graph metrics.
//
// It samples recent traces per service and pairs spans the way the service
// graph processors do: a server or consumer span whose parent belongs to
// another service is an edge from that service. Client spans that never
// reach an instrumented server become edges to the peer they name
// (peer.service, db.system, server.address). Being a sample, it sees which
// edges exist but not their rates.
package tempo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

type Config struct {
	URL     string
	Headers map[string]string
	Window  time.Duration
	// TracesPerService bounds the sample for each service.
	TracesPerService int
	// MaxTraces bounds the whole round.
	MaxTraces int
}

type Source struct {
	name   string
	cfg    Config
	client *http.Client
}

func New(name string, cfg Config) *Source {
	if cfg.Window == 0 {
		cfg.Window = 15 * time.Minute
	}
	if cfg.TracesPerService == 0 {
		cfg.TracesPerService = 20
	}
	if cfg.MaxTraces == 0 {
		cfg.MaxTraces = 400
	}
	return &Source{name: name, cfg: cfg, client: &http.Client{Timeout: 15 * time.Second}}
}

func (s *Source) Name() string { return s.name }
func (s *Source) Type() string { return "tempo" }

type deniedError struct{ status int }

func (e *deniedError) Error() string { return fmt.Sprintf("HTTP %d", e.status) }

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	var obs source.Observation
	services, err := s.services(ctx)
	if err != nil {
		var denied *deniedError
		if errors.As(err, &denied) {
			obs.Degraded = true
			obs.Message = "access denied"
			obs.Findings = append(obs.Findings, model.Finding{
				Rule: "tempo-denied", Kind: model.FindingPermission, Severity: model.SeverityCritical,
				Subject: s.name, Title: fmt.Sprintf("Tempo refused teia (%s)", denied),
				Fix: &model.Fix{Summary: "Give teia read access to Tempo's query API and pass the credential as a header (and X-Scope-OrgID for multi-tenant Tempo).",
					Language: "yaml", Snippet: "headers:\n  Authorization: Bearer ${TEIA_TEMPO_TOKEN}\n  X-Scope-OrgID: your-tenant"},
			})
			return obs, nil
		}
		return obs, err
	}
	if len(services) == 0 {
		obs.Degraded = true
		obs.Message = "no services with traces in the window"
		return obs, nil
	}

	ids, err := s.traceIDs(ctx, services)
	if err != nil {
		return obs, err
	}
	traces := s.fetch(ctx, ids)
	edges := map[string]*source.EdgeObservation{}
	self := map[string]bool{}
	targets := map[string]model.NodeKind{}
	for _, t := range traces {
		pairTrace(t, edges, self, targets)
	}
	for id := range self {
		obs.Nodes = append(obs.Nodes, source.NodeObservation{ID: id, Kind: model.KindService, SelfReported: true,
			Labels: map[string]string{source.SignalLabel(source.SignalTraces): "true"}})
	}
	for id, kind := range targets {
		if !self[id] {
			obs.Nodes = append(obs.Nodes, source.NodeObservation{ID: id, Kind: kind})
		}
	}
	keys := make([]string, 0, len(edges))
	for k := range edges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		obs.Edges = append(obs.Edges, *edges[k])
	}
	obs.Message = fmt.Sprintf("sampled %d traces from %d services", len(traces), len(services))
	return obs, nil
}

// span is the part of an OTLP span teia needs.
type span struct {
	id, parent, service, kind string
	attrs                     map[string]string
}

func pairTrace(spans []span, edges map[string]*source.EdgeObservation, self map[string]bool, targets map[string]model.NodeKind) {
	byID := map[string]span{}
	hasRemoteChild := map[string]bool{}
	for _, sp := range spans {
		byID[sp.id] = sp
		if sp.service != "" {
			self[sp.service] = true
		}
	}
	add := func(from, to, protocol string) {
		if from == "" || to == "" || from == to {
			return
		}
		key := from + "->" + to
		if e, ok := edges[key]; ok {
			if e.Protocol == "" {
				e.Protocol = protocol
			}
			return
		}
		edges[key] = &source.EdgeObservation{From: from, To: to, Protocol: protocol, Signal: source.SignalTraces}
	}
	for _, sp := range spans {
		if sp.kind != "server" && sp.kind != "consumer" {
			continue
		}
		parent, ok := byID[sp.parent]
		if !ok || parent.service == sp.service {
			continue
		}
		hasRemoteChild[parent.id] = true
		add(parent.service, sp.service, protocolOf(parent.attrs, sp.attrs))
	}
	for _, sp := range spans {
		if (sp.kind != "client" && sp.kind != "producer") || hasRemoteChild[sp.id] {
			continue
		}
		target, kind := peerOf(sp.attrs)
		if target == "" {
			continue
		}
		if _, known := targets[target]; !known || kind != model.KindUnknown {
			targets[target] = kind
		}
		add(sp.service, target, protocolOf(sp.attrs, nil))
	}
}

func peerOf(a map[string]string) (string, model.NodeKind) {
	system := first(a, "db.system.name", "db.system")
	kind := model.KindUnknown
	switch strings.ToLower(system) {
	case "":
	case "redis", "memcached", "valkey":
		kind = model.KindCache
	default:
		kind = model.KindDatabase
	}
	if ms := first(a, "messaging.system"); ms != "" {
		kind = model.KindQueue
	}
	if p := first(a, "peer.service"); p != "" {
		return p, kind
	}
	if host := first(a, "server.address", "net.peer.name", "db.namespace", "db.name"); host != "" {
		return host, kind
	}
	return system, kind
}

func protocolOf(client, server map[string]string) string {
	for _, a := range []map[string]string{client, server} {
		if v := first(a, "db.system.name", "db.system", "messaging.system", "rpc.system"); v != "" {
			return strings.ToLower(v)
		}
		if first(a, "http.request.method", "http.method") != "" {
			return "http"
		}
	}
	return ""
}

func first(a map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := a[k]; v != "" {
			return v
		}
	}
	return ""
}

// --- Tempo HTTP API ----------------------------------------------------------

func (s *Source) get(ctx context.Context, path string, params url.Values, out any) error {
	u := strings.TrimRight(s.cfg.URL, "/") + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range s.cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &deniedError{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tempo %s: HTTP %d: %.200s", path, resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

func (s *Source) window() url.Values {
	end := time.Now()
	return url.Values{
		"start": {strconv.FormatInt(end.Add(-s.cfg.Window).Unix(), 10)},
		"end":   {strconv.FormatInt(end.Unix(), 10)},
	}
}

func (s *Source) services(ctx context.Context) ([]string, error) {
	var resp struct {
		TagValues []struct {
			Value string `json:"value"`
		} `json:"tagValues"`
	}
	if err := s.get(ctx, "/api/v2/search/tag/resource.service.name/values", s.window(), &resp); err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}
	out := make([]string, 0, len(resp.TagValues))
	for _, v := range resp.TagValues {
		if v.Value != "" {
			out = append(out, v.Value)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *Source) traceIDs(ctx context.Context, services []string) ([]string, error) {
	seen := map[string]bool{}
	var ids []string
	for _, svc := range services {
		params := s.window()
		params.Set("q", fmt.Sprintf(`{ resource.service.name = %q }`, svc))
		params.Set("limit", strconv.Itoa(s.cfg.TracesPerService))
		var resp struct {
			Traces []struct {
				TraceID string `json:"traceID"`
			} `json:"traces"`
		}
		if err := s.get(ctx, "/api/search", params, &resp); err != nil {
			return nil, fmt.Errorf("searching traces of %s: %w", svc, err)
		}
		for _, t := range resp.Traces {
			if !seen[t.TraceID] && len(ids) < s.cfg.MaxTraces {
				seen[t.TraceID] = true
				ids = append(ids, t.TraceID)
			}
		}
	}
	return ids, nil
}

func (s *Source) fetch(ctx context.Context, ids []string) [][]span {
	out := make([][]span, len(ids))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			var raw otlpTrace
			if err := s.get(ctx, "/api/v2/traces/"+url.PathEscape(id), nil, &raw); err != nil {
				return
			}
			out[i] = raw.spans()
		}()
	}
	wg.Wait()
	return out
}

// otlpTrace accepts both Tempo trace formats: v2 {"trace":{"resourceSpans"}}
// and v1 {"batches":[...]}.
type otlpTrace struct {
	Trace struct {
		ResourceSpans []resourceSpans `json:"resourceSpans"`
	} `json:"trace"`
	Batches []resourceSpans `json:"batches"`
}

type resourceSpans struct {
	Resource struct {
		Attributes []attribute `json:"attributes"`
	} `json:"resource"`
	ScopeSpans []struct {
		Spans []otlpSpan `json:"spans"`
	} `json:"scopeSpans"`
	InstrumentationLibrarySpans []struct {
		Spans []otlpSpan `json:"spans"`
	} `json:"instrumentationLibrarySpans"`
}

type otlpSpan struct {
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId"`
	Kind         json.RawMessage `json:"kind"`
	Attributes   []attribute     `json:"attributes"`
}

type attribute struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string          `json:"stringValue"`
		IntValue    json.RawMessage `json:"intValue"`
	} `json:"value"`
}

func attrs(list []attribute) map[string]string {
	m := make(map[string]string, len(list))
	for _, a := range list {
		v := a.Value.StringValue
		if v == "" && len(a.Value.IntValue) > 0 {
			v = strings.Trim(string(a.Value.IntValue), `"`)
		}
		m[a.Key] = v
	}
	return m
}

// spanKind normalises "SPAN_KIND_SERVER", 2 and "server" to "server".
func spanKind(raw json.RawMessage) string {
	s := strings.ToLower(strings.Trim(string(raw), `"`))
	switch s {
	case "1", "span_kind_internal":
		return "internal"
	case "2", "span_kind_server":
		return "server"
	case "3", "span_kind_client":
		return "client"
	case "4", "span_kind_producer":
		return "producer"
	case "5", "span_kind_consumer":
		return "consumer"
	}
	return strings.TrimPrefix(s, "span_kind_")
}

func (t otlpTrace) spans() []span {
	batches := t.Trace.ResourceSpans
	if len(batches) == 0 {
		batches = t.Batches
	}
	var out []span
	for _, b := range batches {
		service := attrs(b.Resource.Attributes)["service.name"]
		groups := b.ScopeSpans
		for _, g := range b.InstrumentationLibrarySpans {
			groups = append(groups, struct {
				Spans []otlpSpan `json:"spans"`
			}{g.Spans})
		}
		for _, g := range groups {
			for _, sp := range g.Spans {
				out = append(out, span{
					id: sp.SpanID, parent: sp.ParentSpanID, service: service,
					kind: spanKind(sp.Kind), attrs: attrs(sp.Attributes),
				})
			}
		}
	}
	return out
}
