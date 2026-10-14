// Package engine runs discovery rounds: it asks every source what it sees,
// merges the answers into one graph and lets the advisor comment on it.
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
}

type Engine struct {
	sources []source.Source
	opts    Options
	log     *slog.Logger

	mu   sync.RWMutex
	snap model.Snapshot
}

func New(sources []source.Source, opts Options, log *slog.Logger) *Engine {
	if opts.Interval == 0 {
		opts.Interval = 30 * time.Second
	}
	if opts.Timeout == 0 {
		opts.Timeout = 20 * time.Second
	}
	return &Engine{sources: sources, opts: opts, log: log}
}

// Snapshot returns the result of the last round.
func (e *Engine) Snapshot() model.Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snap
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

	g := graph.Build(results, e.opts.Aliases, e.opts.Ignore)

	var fromSources []model.Finding
	for _, r := range results {
		for _, f := range r.Observation.Findings {
			if !g.Ignored[f.Subject] {
				fromSources = append(fromSources, f)
			}
		}
	}

	snap := model.Snapshot{
		GeneratedAt: time.Now().UTC(),
		Nodes:       g.SortedNodes(),
		Edges:       g.SortedEdges(),
		Findings:    advisor.Advise(advisor.Input{Graph: g, Statuses: statuses}, fromSources),
		Sources:     statuses,
		Coverage:    graph.ComputeCoverage(g),
	}
	e.mu.Lock()
	e.snap = snap
	e.mu.Unlock()

	e.log.Info("discovery round",
		"nodes", len(snap.Nodes), "edges", len(snap.Edges), "findings", len(snap.Findings),
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
