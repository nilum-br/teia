// Package query turns any PromQL or LogQL query a company already trusts
// into communication edges, with no code: one series per caller/callee pair,
// and a mapping from labels to the two ends.
//
// It covers what no preset knows about: access logs in Loki, ingress
// controllers, custom exporters, flow logs shipped as metrics.
package query

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	Query  string
	Window time.Duration
	// From and To are label names holding the caller and the callee.
	From, To string
	// FromAddress / ToAddress say the label holds an IP (optionally with a
	// port) to be resolved through inventory, instead of a name.
	FromAddress, ToAddress bool
	// Protocol is a fixed protocol, ProtocolLabel a label holding it.
	Protocol, ProtocolLabel string
	// ToKind optionally sets the callee kind (database, cache, external...).
	ToKind model.NodeKind
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
	var obs source.Observation
	q := strings.ReplaceAll(s.cfg.Query, "$window", promapi.Duration(s.cfg.Window))
	samples, err := s.client.Query(ctx, q)
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
				Title:    fmt.Sprintf("%s refused teia's query (%s)", s.cfg.URL, denied),
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
	skipped := 0
	for _, smp := range samples {
		from := endpointID(smp.Metric[s.cfg.From], s.cfg.FromAddress)
		to := endpointID(smp.Metric[s.cfg.To], s.cfg.ToAddress)
		if from == "" || to == "" {
			skipped++
			continue
		}
		protocol := s.cfg.Protocol
		if s.cfg.ProtocolLabel != "" && smp.Metric[s.cfg.ProtocolLabel] != "" {
			protocol = strings.ToLower(smp.Metric[s.cfg.ProtocolLabel])
		}
		obs.Nodes = append(obs.Nodes,
			source.NodeObservation{ID: from, Kind: model.KindUnknown},
			source.NodeObservation{ID: to, Kind: kindOr(s.cfg.ToKind)},
		)
		obs.Edges = append(obs.Edges, source.EdgeObservation{From: from, To: to, Protocol: protocol, RequestRate: smp.Value, Signal: s.signal(), Window: s.cfg.Window})
	}
	if skipped == len(samples) {
		obs.Degraded = true
		obs.Message = fmt.Sprintf("no series had both %q and %q labels", s.cfg.From, s.cfg.To)
	}
	return obs, nil
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
