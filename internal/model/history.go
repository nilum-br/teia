package model

import "time"

// HistoryContext describes one stored discovery snapshot, not a continuous
// measurement of a requested interval.
type HistoryContext struct {
	ID           string                    `json:"id"`
	RecordedAt   time.Time                 `json:"recordedAt"`
	Availability string                    `json:"availability"`
	Nodes        map[string]EntityPresence `json:"nodes"`
	Edges        map[string]EntityPresence `json:"edges"`
}

// EntityPresence records graph presence separately from original telemetry
// timestamps. A stable ID/address fingerprint cannot prove process identity.
type EntityPresence struct {
	Episode            string    `json:"episode"`
	Identity           string    `json:"identity"`
	IdentityConfidence string    `json:"identityConfidence"`
	FirstSeen          time.Time `json:"firstSeen"`
	LastSeen           time.Time `json:"lastSeen"`
	FirstObservedAt    time.Time `json:"firstObservedAt,omitzero"`
	LastObservedAt     time.Time `json:"lastObservedAt,omitzero"`
}
