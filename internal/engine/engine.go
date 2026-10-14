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
	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

type Options struct {
	Interval time.Duration
	// Timeout bounds each source in a round.
	Timeout time.Duration
	Aliases map[string]string
	Ignore  []string
	Health  graph.HealthThresholds
	// Retention keeps an observed edge alive this long after its last
	// sighting, and keeps using a failing source's last good observation
	// this long after it was taken; zero means three intervals.
	Retention time.Duration
	Notifiers []Notifier
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
	sources []source.Source
	opts    Options
	log     *slog.Logger
	memory  graph.Memory
	differ  Differ
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

// Run refreshes immediately and then on every interval until ctx ends.
func (e *Engine) Run(ctx context.Context) {
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
		results[i] = graph.Result{Source: src, Observation: a.obs, Err: a.err}
		statuses[i] = status(src, a.obs, a.err, a.took)
		last := e.lastGood[i]
		switch {
		case a.err == nil:
			e.lastGood[i] = goodObservation{obs: a.obs, at: now}
		case !last.at.IsZero() && now.Sub(last.at) <= e.opts.Retention:
			// Keep what the source knew for a while: a bad flows file would
			// otherwise turn every ok edge into a ghost, and a failed
			// inventory every name back into an address.
			age := now.Sub(last.at).Round(time.Second)
			results[i] = graph.Result{Source: src, Observation: last.obs}
			statuses[i] = status(src, last.obs, nil, a.took)
			statuses[i].State = model.SourceError
			statuses[i].Message = fmt.Sprintf("failed: %v; using the last good observation from %s ago", a.err, age)
		default:
			e.lastGood[i] = goodObservation{}
		}
	}

	g := graph.Build(results, e.opts.Aliases, e.opts.Ignore)
	e.memory.Retain(g, now)
	graph.ApplyTech(g)
	graph.ApplyHealth(g, e.opts.Health)
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
		Round:       prev.Round + 1,
		GeneratedAt: now,
		Nodes:       g.SortedNodes(),
		Edges:       g.SortedEdges(),
		Findings:    advisor.Advise(advisor.Input{Graph: g, Statuses: statuses}, fromSources),
		Sources:     statuses,
		Coverage:    graph.ComputeCoverage(g),
	}
	snap.Changes = e.differ.Diff(prev, snap, now)

	// Encode once for every stream subscriber instead of once per client.
	data, err := json.Marshal(snap)
	if err != nil {
		e.log.Error("encoding snapshot", "round", snap.Round, "err", err)
		data = nil
	}

	e.mu.Lock()
	e.snap, e.snapJSON = snap, data
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

// answer is what one source returned in a round.
type answer struct {
	obs  source.Observation
	err  error
	took time.Duration
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
			// Free the slot before answering: once the round has every
			// answer it may end, and the next round must find this source
			// idle. A source past the deadline keeps its slot until it
			// returns, which is what the busy check is for.
			e.busy[i].Store(false)
			done <- indexed{i, answer{obs: obs, err: err, took: time.Since(start)}}
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
