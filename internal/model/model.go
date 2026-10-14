// Package model holds the types shared by sources, the engine, the advisor
// and the API: the communication graph, findings and coverage.
package model

import "time"

// NodeKind classifies a node in the communication graph.
type NodeKind string

const (
	KindService  NodeKind = "service"
	KindDatabase NodeKind = "database"
	KindCache    NodeKind = "cache"
	KindQueue    NodeKind = "queue"
	KindExternal NodeKind = "external"
	KindUnknown  NodeKind = "unknown"
)

// Visibility says how much teia can see from a node's own point of view.
type Visibility string

const (
	// VisibilityFull: the node reports its own telemetry.
	VisibilityFull Visibility = "full"
	// VisibilityPartial: the node is only seen through its peers.
	VisibilityPartial Visibility = "partial"
	// VisibilityNone: the node is known to exist but no traffic was seen.
	VisibilityNone Visibility = "none"
)

// Health is how a node or edge is behaving, from its error rate.
type Health string

const (
	HealthHealthy  Health = "healthy"  // errors below the degraded threshold
	HealthDegraded Health = "degraded" // errors between the thresholds
	HealthFailing  Health = "failing"  // errors at or above the failing threshold
	HealthUnknown  Health = "unknown"  // no rate known (declared only, or a signal without rates)
)

// Node is a workload, datastore or external endpoint.
type Node struct {
	ID   string   `json:"id"`
	Kind NodeKind `json:"kind"`
	// Tech is the recognised technology (postgresql, redis, kafka...).
	Tech       string     `json:"tech,omitempty"`
	Visibility Visibility `json:"visibility"`
	Health     Health     `json:"health"`
	// RequestRate and ErrorRate summarise traffic into the node.
	RequestRate float64           `json:"requestRate,omitempty"`
	ErrorRate   float64           `json:"errorRate,omitempty"`
	Sources     []string          `json:"sources"`
	Addresses   []string          `json:"addresses,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// EdgeState is the result of comparing declared and observed communication.
type EdgeState string

const (
	// EdgeOK: declared and observed.
	EdgeOK EdgeState = "ok"
	// EdgeGhost: observed but nobody declared it.
	EdgeGhost EdgeState = "ghost"
	// EdgeStale: declared but no traffic was observed in the window.
	EdgeStale EdgeState = "stale"
)

// Edge is a directed communication from a caller to a callee.
type Edge struct {
	From     string    `json:"from"`
	To       string    `json:"to"`
	Protocol string    `json:"protocol,omitempty"`
	Port     int       `json:"port,omitempty"`
	Declared bool      `json:"declared"`
	Observed bool      `json:"observed"`
	State    EdgeState `json:"state"`
	Health   Health    `json:"health"`
	Sources  []string  `json:"sources"`
	// Signals are the kinds of telemetry that observed the edge: traces,
	// mesh, network, logs, metrics, database.
	Signals     []string `json:"signals,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	RequestRate float64  `json:"requestRate,omitempty"` // requests per second
	ErrorRate   float64  `json:"errorRate,omitempty"`   // failed share, 0..1
}

// Key identifies an edge inside a graph.
func (e Edge) Key() string { return e.From + "->" + e.To }

// Severity orders findings by urgency.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// Rank returns a sortable weight, lower is more urgent.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

// FindingKind groups findings by what the user has to do about them.
type FindingKind string

const (
	// FindingBlindSpot: teia cannot see part of the graph.
	FindingBlindSpot FindingKind = "blind-spot"
	// FindingPermission: a source is connected but lacks a grant.
	FindingPermission FindingKind = "permission"
	// FindingDrift: declared and observed communication disagree.
	FindingDrift FindingKind = "drift"
	// FindingSource: a source is misconfigured or unreachable.
	FindingSource FindingKind = "source"
)

// Fix is the concrete next step attached to a finding.
type Fix struct {
	Summary  string `json:"summary"`
	Snippet  string `json:"snippet,omitempty"`
	Language string `json:"language,omitempty"` // sql, yaml, shell...
}

// Finding is something teia wants the user to know, with a way forward.
type Finding struct {
	ID       string      `json:"id"`
	Rule     string      `json:"rule"`
	Kind     FindingKind `json:"kind"`
	Severity Severity    `json:"severity"`
	Subject  string      `json:"subject"`
	Title    string      `json:"title"`
	Detail   string      `json:"detail,omitempty"`
	Fix      *Fix        `json:"fix,omitempty"`
}

// SourceState is the health of one discovery source.
type SourceState string

const (
	SourceOK       SourceState = "ok"
	SourceDegraded SourceState = "degraded"
	SourceError    SourceState = "error"
)

// SourceStatus reports what a source did in the last discovery round.
type SourceStatus struct {
	Name     string        `json:"name"`
	Type     string        `json:"type"`
	State    SourceState   `json:"state"`
	Message  string        `json:"message,omitempty"`
	Explores []string      `json:"explores,omitempty"` // node IDs this source can look inside
	Nodes    int           `json:"nodes"`
	Edges    int           `json:"edges"`
	Duration time.Duration `json:"duration"`
}

// Coverage summarises how complete the picture is.
type Coverage struct {
	Nodes int `json:"nodes"`
	Edges int `json:"edges"`
	// Visibility is the share of non-external nodes with full visibility.
	Visibility float64 `json:"visibility"`
	// Declared is the share of observed edges that are declared.
	Declared float64 `json:"declared"`
	// Observed is the share of declared edges that are observed.
	Observed float64 `json:"observed"`
}

// ChangeType names what changed between two discovery rounds.
type ChangeType string

const (
	ChangeEdgeAdded       ChangeType = "edge.added"
	ChangeEdgeRemoved     ChangeType = "edge.removed"
	ChangeEdgeState       ChangeType = "edge.state"
	ChangeEdgeHealth      ChangeType = "edge.health"
	ChangeNodeAdded       ChangeType = "node.added"
	ChangeNodeRemoved     ChangeType = "node.removed"
	ChangeNodeHealth      ChangeType = "node.health"
	ChangeNodeVisibility  ChangeType = "node.visibility"
	ChangeFindingOpened   ChangeType = "finding.opened"
	ChangeFindingResolved ChangeType = "finding.resolved"
)

// Change is one difference between two rounds, ready to show or send.
type Change struct {
	At      time.Time  `json:"at"`
	Type    ChangeType `json:"type"`
	Subject string     `json:"subject"`
	From    string     `json:"from,omitempty"` // previous value
	To      string     `json:"to,omitempty"`   // new value
	Message string     `json:"message"`
	// ID is the finding's ID on finding changes, so an opened and a resolved
	// finding can be matched when several rules share a subject.
	ID string `json:"id,omitempty"`
}

// Snapshot is the result of one discovery round.
type Snapshot struct {
	Round       int64          `json:"round"`
	GeneratedAt time.Time      `json:"generatedAt"`
	Nodes       []Node         `json:"nodes"`
	Edges       []Edge         `json:"edges"`
	Findings    []Finding      `json:"findings"`
	Sources     []SourceStatus `json:"sources"`
	Coverage    Coverage       `json:"coverage"`
	// Changes since the previous round.
	Changes []Change `json:"changes,omitempty"`
}
