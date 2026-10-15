package model

import "time"

// EvidenceLink is a configured public destination, never a source credential.
// Search links carry their evidence period; they do not identify an exact trace.
type EvidenceLink struct {
	Label       string    `json:"label"`
	URL         string    `json:"url"`
	Kind        string    `json:"kind"`
	Description string    `json:"description"`
	PeriodFrom  time.Time `json:"periodFrom,omitzero"`
	PeriodTo    time.Time `json:"periodTo,omitzero"`
}
