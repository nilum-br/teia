package graph

import (
	"fmt"
	"math"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// FrequencyMemory keeps original event timestamps, not times at which snapshots
// were collected. Optional persisted history is read once per declared rare flow.
type FrequencyMemory struct{ edges map[string]frequencySeen }

type frequencySeen struct {
	last   time.Time
	loaded bool
}

type FrequencyLookup func(string) (model.Edge, time.Time, bool, error)

func (m *FrequencyMemory) Apply(g *Graph, now time.Time, lookup FrequencyLookup) []error {
	if m.edges == nil {
		m.edges = map[string]frequencySeen{}
	}
	active := map[string]bool{}
	var errs []error
	for key, edge := range g.Edges {
		edge.Frequency = nil
		if !edge.Declared {
			edge.ExpectedFrequencySeconds = 0
			continue
		}
		if edge.ExpectedFrequencySeconds <= 0 {
			continue
		}
		active[key] = true
		seen := m.edges[key]
		if !seen.loaded {
			if lookup == nil {
				seen.loaded = true
			} else {
				old, _, found, err := lookup(key)
				if err != nil {
					errs = append(errs, fmt.Errorf("frequency history for %s: %w", key, err))
				} else {
					seen.loaded = true
					if found {
						seen.last = old.LatestTopologyObservedAt(now)
					}
				}
			}
		}
		if at := edge.LatestTopologyObservedAt(now); at.After(seen.last) {
			seen.last = at
		}
		m.edges[key] = seen
		status := &model.FlowFrequency{Status: "unknown", Detail: "Original call time is unavailable; a short discovery window cannot assess this expected frequency."}
		if !seen.last.IsZero() {
			status.LastObservedAt = seen.last
			interval := time.Duration(math.MaxInt64)
			if edge.ExpectedFrequencySeconds < float64(math.MaxInt64)/float64(time.Second) {
				interval = time.Duration(edge.ExpectedFrequencySeconds * float64(time.Second))
			}
			status.NextExpectedAt = seen.last.Add(interval)
			if now.After(status.NextExpectedAt) {
				status.Status = "overdue"
				status.Detail = "No newer original observation is known after the expected interval. Sampling or collection gaps may hide a call; this does not prove the job failed."
			} else {
				status.Status = "not-due"
				status.Detail = "The last original observation is within the expected interval; absence in this discovery window is not stale drift."
			}
		}
		edge.Frequency = status
	}
	for key := range m.edges {
		if !active[key] {
			delete(m.edges, key)
		}
	}
	return errs
}
