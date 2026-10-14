// Package engine runs discovery rounds: it asks every source what it sees,
// merges the answers into one graph, lets the advisor comment on it, and
// tells subscribers and notifiers what changed.
package engine

import (
	"context"
	"log/slog"
	"sync"
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
	// sighting; zero means three intervals.
	Retention time.Duration
	Notifiers []Notifier
}

// Notifier receives the changes of every round that has some.
type Notifier interface {
	Notify(ctx context.Context, changes []model.Change) error
}

// recentChanges bounds the activity feed kept in memory.
const recentChanges = 300

type Engine struct {
	sources []source.Source
	opts    Options
	log     *slog.Logger
	memory  graph.Memory

	mu      sync.RWMutex
	snap    model.Snapshot
	recent  []model.Change
	subs    map[chan model.Snapshot]struct{}
	running sync.Mutex // one round at a time
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
		memory: graph.Memory{Retention: opts.Retention},
		subs:   map[chan model.Snapshot]struct{}{},
	}
}

// Snapshot returns the result of the last round.
func (e *Engine) Snapshot() model.Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snap
}

// Changes returns the most recent changes, oldest first.
func (e *Engine) Changes() []model.Change {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]model.Change(nil), e.recent...)
}

// Subscribe returns a channel that receives every new snapshot, and a
// function to stop. Slow subscribers only get the latest snapshot.
func (e *Engine) Subscribe() (<-chan model.Snapshot, func()) {
	ch := make(chan model.Snapshot, 1)
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

	results := make([]graph.Result, len(e.sources))
	statuses := make([]model.SourceStatus, len(e.sources))
	var wg sync.WaitGroup
	for i, src := range e.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, e.opts.Timeout)
			defer cancel()
			start := time.Now()
			obs, err := src.Discover(sctx)
			results[i] = graph.Result{Source: src, Observation: obs, Err: err}
			statuses[i] = status(src, obs, err, time.Since(start))
		}()
	}
	wg.Wait()

	now := time.Now().UTC()
	g := graph.Build(results, e.opts.Aliases, e.opts.Ignore)
	e.memory.Retain(g, now)
	graph.ApplyTech(g)
	graph.ApplyHealth(g, e.opts.Health)

	var fromSources []model.Finding
	for _, r := range results {
		for _, f := range r.Observation.Findings {
			if !g.Ignored[f.Subject] {
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
	snap.Changes = Diff(prev, snap, now)

	e.mu.Lock()
	e.snap = snap
	e.recent = append(e.recent, snap.Changes...)
	if len(e.recent) > recentChanges {
		e.recent = e.recent[len(e.recent)-recentChanges:]
	}
	for ch := range e.subs {
		select {
		case <-ch: // drop the stale snapshot a slow reader has not taken
		default:
		}
		ch <- snap
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

func status(src source.Source, obs source.Observation, err error, d time.Duration) model.SourceStatus {
	s := model.SourceStatus{
		Name:     src.Name(),
		Type:     src.Type(),
		State:    model.SourceOK,
		Message:  obs.Message,
		Explores: obs.Explores,
		Nodes:    len(obs.Nodes),
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
