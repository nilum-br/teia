package model

import "time"

// IdentityMethod describes the evidence used to name an entity. It is not a
// confidence score: an explicit name and a temporal address match answer
// different questions and cannot be ranked as percentages.
type IdentityMethod string

const (
	IdentityExplicit   IdentityMethod = "explicit"
	IdentityName       IdentityMethod = "name"
	IdentityAlias      IdentityMethod = "alias"
	IdentityIPTemporal IdentityMethod = "ip-temporal"
	IdentityInferred   IdentityMethod = "inferred"
	IdentityAmbiguous  IdentityMethod = "ambiguous"
	IdentityUnresolved IdentityMethod = "unresolved"
)

// IdentityCandidate preserves each inventory's claim separately. Since is the
// reported container/pod/Service start, not an independently verified IP lease.
type IdentityCandidate struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	SourceType  string    `json:"sourceType"`
	Basis       string    `json:"basis,omitempty"`
	Since       time.Time `json:"since,omitzero"`
	CollectedAt time.Time `json:"collectedAt,omitzero"`
	Retained    bool      `json:"retained,omitempty"`
}

// IdentityResolution explains one source's contribution to an identity. A node
// may have several entries, and each edge endpoint keeps its own resolution.
// Missing times remain unknown. ResolvedID can still be an ip: identifier when
// the inventory cannot safely choose a workload.
type IdentityResolution struct {
	Input         string              `json:"input"`
	ResolvedID    string              `json:"resolvedId"`
	Method        IdentityMethod      `json:"method"`
	Source        string              `json:"source"`
	SourceType    string              `json:"sourceType"`
	Basis         string              `json:"basis,omitempty"`
	Detail        string              `json:"detail"`
	Candidates    []IdentityCandidate `json:"candidates,omitempty"`
	Alternatives  []string            `json:"alternatives,omitempty"`
	TimeBasis     string              `json:"timeBasis"`
	CollectedAt   time.Time           `json:"collectedAt,omitzero"`
	EvaluatedAt   time.Time           `json:"evaluatedAt,omitzero"`
	ObservedFrom  time.Time           `json:"observedFrom,omitzero"`
	ObservedAt    time.Time           `json:"observedAt,omitzero"`
	WindowSeconds float64             `json:"windowSeconds,omitempty"`
	Retained      bool                `json:"retained,omitempty"`
}

type ConnectionIdentity struct {
	From []IdentityResolution `json:"from"`
	To   []IdentityResolution `json:"to"`
}
