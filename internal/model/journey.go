package model

// JourneyContract is the engine's versioned ownership scope. It never claims
// that two calls belong to the same request or transaction.
type JourneyContract struct {
	Version  int            `json:"version"`
	Mode     string         `json:"mode"`
	Key      string         `json:"key"`
	MaxDepth int            `json:"maxDepth"`
	MaxNodes int            `json:"maxNodes"`
	Scopes   []JourneyScope `json:"scopes"`
}

type JourneyScope struct {
	Name          string            `json:"name"`
	Key           string            `json:"key"`
	Roles         map[string]string `json:"roles"`
	EntryPoints   []string          `json:"entryPoints"`
	EntryEdges    []string          `json:"entryEdges"`
	BoundaryPorts []JourneyBoundary `json:"boundaryPorts"`
	Health        Health            `json:"health"`
	Truncated     bool              `json:"truncated"`
}

// JourneyBoundary is one crossing connection. Interfaces may fold these into
// visual ports without changing which connections cross the scope.
type JourneyBoundary struct {
	Kind   string `json:"kind"` // caller, tenant, exit
	EdgeID string `json:"edgeId"`
	From   string `json:"from"`
	To     string `json:"to"`
}
