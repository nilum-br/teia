// Package query turns any PromQL or LogQL query a company already trusts
// into communication edges, with no code: one series per caller/callee pair,
// and a mapping from labels to the two ends.
//
// It covers what no preset knows about: access logs in Loki, ingress
// controllers, custom exporters, flow logs shipped as metrics. An optional
// errors query (failed requests per second, grouped the same way) and latency
// query (p95 in seconds) give the edges their health; without the first, the
// edges' failures are unknown, not zero.
package query

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
	"github.com/nilum-br/teia/internal/source/promapi"
)

// Dialect selects the API the query is sent to.
type Dialect string

const (
	Prometheus Dialect = "prometheus"
	Loki       Dialect = "loki"
)

// Config maps a query's labels to an edge.
type Config struct {
	Dialect Dialect
	URL     string
	Headers map[string]string
	// Query may use $window, replaced by the window as a range selector.
	// Its value is requests (or log lines) per second.
	Query string
	// Errors, optional, returns failed requests per second with the same
	// From and To labels; Latency, optional, the p95 in seconds.
	Errors, Latency string
	Window          time.Duration
	// From and To are label names holding the caller and the callee.
	From, To string
	// FromAddress / ToAddress say the label holds an IP (optionally with a
	// port) to be resolved through inventory, instead of a name.
	FromAddress, ToAddress bool
	// Protocol is a fixed protocol, ProtocolLabel a label holding it.
	Protocol, ProtocolLabel string
	// ToKind optionally sets the callee kind (database, cache, external...).
	ToKind model.NodeKind
	// Dimensions maps a business dimension (journey, tenant...) to the label
	// holding it. The queries must keep those labels in their grouping; each
	// combination becomes a slice of the edge with its own rates and p95.
	Dimensions map[string]string
}

type Source struct {
	name   string
	cfg    Config
	client *promapi.Client
}

func New(name string, cfg Config) *Source {
	path := "/api/v1/query"
	if cfg.Dialect == Loki {
		path = "/loki/api/v1/query"
	}
	if cfg.Window == 0 {
		cfg.Window = 5 * time.Minute
	}
	return &Source{name: name, cfg: cfg, client: promapi.New(cfg.URL, path, cfg.Headers)}
}

func (s *Source) Name() string { return s.name }

func (s *Source) signal() string {
	if s.cfg.Dialect == Loki {
		return source.SignalLogs
	}
	return source.SignalMetrics
}
func (s *Source) Type() string { return string(s.cfg.Dialect) + "-query" }

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	obs := source.Observation{EvaluatedAt: time.Now().UTC()}
	samples, err := s.client.QueryAt(ctx, s.expand(s.cfg.Query), obs.EvaluatedAt)
	if err != nil {
		var denied *promapi.DeniedError
		if errors.As(err, &denied) {
			obs.Degraded = true
			obs.Message = "access denied"
			obs.Findings = append(obs.Findings, model.Finding{
				Rule:     "query-denied",
				Kind:     model.FindingPermission,
				Severity: model.SeverityCritical,
				Subject:  s.name,
				Title:    fmt.Sprintf("%s refused teia's query (%s)", promapi.RedactURL(s.cfg.URL), denied),
				Fix: &model.Fix{
					Summary:  "Give teia a read-only credential and pass it as a header.",
					Language: "yaml",
					Snippet:  "headers:\n  Authorization: Bearer ${TEIA_TOKEN}",
				},
			})
			return obs, nil
		}
		return obs, fmt.Errorf("%s query: %w", s.cfg.Dialect, err)
	}
	if len(samples) == 0 {
		obs.Degraded = true
		obs.Message = "the query returned no series in the window"
		return obs, nil
	}

	// One edge per caller and callee, whatever other labels split the series:
	// rates add up, the protocol is the busiest series'.
	type acc struct {
		edge             source.EdgeObservation
		busiest, p95Sum  float64
		failed, p95Count float64
		slices           map[string]*sliceAcc
		sliceOrder       []string
	}
	edges := map[string]*acc{}
	var order []string
	skipped := 0
	for _, smp := range samples {
		from, to, ok := s.ends(smp.Metric)
		if !ok {
			skipped++
			continue
		}
		key := from + "->" + to
		a := edges[key]
		if a == nil {
			a = &acc{edge: source.EdgeObservation{From: from, To: to, Signal: s.signal(), Window: s.cfg.Window, NoErrors: s.cfg.Errors == ""}, slices: map[string]*sliceAcc{}}
			edges[key] = a
			order = append(order, key)
			obs.Nodes = append(obs.Nodes,
				source.NodeObservation{ID: from, Kind: model.KindUnknown},
				source.NodeObservation{ID: to, Kind: kindOr(s.cfg.ToKind)},
			)
		}
		a.edge.RequestRate += smp.Value
		if smp.Value >= a.busiest {
			a.busiest = smp.Value
			a.edge.Protocol = s.protocol(smp.Metric)
		}
		if dims := s.dims(smp.Metric); dims != nil {
			sk := sliceKey(dims)
			sl := a.slices[sk]
			if sl == nil {
				sl = &sliceAcc{slice: source.Slice{Dims: dims, NoErrors: s.cfg.Errors == ""}}
				a.slices[sk] = sl
				a.sliceOrder = append(a.sliceOrder, sk)
			}
			sl.slice.RequestRate += smp.Value
		}
	}
	if skipped == len(samples) {
		obs.Degraded = true
		obs.Message = fmt.Sprintf("no series had both %q and %q labels", s.cfg.From, s.cfg.To)
		return obs, nil
	}

	var problems []string
	if s.cfg.Errors != "" {
		failed, err := s.client.QueryAt(ctx, s.expand(s.cfg.Errors), obs.EvaluatedAt)
		if err != nil {
			problems = append(problems, "errors query failed: "+promapi.RedactError(err).Error())
		}
		for _, smp := range failed {
			if from, to, ok := s.ends(smp.Metric); ok && edges[from+"->"+to] != nil {
				a := edges[from+"->"+to]
				a.failed += smp.Value
				if sl := a.slices[sliceKey(s.dims(smp.Metric))]; sl != nil {
					sl.failed += smp.Value
				}
			}
		}
		for _, a := range edges {
			a.edge.NoErrors = err != nil
			if err == nil && a.edge.RequestRate > 0 {
				a.edge.ErrorRate = math.Min(1, a.failed/a.edge.RequestRate)
			}
			for _, sl := range a.slices {
				sl.slice.NoErrors = err != nil
				if err == nil && sl.slice.RequestRate > 0 {
					sl.slice.ErrorRate = math.Min(1, sl.failed/sl.slice.RequestRate)
				}
			}
		}
	}
	if s.cfg.Latency != "" {
		latency, err := s.client.QueryAt(ctx, s.expand(s.cfg.Latency), obs.EvaluatedAt)
		if err != nil {
			problems = append(problems, "latency query failed: "+promapi.RedactError(err).Error())
		}
		for _, smp := range latency {
			if math.IsNaN(smp.Value) || math.IsInf(smp.Value, 0) {
				continue // no calls in a bucket range: no quantile, not zero
			}
			if from, to, ok := s.ends(smp.Metric); ok && edges[from+"->"+to] != nil {
				a := edges[from+"->"+to]
				a.p95Sum += smp.Value * 1000
				a.p95Count++
				if sl := a.slices[sliceKey(s.dims(smp.Metric))]; sl != nil {
					sl.p95Sum += smp.Value * 1000
					sl.p95Count++
				}
			}
		}
		for _, a := range edges {
			if a.p95Count > 0 {
				a.edge.LatencyP95Ms = a.p95Sum / a.p95Count
				a.edge.LatencyApproximate = a.p95Count > 1
			}
			var weighted, weight float64
			for _, sl := range a.slices {
				if sl.p95Count > 0 {
					sl.slice.LatencyP95Ms = sl.p95Sum / sl.p95Count
					sl.slice.LatencyApproximate = sl.p95Count > 1
					weighted += sl.slice.LatencyP95Ms * sl.slice.RequestRate
					weight += sl.slice.RequestRate
				}
			}
			// Series split by dimensions: weight each slice's p95 by its
			// traffic rather than averaging a quiet slice like a busy one.
			// Still an approximation, never a sum of percentiles.
			if weight > 0 && len(a.slices) > 1 {
				a.edge.LatencyP95Ms = weighted / weight
				a.edge.LatencyApproximate = true
			}
		}
	}
	for _, key := range order {
		a := edges[key]
		for _, sk := range a.sliceOrder {
			a.edge.Slices = append(a.edge.Slices, a.slices[sk].slice)
		}
		obs.Edges = append(obs.Edges, a.edge)
	}
	for dim := range s.cfg.Dimensions {
		obs.Dimensions = append(obs.Dimensions, dim)
	}
	sort.Strings(obs.Dimensions)
	if len(problems) > 0 {
		obs.Degraded = true
		obs.Message = strings.Join(problems, "; ")
	}
	return obs, nil
}

type sliceAcc struct {
	slice                    source.Slice
	failed, p95Sum, p95Count float64
}

// dims reads a series' business dimensions; nil when none is configured or
// the series carries none of them.
func (s *Source) dims(m map[string]string) map[string]string {
	var out map[string]string
	for dim, label := range s.cfg.Dimensions {
		if v := strings.TrimSpace(m[label]); v != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[dim] = v
		}
	}
	return out
}

// sliceKey identifies a combination of dimension values.
func sliceKey(dims map[string]string) string {
	if len(dims) == 0 {
		return ""
	}
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

// expand puts the window into a query.
func (s *Source) expand(q string) string {
	return strings.ReplaceAll(q, "$window", promapi.Duration(s.cfg.Window))
}

// ends reads a series' caller and callee; ok is false when one is missing.
func (s *Source) ends(m map[string]string) (from, to string, ok bool) {
	from = endpointID(m[s.cfg.From], s.cfg.FromAddress)
	to = endpointID(m[s.cfg.To], s.cfg.ToAddress)
	return from, to, from != "" && to != ""
}

// protocol is the configured one, or the series' protocol label.
func (s *Source) protocol(m map[string]string) string {
	if s.cfg.ProtocolLabel != "" && m[s.cfg.ProtocolLabel] != "" {
		return strings.ToLower(m[s.cfg.ProtocolLabel])
	}
	return s.cfg.Protocol
}

// endpointID turns a label value into a node ID. Addresses become ip:<host>,
// with any port dropped, so inventory sources can resolve them.
func endpointID(v string, address bool) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "-" {
		return ""
	}
	if !address {
		return v
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	return source.AddressID(strings.Trim(v, "[]"))
}

func kindOr(k model.NodeKind) model.NodeKind {
	if k == "" {
		return model.KindUnknown
	}
	return k
}
