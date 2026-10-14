// Package model holds the types shared by sources, the engine, the advisor
// and the API: the communication graph, findings and coverage.
package model

import (
	"fmt"
	"time"
)

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

// Health is how a node or edge is behaving: the worst of its reasons.
type Health string

const (
	HealthHealthy  Health = "healthy"  // traffic, and every golden signal within its limits
	HealthDegraded Health = "degraded" // some signal past its degraded limit
	HealthFailing  Health = "failing"  // some signal past its failing limit
	HealthUnknown  Health = "unknown"  // nothing measured (declared only, or a signal without rates)
)

// Rank orders health from best to worst, for "the worst of".
func (h Health) Rank() int {
	switch h {
	case HealthFailing:
		return 3
	case HealthDegraded:
		return 2
	case HealthHealthy:
		return 1
	default:
		return 0
	}
}

// Cause is the golden signal behind a reason.
type Cause string

const (
	CauseErrors     Cause = "errors"     // Value: failed share, 0..1
	CauseLatency    Cause = "latency"    // Value: p95 in milliseconds
	CauseSaturation Cause = "saturation" // Value: used share of a limit, 0..1 (connections, memory, CPU)
	CauseBacklog    Cause = "backlog"    // Value: work waiting (consumer lag, ready messages)
	CauseLag        Cause = "lag"        // Value: seconds behind, or without progress
	CauseLocks      Cause = "locks"      // Value: sessions waiting on locks
	CauseRestarts   Cause = "restarts"   // Value: restarts in the window
)

// Measure is a reading beyond traffic that tells how a node or edge is
// doing: how full (saturation), how far behind (backlog), how stuck (locks).
// Sources take them; the health judge compares them with their limits.
type Measure struct {
	Cause Cause   `json:"cause"`
	What  string  `json:"what"` // "connections", "sessions waiting on locks", "consumer lag"
	Value float64 `json:"value"`
	// Max is the capacity Value counts against (max_connections, a memory
	// limit); zero when there is none.
	Max float64 `json:"max,omitempty"`
	// Unit of Value and Max: UnitBytes, UnitSeconds, UnitShare (0..1), or
	// empty for a count.
	Unit string `json:"unit,omitempty"`
	// Via names who causes it, when the source knows (the session holding
	// a lock): a node ID, or an address the graph resolves to one.
	Via string `json:"via,omitempty"`
}

// Units of a measure.
const (
	UnitBytes   = "bytes"
	UnitSeconds = "seconds"
	UnitShare   = "share"
)

// FormatValue writes a measure's value in its unit.
func FormatValue(v float64, unit string) string {
	switch unit {
	case UnitBytes:
		return FormatBytes(v)
	case UnitSeconds:
		return FormatSeconds(v)
	case UnitShare:
		return FormatPercent(v)
	}
	return FormatCount(v)
}

// FormatBytes writes a size in binary units: 512 B, 48.0 MiB, 1.5 GiB.
func FormatBytes(b float64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%.0f B", b)
	}
	div, exp := float64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", b/div, "KMGTP"[exp])
}

// Reason says why a node or edge is not healthy: which signal crossed which
// limit, so the map can say "p95 2.3 s" instead of only turning red.
type Reason struct {
	Cause   Cause   `json:"cause"`
	Health  Health  `json:"health"` // degraded or failing
	Value   float64 `json:"value"`
	Limit   float64 `json:"limit"` // the limit it crossed, in Value's unit
	Summary string  `json:"summary"`
	// What is the measure behind a saturation, backlog or locks reason.
	What string `json:"what,omitempty"`
	// Via names the dependency this most likely comes from: a slow or
	// failing callee that explains the caller's latency or errors.
	Via string `json:"via,omitempty"`
}

// Worst returns the worst health among reasons, or fallback when there is
// none.
func Worst(reasons []Reason, fallback Health) Health {
	h := fallback
	for _, r := range reasons {
		if r.Health.Rank() > h.Rank() {
			h = r.Health
		}
	}
	return h
}

// FormatMs writes a duration in milliseconds the way people read it
// ("0.4 ms", "12 ms", "1.2 s").
func FormatMs(ms float64) string {
	switch {
	case ms >= 999.5: // rounds to 1000 or more
		return fmt.Sprintf("%.1f s", ms/1000)
	case ms >= 9.95: // rounds to 10 or more
		return fmt.Sprintf("%.0f ms", ms)
	default:
		return fmt.Sprintf("%.1f ms", ms)
	}
}

// FormatCount writes a count the way people read it: 7, 1.2k, 45k, 1.3M.
func FormatCount(n float64) string {
	switch {
	case n >= 1e7:
		return fmt.Sprintf("%.0fM", n/1e6)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", n/1e6)
	case n >= 1e4:
		return fmt.Sprintf("%.0fk", n/1e3)
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}

// FormatSeconds writes a duration in seconds the way people read it:
// "45 s", "12 min", "2.5 h".
func FormatSeconds(s float64) string {
	switch {
	case s >= 5400:
		return fmt.Sprintf("%.1f h", s/3600)
	case s >= 90:
		return fmt.Sprintf("%.0f min", s/60)
	}
	return fmt.Sprintf("%.0f s", s)
}

// FormatPercent writes a share (0..1) as a percentage with one decimal.
func FormatPercent(share float64) string { return fmt.Sprintf("%.1f%%", share*100) }

// Node is a workload, datastore or external endpoint.
type Node struct {
	ID   string   `json:"id"`
	Kind NodeKind `json:"kind"`
	// Tech is the recognised technology (postgresql, redis, kafka...).
	Tech       string     `json:"tech,omitempty"`
	Visibility Visibility `json:"visibility"`
	Health     Health     `json:"health"`
	// RequestRate and ErrorRate summarise traffic into the node.
	RequestRate float64 `json:"requestRate,omitempty"`
	ErrorRate   float64 `json:"errorRate,omitempty"`
	// LatencyP50Ms and LatencyP95Ms summarise how long the calls it serves
	// take, in milliseconds: its callers' percentiles weighted by their
	// traffic, an approximation (percentiles do not add up exactly).
	LatencyP50Ms float64 `json:"latencyP50Ms,omitempty"`
	LatencyP95Ms float64 `json:"latencyP95Ms,omitempty"`
	// Measures are readings beyond traffic (connections in use...).
	Measures []Measure `json:"measures,omitempty"`
	// Reasons explain a health other than healthy, worst first.
	Reasons   []Reason          `json:"reasons,omitempty"`
	Sources   []string          `json:"sources"`
	Addresses []string          `json:"addresses,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
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
	// LatencyP50Ms and LatencyP95Ms are how long calls take as the caller
	// sees them, in milliseconds, when a signal measures durations (the
	// histograms of the service graph, a mesh or Hubble; sampled spans).
	LatencyP50Ms float64 `json:"latencyP50Ms,omitempty"`
	LatencyP95Ms float64 `json:"latencyP95Ms,omitempty"`
	// Measures are readings beyond traffic (sessions waiting on locks...).
	Measures []Measure `json:"measures,omitempty"`
	// Reasons explain a health other than healthy, worst first.
	Reasons []Reason `json:"reasons,omitempty"`
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
