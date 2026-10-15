// Package engine runs discovery rounds: it asks every source what it sees,
// merges the answers into one graph, lets the advisor comment on it, and
// tells subscribers and notifiers what changed.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nilum-br/teia/internal/advisor"
	"github.com/nilum-br/teia/internal/evidencelinks"
	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/history"
	"github.com/nilum-br/teia/internal/journey"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/slice"
	"github.com/nilum-br/teia/internal/source"
)

type Options struct {
	History  *history.Store
	Interval time.Duration
	// Timeout bounds each source in a round.
	Timeout         time.Duration
	Aliases         map[string]string
	Ignore          []string
	Health          graph.HealthThresholds
	HealthOverrides []graph.HealthOverride
	// Retention keeps an observed edge alive this long after its last
	// sighting, and keeps using a failing source's last good observation
	// this long after it was taken; zero means three intervals.
	Retention     time.Duration
	Notifiers     []Notifier
	EvidenceLinks []evidencelinks.Template
	// Cluster names the cluster these sources describe, when teia maps
	// several: it is stamped on every snapshot and change.
	Cluster string
	// Phase delays Run's first round. Clusters spread their rounds across
	// the interval instead of querying every backend and building every
	// graph at the same moment.
	Phase time.Duration
}

// Notifier receives the changes of every round that has some.
type Notifier interface {
	Notify(ctx context.Context, changes []model.Change) error
}

// recentChanges bounds the activity feed kept in memory.
const recentChanges = 300

// waitMargin is how long a round waits past the timeout for a source that
// ignores its context before it gives up on it.
const waitMargin = 2 * time.Second

type Engine struct {
	sources   []source.Source
	opts      Options
	log       *slog.Logger
	memory    graph.Memory
	frequency graph.FrequencyMemory
	differ    Differ
	// lastGood is each source's last successful observation, reused while
	// the source fails so one bad round does not erase what it knew.
	lastGood []goodObservation
	// busy marks sources still running from an earlier round, so a source
	// that never returns does not pile up a goroutine every round.
	busy   []atomic.Bool
	now    func() time.Time
	margin time.Duration

	mu       sync.RWMutex
	snap     model.Snapshot
	snapJSON []byte
	// results and statuses of the last round, for sliced views.
	results  []graph.Result
	statuses []model.SourceStatus
	sliced   map[string]model.Snapshot
	recent   []model.Change
	subs     map[chan []byte]struct{}
	running  sync.Mutex // one round at a time
}

type goodObservation struct {
	obs source.Observation
	at  time.Time
}

func New(sources []source.Source, opts Options, log *slog.Logger) *Engine {
	if opts.Interval == 0 {
		opts.Interval = 30 * time.Second
	}
	if opts.Timeout == 0 {
		opts.Timeout = 20 * time.Second
	}
	if opts.Health == (graph.HealthThresholds{}) {
		opts.Health = graph.DefaultHealth
	}
	if opts.Retention == 0 {
		opts.Retention = 3 * opts.Interval
	}
	return &Engine{
		sources: sources, opts: opts, log: log,
		memory:   graph.Memory{Retention: opts.Retention},
		lastGood: make([]goodObservation, len(sources)),
		busy:     make([]atomic.Bool, len(sources)),
		now:      time.Now,
		margin:   waitMargin,
		subs:     map[chan []byte]struct{}{},
	}
}

// Snapshot returns the result of the last round.
func (e *Engine) Snapshot() model.Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snap
}

// SnapshotJSON returns the last snapshot encoded once for every reader, or
// nil before the first round. Callers must not modify it.
func (e *Engine) SnapshotJSON() []byte {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snapJSON
}

// Changes returns the most recent changes, oldest first.
func (e *Engine) Changes() []model.Change {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]model.Change(nil), e.recent...)
}

// Subscribe returns a channel that receives the JSON of every new snapshot,
// shared by all subscribers (do not modify it), and a function to stop.
// Slow subscribers only get the latest snapshot.
func (e *Engine) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 1)
	e.mu.Lock()
	e.subs[ch] = struct{}{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subs, ch)
		e.mu.Unlock()
	}
}

// Run refreshes after Phase (immediately without one) and then on every
// interval until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	if e.opts.Phase > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.opts.Phase):
		}
	}
	ticker := time.NewTicker(e.opts.Interval)
	defer ticker.Stop()
	for {
		e.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Refresh runs one discovery round and stores its snapshot.
func (e *Engine) Refresh(ctx context.Context) model.Snapshot {
	e.running.Lock()
	defer e.running.Unlock()

	answers := e.discover(ctx)
	now := e.now().UTC()
	results := make([]graph.Result, len(e.sources))
	statuses := make([]model.SourceStatus, len(e.sources))
	for i, src := range e.sources {
		a := answers[i]
		results[i] = graph.Result{Source: src, Observation: a.obs, Err: a.err, CollectedAt: a.collectedAt}
		statuses[i] = status(src, a.obs, a.err, a.took)
		last := e.lastGood[i]
		switch {
		case a.err == nil:
			e.lastGood[i] = goodObservation{obs: a.obs, at: a.collectedAt}
			statuses[i].LastSuccessAt = a.collectedAt
		case !last.at.IsZero() && now.Sub(last.at) <= e.opts.Retention:
			// Keep what the source knew for a while: a bad flows file would
			// otherwise turn every ok edge into a ghost, and a failed
			// inventory every name back into an address.
			age := now.Sub(last.at).Round(time.Second)
			results[i] = graph.Result{Source: src, Observation: topologyOnly(last.obs), CollectedAt: last.at, Retained: true}
			statuses[i] = status(src, last.obs, nil, a.took)
			statuses[i].State = model.SourceError
			statuses[i].LastSuccessAt, statuses[i].Retained = last.at, true
			statuses[i].Message = fmt.Sprintf("failed: %v; using the last good observation from %s ago", a.err, age)
		default:
			e.lastGood[i] = goodObservation{}
		}
	}

	g := graph.Build(results, e.opts.Aliases, e.opts.Ignore)
	e.memory.Retain(g, now)
	graph.ApplyTech(g)
	graph.ApplyHealth(g, e.opts.Health, e.opts.HealthOverrides...)
	for _, err := range e.frequency.Apply(g, now, e.opts.History.LastObservedEdge) {
		e.log.Warn("evaluating expected flow frequency", "err", err)
	}
	// Explorers name their node as configured; the graph may spell it otherwise.
	for i := range statuses {
		for j, id := range statuses[i].Explores {
			statuses[i].Explores[j] = g.Canonical(id)
		}
	}

	var fromSources []model.Finding
	for _, r := range results {
		for _, f := range r.Observation.Findings {
			if !g.Ignored[g.Canonical(f.Subject)] {
				fromSources = append(fromSources, f)
			}
		}
	}

	e.mu.RLock()
	prev := e.snap
	e.mu.RUnlock()

	snap := model.Snapshot{
		Cluster:     e.opts.Cluster,
		Round:       prev.Round + 1,
		GeneratedAt: now,
		TimeMode:    "current",
		Nodes:       g.SortedNodes(),
		Edges:       g.SortedEdges(),
		Findings:    advisor.Advise(advisor.Input{Graph: g, Statuses: statuses}, fromSources),
		Sources:     statuses,
		Coverage:    graph.ComputeCoverage(g),
	}
	snap.Dimensions = slice.Values(snap.Edges)
	snap.Journeys = journey.Build(snap, "")
	snap.Operations = operationContract(results, g)
	snap.Changes = e.differ.Diff(prev, snap, now)
	for i := range snap.Changes {
		snap.Changes[i].Cluster = e.opts.Cluster
	}
	evidencelinks.Apply(&snap, e.opts.EvidenceLinks)
	if _, err := e.opts.History.Save(snap); err != nil {
		e.log.Error("saving observation history", "round", snap.Round, "err", err)
	}

	// Encode once for every stream subscriber instead of once per client.
	data, err := json.Marshal(snap)
	if err != nil {
		e.log.Error("encoding snapshot", "round", snap.Round, "err", err)
		data = nil
	}

	e.mu.Lock()
	e.snap, e.snapJSON = snap, data
	e.results, e.statuses, e.sliced = results, statuses, map[string]model.Snapshot{}
	e.recent = append(e.recent, snap.Changes...)
	if len(e.recent) > recentChanges {
		e.recent = e.recent[len(e.recent)-recentChanges:]
	}
	if data != nil {
		for ch := range e.subs {
			select {
			case <-ch: // drop the stale snapshot a slow reader has not taken
			default:
			}
			ch <- data
		}
	}
	e.mu.Unlock()

	if len(snap.Changes) > 0 {
		for _, n := range e.opts.Notifiers {
			go func() {
				nctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				if err := n.Notify(nctx, snap.Changes); err != nil {
					e.log.Warn("notification failed", "err", err)
				}
			}()
		}
	}

	e.log.Info("discovery round", "round", snap.Round,
		"nodes", len(snap.Nodes), "edges", len(snap.Edges), "findings", len(snap.Findings), "changes", len(snap.Changes),
		"visibility", snap.Coverage.Visibility, "declared", snap.Coverage.Declared)
	for _, s := range statuses {
		if s.State != model.SourceOK {
			e.log.Warn("source not healthy", "source", s.Name, "state", s.State, "message", s.Message)
		}
	}
	return snap
}

// maxSliced bounds the sliced snapshots cached for one round.
const maxSliced = 64

// Sliced rebuilds the last round from only the calls whose telemetry carried
// every selected dimension value. Health, journeys and findings are judged
// on those calls alone; the round number and time are the last round's.
// A selection nothing matched gives an empty snapshot with Matched false.
// Sampled operations keep the traces whose entry carried the selection, and
// in them the calls that did; a source that cannot split its traces is
// listed as excluded.
func (e *Engine) Sliced(sel slice.Selector) model.Snapshot {
	key := sel.String()
	e.mu.RLock()
	base, results, statuses := e.snap, e.results, e.statuses
	cached, ok := e.sliced[key]
	e.mu.RUnlock()
	if ok {
		return cached
	}
	narrowed, contributed, excluded := slice.Results(results, sel)
	g := graph.BuildAt(narrowed, e.opts.Aliases, e.opts.Ignore, base.GeneratedAt)
	slice.Prune(g)
	graph.ApplyTech(g)
	graph.ApplyHealth(g, e.opts.Health, e.opts.HealthOverrides...)
	var fromSources []model.Finding
	for _, r := range narrowed {
		fromSources = append(fromSources, r.Observation.Findings...)
	}
	snap := model.Snapshot{
		Cluster:     base.Cluster,
		Round:       base.Round,
		GeneratedAt: base.GeneratedAt,
		TimeMode:    base.TimeMode,
		Nodes:       g.SortedNodes(),
		Edges:       g.SortedEdges(),
		Findings:    advisor.Advise(advisor.Input{Graph: g, Statuses: statuses}, fromSources),
		Sources:     statuses,
		Coverage:    graph.ComputeCoverage(g),
		Dimensions:  base.Dimensions,
		Operations:  operationContract(narrowed, g),
		Slice: &model.SliceScope{
			Selector: map[string]string(sel), Metrics: "scoped",
			Sources: contributed, Excluded: excluded,
		},
	}
	if snap.Sources == nil {
		snap.Sources = []model.SourceStatus{}
	}
	for _, edge := range snap.Edges {
		if edge.Observed {
			snap.Slice.Matched = true
			break
		}
	}
	snap.Journeys = journey.Build(snap, "")
	evidencelinks.Apply(&snap, e.opts.EvidenceLinks)
	e.mu.Lock()
	if e.sliced != nil && e.snap.Round == base.Round && len(e.sliced) < maxSliced {
		e.sliced[key] = snap
	}
	e.mu.Unlock()
	return snap
}

// topologyOnly preserves identities and intent during a source outage, but
// old traffic and readings cannot establish the current health of a service.
// Clone the slices we change: a source may reuse its observation on recovery.
func topologyOnly(obs source.Observation) source.Observation {
	obs.Operations = nil
	obs.Nodes = slices.Clone(obs.Nodes)
	for i := range obs.Nodes {
		obs.Nodes[i].Measures = nil
	}
	obs.Edges = slices.Clone(obs.Edges)
	for i := range obs.Edges {
		edge := &obs.Edges[i]
		edge.RequestRate, edge.ErrorRate = 0, 0
		edge.LatencyP50Ms, edge.LatencyP95Ms = 0, 0
		edge.LatencyApproximate = false
		edge.NoErrors = true
		edge.Measures = nil
		edge.Slices = slices.Clone(edge.Slices)
		for j := range edge.Slices {
			sl := &edge.Slices[j]
			sl.RequestRate, sl.ErrorRate = 0, 0
			sl.LatencyP50Ms, sl.LatencyP95Ms = 0, 0
			sl.LatencyApproximate = false
			sl.NoErrors = true
		}
	}
	obs.Readings = nil
	obs.UnknownCallers = nil
	return obs
}

// answer is what one source returned in a round.
type answer struct {
	obs         source.Observation
	err         error
	took        time.Duration
	collectedAt time.Time
}

// discover asks every source in parallel. A source that panics fails
// alone, and one that ignores its context is given up on shortly after the
// timeout: its late answer lands in a buffer nobody reads.
func (e *Engine) discover(ctx context.Context) []answer {
	type indexed struct {
		i int
		answer
	}
	done := make(chan indexed, len(e.sources))
	out := make([]answer, len(e.sources))
	got := make([]bool, len(e.sources))
	pending := 0
	for i, src := range e.sources {
		if !e.busy[i].CompareAndSwap(false, true) {
			out[i], got[i] = answer{err: fmt.Errorf("skipped: still running from an earlier round")}, true
			continue
		}
		pending++
		go func() {
			sctx, cancel := context.WithTimeout(ctx, e.opts.Timeout)
			defer cancel()
			start := time.Now()
			obs, err := e.discoverOne(sctx, src)
			collectedAt := e.now().UTC()
			// Free the slot before answering: once the round has every
			// answer it may end, and the next round must find this source
			// idle. A source past the deadline keeps its slot until it
			// returns, which is what the busy check is for.
			e.busy[i].Store(false)
			done <- indexed{i, answer{obs: obs, err: err, took: time.Since(start), collectedAt: collectedAt}}
		}()
	}

	wait := e.opts.Timeout + e.margin
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for ; pending > 0; pending-- {
		select {
		case a := <-done:
			out[a.i], got[a.i] = a.answer, true
		case <-deadline.C:
			for i := range out {
				if !got[i] {
					out[i] = answer{err: fmt.Errorf("timed out: no answer within %s, its late answer will be discarded", wait), took: wait}
				}
			}
			return out
		}
	}
	return out
}

// discoverOne turns a panicking adapter into a failed source, so one bug
// does not take the whole process down.
func (e *Engine) discoverOne(ctx context.Context, src source.Source) (obs source.Observation, err error) {
	defer func() {
		if r := recover(); r != nil {
			e.log.Error("source panicked", "source", src.Name(), "panic", r, "stack", string(debug.Stack()))
			obs, err = source.Observation{}, fmt.Errorf("panicked: %v", r)
		}
	}()
	return src.Discover(ctx)
}

func status(src source.Source, obs source.Observation, err error, d time.Duration) model.SourceStatus {
	s := model.SourceStatus{
		Name:     src.Name(),
		Type:     src.Type(),
		State:    model.SourceOK,
		Message:  obs.Message,
		Explores: slices.Clone(obs.Explores),
		Nodes:    source.CountNodes(obs),
		Edges:    len(obs.Edges),
		Duration: d,
	}
	switch {
	case err != nil:
		s.State = model.SourceError
		s.Message = err.Error()
		// A source that failed cannot claim to explore anything.
		s.Explores = nil
	case obs.Degraded:
		s.State = model.SourceDegraded
	}
	return s
}
