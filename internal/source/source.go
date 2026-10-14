// Package source defines how discovery adapters report what they see.
//
// A source never builds the graph itself: it returns raw observations and the
// engine merges them, resolves identities and runs the advisor on top.
package source

import (
	"context"
	"strings"

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
	Labels    map[string]string
	// Ignore drops the node (and its edges) from the graph.
	Ignore bool
}

// EdgeObservation is a communication as seen by one source.
type EdgeObservation struct {
	From, To    string
	Protocol    string
	Port        int
	Declared    bool // true for intent (flows files), false for traffic
	Reason      string
	RequestRate float64
	ErrorRate   float64
}

// Observation is everything a source learned in one round.
type Observation struct {
	Nodes []NodeObservation
	Edges []EdgeObservation
	// Findings are raised by the source itself, typically about its own
	// permissions or configuration.
	Findings []model.Finding
	// Explores lists node IDs this source can look inside, so the advisor
	// does not suggest exploring them again.
	Explores []string
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
