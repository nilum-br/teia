package model

// HealthRule is the effective pair of limits for one signal on one component
// or connection. It is present even when the signal is missing or healthy;
// choosing a rule is not evidence that a measurement exists.
type HealthRule struct {
	Cause    Cause   `json:"cause"`
	Name     string  `json:"name"`
	Match    string  `json:"match"` // global, identity or selector
	Priority int     `json:"priority,omitempty"`
	Degraded float64 `json:"degraded"`
	Failing  float64 `json:"failing"`
	Unit     string  `json:"unit"` // share, milliseconds, seconds or count
}
