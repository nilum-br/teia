package model

import (
	"slices"
	"time"
)

// FlowFrequency qualifies rare-flow drift without claiming continuous traffic
// coverage. LastObservedAt is an original source timestamp, never collection time.
type FlowFrequency struct {
	Status         string    `json:"status"` // not-due, overdue, unknown
	LastObservedAt time.Time `json:"lastObservedAt,omitzero"`
	NextExpectedAt time.Time `json:"nextExpectedAt,omitzero"`
	Detail         string    `json:"detail"`
}

// LatestTopologyObservedAt is the shared eligibility contract for current
// frequency evaluation and persisted history. Capture/evaluation timestamps,
// retained evidence, future clocks and invalid intervals never establish a call.
func (e Edge) LatestTopologyObservedAt(now time.Time) time.Time {
	var last time.Time
	if !e.Observed {
		return last
	}
	for _, ev := range e.Evidence {
		if ev.Retained || !slices.Contains(ev.Metrics, "topology") || ev.ObservedAt.IsZero() || ev.ObservedAt.After(now) || (!ev.ObservedFrom.IsZero() && ev.ObservedFrom.After(ev.ObservedAt)) {
			continue
		}
		if ev.ObservedAt.After(last) {
			last = ev.ObservedAt
		}
	}
	return last
}
