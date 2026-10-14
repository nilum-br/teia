// Package source defines how discovery adapters report what they see.
//
// A source never builds the graph itself: it returns raw observations and the
// engine merges them, resolves identities and runs the advisor on top.
package source

import (
	"context"
	"strings"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// AddressPrefix marks a node ID that is still a network address, e.g.
// "ip:10.1.2.3". The engine replaces it with a workload name when an
// inventory source knows that address.
const AddressPrefix = "ip:"

// AddressID builds a node ID for an unresolved address.
func AddressID(addr string) string { return AddressPrefix + addr }

// IsAddressID reports whether id is an unresolved address.
func IsAddressID(id string) bool { return strings.HasPrefix(id, AddressPrefix) }

// NodeObservation is a node as seen by one source.
type NodeObservation struct {
	ID   string
	Kind model.NodeKind
	// SelfReported is true when the node emitted the data itself
	// (e.g. its own spans), which gives it full visibility.
	SelfReported bool
	// Addresses lets the engine map "ip:..." IDs back to this node.
	Addresses []string
	// Since is when the node got those addresses (container or pod start).
	// IPs get recycled: an address seen in a time window is only attributed
	// to a node that held it for the whole window.
	Since  time.Time
	Labels map[string]string
	// Ignore drops the node (and its edges) from the graph.
	Ignore bool
	// Measures are readings beyond traffic, such as connections in use.
	Measures []model.Measure
}

// Reading carries measures for a node, or an edge, that another source
// names. Exporters label their metrics with guesses at a name (a service, a
// consumer group, the exporter's own address), so a reading never creates a
// node: the graph gives it to the first candidate it already knows.
type Reading struct {
	// Candidates are names for the node, best first, after aliases and
	// addresses (an "ip:..." candidate becomes the node holding it).
	Candidates []string
	// To, when set, puts the measures on the edge from the matched node to
	// the first of To with such an edge (a consumer group and its topic).
	To       []string
	Measures []model.Measure
	// Origin says where the reading came from, for the finding raised when
	// no candidate matches: "redis (instance catalog-cache-exporter:9121)".
	Origin string
}

// EdgeObservation is a communication as seen by one source.
type EdgeObservation struct {
	From, To string
	Protocol string
	Port     int
	Declared bool // true for intent (flows files), false for traffic
	Reason   string
	// Signal is the kind of telemetry that observed the edge.
	Signal string
	// Window is how far back the observation looks (log and metric
	// queries); zero means "right now" (a database's current sessions).
	Window      time.Duration
	RequestRate float64
	ErrorRate   float64
	// LatencyP50Ms and LatencyP95Ms are call durations in milliseconds;
	// zero when the signal does not measure them.
	LatencyP50Ms float64
	LatencyP95Ms float64
	// Measures are readings beyond traffic, such as sessions of this caller
	// waiting on locks.
	Measures []model.Measure
}

// Signals name the kind of telemetry that saw a node first-hand. A node
// carries the label SignalLabel(signal)="true" for each, so the advisor can
// tell who could have been the "unknown" caller of a given signal.
const (
	SignalTraces   = "traces"   // OpenTelemetry SDKs or agents
	SignalMesh     = "mesh"     // service mesh proxies
	SignalNetwork  = "network"  // eBPF / flow telemetry
	SignalLogs     = "logs"     // access logs and other log lines
	SignalMetrics  = "metrics"  // other metrics (custom queries)
	SignalDatabase = "database" // a datastore reporting its own clients
)

// SignalLabel is the node label recording that a signal saw the node.
func SignalLabel(signal string) string { return "teia.signal." + signal }

// sensitiveLabelKeys are substrings (matched case-insensitively) of label keys
// that tend to carry credentials or whole manifests: basic-auth hashes in
// Traefik labels, tokens and keys passed as labels, the last applied
// configuration kubectl keeps. Labels end up in the unauthenticated API, so
// inventories drop these rather than trust every team to keep labels clean.
var sensitiveLabelKeys = []string{
	"password", "passwd", "secret", "token",
	"apikey", "api-key", "api_key",
	"credential", "basicauth", "auth.users",
	"private-key", "private_key", "cert",
	"kubectl.kubernetes.io/last-applied-configuration",
}

// MaxLabelValue is the longest label value kept: anything longer is a blob
// (a manifest, a certificate, a script), not something to group or filter by.
const MaxLabelValue = 256

// SensitiveLabel reports whether a label should be kept out of the graph,
// by its key or by the size of its value.
func SensitiveLabel(key, value string) bool {
	if len(value) > MaxLabelValue {
		return true
	}
	key = strings.ToLower(key)
	for _, s := range sensitiveLabelKeys {
		if strings.Contains(key, s) {
			return true
		}
	}
	return false
}

// SafeLabels copies the labels that are safe to expose. It never returns nil,
// so callers can add their own labels to the result.
func SafeLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if !SensitiveLabel(k, v) {
			out[k] = v
		}
	}
	return out
}

// UnknownCaller is traffic into Callee whose callers Signal cannot name: the
// service graph "user" (callers without traces), Istio "unknown" (callers
// without a sidecar).
type UnknownCaller struct {
	Callee string
	Signal string
	Rate   float64
}

// Observation is everything a source learned in one round.
type Observation struct {
	Nodes []NodeObservation
	Edges []EdgeObservation
	// Findings are raised by the source itself, typically about its own
	// permissions or configuration.
	Findings []model.Finding
	// Readings are measures for nodes or edges other sources name.
	Readings []Reading
	// Explores lists node IDs this source can look inside, so the advisor
	// does not suggest exploring them again.
	Explores []string
	// UnknownCallers is traffic into a callee from callers this source
	// cannot identify.
	UnknownCallers []UnknownCaller
	// Degraded marks a round that worked but saw less than it could.
	Degraded bool
	Message  string
}

// Source is a discovery adapter.
type Source interface {
	Name() string
	Type() string
	Discover(ctx context.Context) (Observation, error)
}

// CountNodes is the number of distinct nodes an observation names. An
// inventory reports a workload once per pod and once per Service address.
func CountNodes(obs Observation) int {
	seen := make(map[string]struct{}, len(obs.Nodes))
	for _, n := range obs.Nodes {
		seen[n.ID] = struct{}{}
	}
	return len(seen)
}
