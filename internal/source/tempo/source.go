// Package tempo discovers communication straight from traces stored in
// Grafana Tempo, for platforms that keep traces but do not generate service
// graph metrics.
//
// It samples recent traces per service and pairs spans the way the service
// graph processors do: a server or consumer span whose parent belongs to
// another service is an edge from that service. Client spans that never
// reach an instrumented server become edges to the peer they name
// (peer.service, server.address, db.system). Being a sample, it sees which
// edges exist but not their rates.
package tempo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
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
	// TracesPerService bounds the sample for each service.
	TracesPerService int
	// MaxTraces bounds the whole round.
	MaxTraces int
	// Dimensions maps a business dimension (journey, tenant...) to the span
	// attribute holding it. A span without the attribute inherits it from
	// its closest ancestor in the same trace that has it, so a call belongs
	// to the journey of the request that caused it, not of its service.
	Dimensions map[string]string
	// Selector maps span attributes (resource.k8s.cluster.name) to the
	// value every sampled trace must carry: one cluster's traces on a Tempo
	// that keeps several.
	Selector map[string]string
}

type Source struct {
	name   string
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu sync.Mutex
	// next is where the next round starts in the sorted service list: when
	// MaxTraces cannot cover every service, rounds take turns instead of
	// always sampling the same alphabetical head.
	next int
}

// searchConcurrency bounds the searches a round runs at once: enough to
// cover many services in one round, few enough not to flood the query queue
// of a Tempo that other clusters' rounds and people's queries share.
const searchConcurrency = 4

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
	return &Source{name: name, cfg: cfg, client: &http.Client{Timeout: 15 * time.Second}, now: time.Now}
}

func (s *Source) Name() string { return s.name }
func (s *Source) Type() string { return "tempo" }

// conditions renders the selector as TraceQL conditions, in a stable order,
// each joined by "&&"; empty without a selector.
func (s *Source) conditions() string {
	attrs := make([]string, 0, len(s.cfg.Selector))
	for attr := range s.cfg.Selector {
		attrs = append(attrs, attr)
	}
	sort.Strings(attrs)
	parts := make([]string, len(attrs))
	for i, attr := range attrs {
		parts[i] = fmt.Sprintf("%s = %q", attr, s.cfg.Selector[attr])
	}
	return strings.Join(parts, " && ")
}

type deniedError struct{ status int }

func (e *deniedError) Error() string { return fmt.Sprintf("HTTP %d", e.status) }

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	var obs source.Observation
	// Every round says which dimensions it can split by, even one without
	// traces, so a slice does not treat this source as unable to split.
	for dim := range s.cfg.Dimensions {
		obs.Dimensions = append(obs.Dimensions, dim)
	}
	sort.Strings(obs.Dimensions)
	// Searching each service can take seconds. Use one window for the whole
	// round rather than silently moving the period as each request starts.
	window := s.window()
	services, err := s.services(ctx, window)
	if err != nil {
		var denied *deniedError
		if errors.As(err, &denied) {
			return s.denied(denied), nil
		}
		return obs, err
	}
	if len(services) == 0 {
		obs.Operations = operationSamples(s.name, window, nil, s.cfg.Dimensions, 0, 0, 0, 0, false)
		obs.Degraded = true
		obs.Message = "no services with traces in the window"
		return obs, nil
	}

	s.mu.Lock()
	start := s.next % len(services)
	s.mu.Unlock()
	found := s.traceIDs(ctx, services, start, window, searchUntil(ctx))
	// Carry on from the first service this round did not reach, even when
	// searches failed: retrying the same ones first every round would leave
	// the services after them unsampled for good.
	s.mu.Lock()
	s.next = (start + found.searched) % len(services)
	s.mu.Unlock()
	if found.searched > 0 && found.failed == found.searched {
		var denied *deniedError
		if errors.As(found.firstErr, &denied) {
			return s.denied(denied), nil
		}
		return obs, fmt.Errorf("every search failed (%d), first %w", found.failed, found.firstErr)
	}
	ids, searched := found.ids, found.searched

	traces, failed, fetchErr := s.fetch(ctx, ids, fetchWindow(window, s.cfg.Window))
	if failed > 0 && failed == len(ids) {
		var denied *deniedError
		if errors.As(fetchErr, &denied) {
			return s.denied(denied), nil
		}
		return obs, fmt.Errorf("fetching traces: all %d failed, first %w", failed, fetchErr)
	}
	p := newPairing(s.cfg.Dimensions)
	for _, t := range traces {
		p.pair(t.spans)
	}
	obs.Operations = operationSamples(s.name, window, traces, s.cfg.Dimensions, len(ids), failed, searched, len(services), found.limited || searched < len(services))
	edges, self, targets := p.edges, p.self, p.targets
	for key, ms := range p.durations {
		edges[key].LatencyP50Ms, edges[key].LatencyP95Ms = quantile(ms, 0.5), quantile(ms, 0.95)
	}
	for _, key := range p.sliceOrder {
		sl := p.slices[key]
		if ms := sl.durations; len(ms) > 0 {
			sl.slice.LatencyP50Ms, sl.slice.LatencyP95Ms = quantile(ms, 0.5), quantile(ms, 0.95)
		}
		e := edges[sl.edge]
		e.Slices = append(e.Slices, sl.slice)
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
		edges[k].Window = s.cfg.Window
		obs.Edges = append(obs.Edges, *edges[k])
	}
	obs.Message = fmt.Sprintf("sampled %d traces from %d services", len(traces), searched)
	if searched < len(services) {
		obs.Message = fmt.Sprintf("sampled %d traces from %d of %d services (the others are next)", len(traces), searched, len(services))
	}
	if found.outOfTime {
		obs.Degraded = true
		obs.Message += "; out of time for more searches in this round"
	}
	if found.failed > 0 {
		obs.Degraded = true
		obs.Message += fmt.Sprintf("; %d of %d searches failed, first %v", found.failed, searched, found.firstErr)
	}
	if failed > 0 {
		obs.Degraded = true
		obs.Message += fmt.Sprintf("; %d of %d traces could not be fetched, first %v", failed, len(ids), fetchErr)
	}
	return obs, nil
}

func (s *Source) denied(err *deniedError) source.Observation {
	return source.Observation{Degraded: true, Message: "access denied", Findings: []model.Finding{{
		Rule: "tempo-denied", Kind: model.FindingPermission, Severity: model.SeverityCritical,
		Subject: s.name, Title: fmt.Sprintf("Tempo refused teia (%s)", err),
		Detail: fmt.Sprintf("teia only needs read access to the query API of %s.", promapi.RedactURL(s.cfg.URL)),
		Fix: &model.Fix{Summary: "Give teia read access to Tempo's query API and pass the credential as a header (and X-Scope-OrgID for multi-tenant Tempo).",
			Language: "yaml", Snippet: "headers:\n  Authorization: Bearer ${TEIA_TEMPO_TOKEN}\n  X-Scope-OrgID: your-tenant"},
	}}}
}

// span is the part of an OTLP span teia needs.
type span struct {
	id, parent, service, kind string
	name, status              string
	links                     []model.OperationSpanLink
	attrs                     map[string]string
	dur                       time.Duration // zero when the span has no valid times
	startedAt, endedAt        time.Time
}

// pairing accumulates the edges of the sampled traces, the duration of each
// call (in milliseconds) under its edge's key, and, with dimensions, the same
// per combination of dimension values.
type pairing struct {
	dims       map[string]string
	edges      map[string]*source.EdgeObservation
	self       map[string]bool
	targets    map[string]model.NodeKind
	durations  map[string][]float64
	slices     map[string]*traceSlice
	sliceOrder []string
}

type traceSlice struct {
	edge      string
	slice     source.Slice
	durations []float64
}

func newPairing(dims map[string]string) *pairing {
	return &pairing{dims: dims, edges: map[string]*source.EdgeObservation{}, self: map[string]bool{},
		targets: map[string]model.NodeKind{}, durations: map[string][]float64{}, slices: map[string]*traceSlice{}}
}

// envelope widens a sample's time bounds with one call. Both bounds become
// unknown if any call has no valid times.
func envelope(count int, from, at *time.Time, call span) {
	switch {
	case count == 1:
		*from, *at = call.startedAt, call.endedAt
	case call.startedAt.IsZero() || call.endedAt.IsZero() || from.IsZero() || at.IsZero():
		// An undated call might be outside the known envelope. Do not
		// present a partial range as the age of the entire sample.
		*from, *at = time.Time{}, time.Time{}
	default:
		if call.startedAt.Before(*from) {
			*from = call.startedAt
		}
		if call.endedAt.After(*at) {
			*at = call.endedAt
		}
	}
}

// dimsOf reads the configured dimensions of a span, each from the span itself
// or else from its closest ancestor holding it.
func (p *pairing) dimsOf(sp span, byID map[string]span) map[string]string {
	if len(p.dims) == 0 {
		return nil
	}
	var out map[string]string
	for dim, attr := range p.dims {
		at, seen := sp, map[string]bool{}
		for depth := 0; depth < 256 && !seen[at.id]; depth++ {
			seen[at.id] = true
			if v := strings.TrimSpace(at.attrs[attr]); v != "" {
				if out == nil {
					out = map[string]string{}
				}
				out[dim] = v
				break
			}
			parent, ok := byID[at.parent]
			if !ok {
				break
			}
			at = parent
		}
	}
	return out
}

// pairTrace adds the edges one trace shows, and the duration of each call
// (in milliseconds) to durations under the edge's key, without dimensions.
func pairTrace(spans []span, edges map[string]*source.EdgeObservation, self map[string]bool, targets map[string]model.NodeKind, durations map[string][]float64) {
	p := &pairing{edges: edges, self: self, targets: targets, durations: durations, slices: map[string]*traceSlice{}}
	p.pair(spans)
}

// pair adds the edges one trace shows.
func (p *pairing) pair(spans []span) {
	edges, self, targets, durations := p.edges, p.self, p.targets, p.durations
	byID := map[string]span{}
	hasRemoteChild := map[string]bool{}
	for _, sp := range spans {
		byID[sp.id] = sp
		if sp.service != "" {
			self[sp.service] = true
		}
	}
	add := func(from, to, protocol string, call span, dims map[string]string) {
		if from == "" || to == "" || from == to {
			return
		}
		key := from + "->" + to
		// A synchronous call's span lasts as long as the caller waits; a
		// message is not waited for, so producer and consumer spans say
		// nothing about latency.
		timed := call.dur > 0 && (call.kind == "client" || call.kind == "server")
		if timed {
			durations[key] = append(durations[key], float64(call.dur)/float64(time.Millisecond))
		}
		e := edges[key]
		if e == nil {
			e = &source.EdgeObservation{From: from, To: to, Signal: source.SignalTraces}
			edges[key] = e
		}
		if e.Protocol == "" {
			e.Protocol = protocol
		}
		e.SampleCount++
		envelope(e.SampleCount, &e.ObservedFrom, &e.ObservedAt, call)
		if len(dims) == 0 {
			return
		}
		sk := key + "\x00" + dimsKey(dims)
		sl := p.slices[sk]
		if sl == nil {
			// Spans count calls in a sample, not their rate or failures.
			sl = &traceSlice{edge: key, slice: source.Slice{Dims: dims, NoErrors: true}}
			p.slices[sk] = sl
			p.sliceOrder = append(p.sliceOrder, sk)
		}
		sl.slice.SampleCount++
		envelope(sl.slice.SampleCount, &sl.slice.ObservedFrom, &sl.slice.ObservedAt, call)
		if timed {
			sl.durations = append(sl.durations, float64(call.dur)/float64(time.Millisecond))
		}
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
		// The caller's client span includes the network; fall back to the
		// server span when the caller only has an internal one.
		call := parent
		if call.kind != "client" {
			call = sp
		}
		dims := p.dimsOf(sp, byID)
		add(parent.service, sp.service, protocolOf(parent.attrs, sp.attrs), call, dims)
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
		add(sp.service, target, protocolOf(sp.attrs, nil), sp, p.dimsOf(sp, byID))
	}
}

// dimsKey identifies a combination of dimension values.
func dimsKey(dims map[string]string) string {
	keys := make([]string, 0, len(dims))
	for k := range dims {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + dims[k] + "\x00")
	}
	return b.String()
}

// quantile returns the q-quantile of a sample (nearest rank). With the few
// traces a round samples, it is a hint rather than a measurement; metrics
// from histograms take precedence when both exist.
func quantile(sample []float64, q float64) float64 {
	if len(sample) == 0 {
		return 0
	}
	sorted := slices.Clone(sample)
	slices.Sort(sorted)
	i := int(math.Ceil(q*float64(len(sorted)))) - 1
	return sorted[max(0, min(i, len(sorted)-1))]
}

// peerOf names the callee of a client span. db.name and db.namespace are
// not used: they name a database inside a server, and a database called
// "orders" would merge with the orders service.
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
	if host := first(a, "server.address", "net.peer.name"); host != "" {
		return hostID(host), kind
	}
	return system, kind
}

// hostID turns an address attribute into a node ID. An IP becomes an
// address that inventory sources resolve to a workload; a cluster DNS name
// is shortened the way the Prometheus presets do it
// ("payments.shop.svc.cluster.local" is "payments"); other hosts such as
// "api.stripe.com" are kept whole.
func hostID(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if net.ParseIP(host) != nil {
		return source.AddressID(host)
	}
	if i := strings.Index(host+".", ".svc."); i > 0 {
		host = host[:i]
		if j := strings.Index(host, "."); j > 0 {
			host = host[:j]
		}
	}
	return host
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
		return promapi.RedactError(err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range s.cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return promapi.RedactError(err)
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
	end := s.now()
	return url.Values{
		"start": {strconv.FormatInt(end.Add(-s.cfg.Window).Unix(), 10)},
		"end":   {strconv.FormatInt(end.Unix(), 10)},
	}
}

func (s *Source) services(ctx context.Context, window url.Values) ([]string, error) {
	var resp struct {
		TagValues []struct {
			Value string `json:"value"`
		} `json:"tagValues"`
	}
	params := url.Values{"start": window["start"], "end": window["end"]}
	if c := s.conditions(); c != "" {
		// Only the services that have traces with the selector.
		params.Set("q", "{ "+c+" }")
	}
	if err := s.get(ctx, "/api/v2/search/tag/resource.service.name/values", params, &resp); err != nil {
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

// fetchShare is the part of a round's time kept for fetching the traces the
// searches found: no new search starts once less than this is left.
const fetchShare = 0.4

// searchUntil is when a round with ctx's deadline stops starting searches;
// zero without a deadline.
func searchUntil(ctx context.Context) time.Time {
	deadline, ok := ctx.Deadline()
	if !ok {
		return time.Time{}
	}
	return deadline.Add(-time.Duration(float64(time.Until(deadline)) * fetchShare))
}

// fetchWindow bounds trace fetches: without a time range, Tempo looks a
// trace up in every block it keeps. A trace the search found has spans in
// the search window, and its first spans may start up to a window earlier.
func fetchWindow(search url.Values, window time.Duration) url.Values {
	start, err := strconv.ParseInt(search.Get("start"), 10, 64)
	if err != nil {
		return nil
	}
	return url.Values{"start": {strconv.FormatInt(start-int64(window/time.Second), 10)}, "end": search["end"]}
}

// sampling is what one round's searches found.
type sampling struct {
	ids              []string
	searched, failed int
	limited          bool // some search filled its limit: there are more traces
	outOfTime        bool // the round stopped searching to keep time for fetches
	firstErr         error
}

// traceIDs searches recent traces service by service, from services[start]
// and wrapping around, a few services at a time. Each service gets an even
// share of MaxTraces (at least one trace, at most TracesPerService) and the
// searches stop once the cap is reached, or at until, so a round stays within
// budget and the next one carries on from the first service left out. A
// failed search costs its service this round only. searched counts the
// services from start that got an answer, failed or not.
func (s *Source) traceIDs(ctx context.Context, services []string, start int, window url.Values, until time.Time) sampling {
	per := max(1, min(s.cfg.MaxTraces/len(services), s.cfg.TracesPerService))
	seen := map[string]bool{}
	var out sampling
	for out.searched < len(services) && len(out.ids) < s.cfg.MaxTraces {
		if !until.IsZero() && !time.Now().Before(until) {
			out.outOfTime = true
			break
		}
		// As many services as can still fill the cap, if each fills its share.
		remaining := s.cfg.MaxTraces - len(out.ids)
		n := min(searchConcurrency, (remaining+per-1)/per, len(services)-out.searched)
		type answer struct {
			ids []string
			err error
		}
		answers := make([]answer, n)
		limits := make([]int, n)
		var wg sync.WaitGroup
		for k := range n {
			svc := services[(start+out.searched+k)%len(services)]
			limits[k] = min(per, remaining-k*per)
			wg.Add(1)
			go func() {
				defer wg.Done()
				answers[k].ids, answers[k].err = s.search(ctx, svc, limits[k], window)
				if answers[k].err != nil {
					answers[k].err = fmt.Errorf("searching traces of %s: %w", svc, answers[k].err)
				}
			}()
		}
		wg.Wait()
		// A search the round's deadline cut off is no failure of its
		// service: it and the ones after it come first next round.
		answered := n
		if ctx.Err() != nil {
			for k, a := range answers {
				if a.err != nil && errors.Is(a.err, ctx.Err()) {
					answered = k
					break
				}
			}
			out.outOfTime = true
		}
		for k, a := range answers[:answered] {
			if a.err != nil {
				out.failed++
				if out.firstErr == nil {
					out.firstErr = a.err
				}
				continue
			}
			out.limited = out.limited || len(a.ids) >= limits[k]
			taken := 0
			for _, id := range a.ids {
				if taken == limits[k] || len(out.ids) == s.cfg.MaxTraces {
					break
				}
				if id != "" && !seen[id] {
					seen[id] = true
					out.ids = append(out.ids, id)
					taken++
				}
			}
		}
		out.searched += answered
		if answered < n {
			break
		}
	}
	return out
}

// search returns the IDs of up to limit recent traces of one service.
func (s *Source) search(ctx context.Context, svc string, limit int, window url.Values) ([]string, error) {
	params := url.Values{"start": window["start"], "end": window["end"]}
	q := fmt.Sprintf(`resource.service.name = %q`, svc)
	if c := s.conditions(); c != "" {
		q += " && " + c
	}
	params.Set("q", "{ "+q+" }")
	params.Set("limit", strconv.Itoa(limit))
	var resp struct {
		Traces []struct {
			TraceID string `json:"traceID"`
		} `json:"traces"`
	}
	if err := s.get(ctx, "/api/search", params, &resp); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resp.Traces))
	for _, t := range resp.Traces {
		ids = append(ids, t.TraceID)
	}
	return ids, nil
}

// fetch downloads the traces. It returns the ones it got, how many failed
// and the first failure.
func (s *Source) fetch(ctx context.Context, ids []string, bounds url.Values) ([]sampledTrace, int, error) {
	out := make([][]span, len(ids))
	errs := make([]error, len(ids))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			var raw otlpTrace
			if err := s.get(ctx, "/api/v2/traces/"+url.PathEscape(id), bounds, &raw); err != nil {
				errs[i] = err
				return
			}
			out[i] = raw.spans()
		}()
	}
	wg.Wait()
	var traces []sampledTrace
	failed := 0
	var first error
	for i, err := range errs {
		if err != nil {
			failed++
			if first == nil {
				first = fmt.Errorf("trace %s: %w", ids[i], err)
			}
			continue
		}
		traces = append(traces, sampledTrace{id: ids[i], spans: out[i]})
	}
	return traces, failed, first
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
	Name         string          `json:"name"`
	Kind         json.RawMessage `json:"kind"`
	Attributes   []attribute     `json:"attributes"`
	Status       struct {
		Code json.RawMessage `json:"code"`
	} `json:"status"`
	Links []model.OperationSpanLink `json:"links"`
	// Nanoseconds since the epoch, as a string or a number depending on the
	// encoder.
	StartTimeUnixNano json.RawMessage `json:"startTimeUnixNano"`
	EndTimeUnixNano   json.RawMessage `json:"endTimeUnixNano"`
}

// times preserves the original span's timestamps, accepting OTLP JSON's
// string and number encodings without going through floating point. A zero
// duration is valid, but missing, nonpositive and reversed times are not.
func (sp otlpSpan) times() (time.Time, time.Time) {
	start, err1 := strconv.ParseInt(strings.Trim(string(sp.StartTimeUnixNano), `"`), 10, 64)
	end, err2 := strconv.ParseInt(strings.Trim(string(sp.EndTimeUnixNano), `"`), 10, 64)
	if err1 != nil || err2 != nil || start <= 0 || end < start {
		return time.Time{}, time.Time{}
	}
	return time.Unix(0, start).UTC(), time.Unix(0, end).UTC()
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
				start, end := sp.times()
				out = append(out, span{
					id: sp.SpanID, parent: sp.ParentSpanID, service: service,
					name: sp.Name, status: strings.Trim(string(sp.Status.Code), `"`), links: sp.Links,
					kind: spanKind(sp.Kind), attrs: attrs(sp.Attributes), dur: end.Sub(start),
					startedAt: start, endedAt: end,
				})
			}
		}
	}
	return out
}
