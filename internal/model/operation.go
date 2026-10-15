package model

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// OperationContract describes sampled span ancestry. It deliberately carries
// neither platform health nor an inferred request-rate denominator.
type OperationContract struct {
	Version      int                     `json:"version"`
	Mode         string                  `json:"mode"`
	Sampling     OperationSampling       `json:"sampling"`
	Sources      []OperationSource       `json:"sources"`
	Scopes       []OperationScope        `json:"scopes"`
	Unattributed []OperationUnattributed `json:"unattributed"`
}

type OperationSampling struct {
	Policy        string   `json:"policy"`
	TotalRequests *int64   `json:"totalRequests"`
	Coverage      *float64 `json:"coverage"`
}

type OperationPeriod struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type OperationSource struct {
	Source            string           `json:"source"`
	QueryPeriod       *OperationPeriod `json:"queryPeriod,omitempty"`
	CollectedAt       time.Time        `json:"collectedAt,omitzero"`
	RequestedTraces   int              `json:"requestedTraces"`
	FetchedTraces     int              `json:"fetchedTraces"`
	FailedTraces      int              `json:"failedTraces"`
	QueriedServices   int              `json:"queriedServices"`
	AvailableServices int              `json:"availableServices"`
	Truncated         bool             `json:"truncated"`
	// Status is sampled, partial or unavailable; in a slice, excluded when the
	// source cannot split its traces by the selected dimensions.
	Status string `json:"status"`
}

type OperationScope struct {
	ID                    string            `json:"id"`
	Source                string            `json:"source"`
	EntryService          string            `json:"entryService"`
	Operation             string            `json:"operation"`
	QueryPeriod           OperationPeriod   `json:"queryPeriod"`
	SampledRootCount      int               `json:"sampledRootCount"`
	IncompleteTraceCount  int               `json:"incompleteTraceCount"`
	SampleRootDurationsMs []float64         `json:"sampleRootDurationsMs"`
	RootOutcomes          OperationOutcomes `json:"rootOutcomes"`
	Nodes                 []OperationNode   `json:"nodes"`
	Edges                 []OperationEdge   `json:"edges"`
	Traces                []OperationTrace  `json:"traces"`
	Truncated             bool              `json:"truncated"`
}

type OperationOutcomes struct {
	OK      int `json:"ok"`
	Error   int `json:"error"`
	Unknown int `json:"unknown"`
}

type OperationNode struct {
	ID   string   `json:"id"`
	Kind NodeKind `json:"kind"`
}

type OperationEdge struct {
	From     string          `json:"from"`
	To       string          `json:"to"`
	TraceIDs []string        `json:"traceIds"`
	Calls    []OperationCall `json:"calls"`
}

type OperationCall struct {
	TraceID      string    `json:"traceId"`
	SpanID       string    `json:"spanId"`
	ParentSpanID string    `json:"parentSpanId,omitempty"`
	Attribution  string    `json:"attribution"` // parent-span or client-peer
	StartedAt    time.Time `json:"startedAt,omitzero"`
	EndedAt      time.Time `json:"endedAt,omitzero"`
	DurationMs   *float64  `json:"durationMs,omitempty"`
}

type OperationTrace struct {
	TraceID              string          `json:"traceId"`
	RootSpanID           string          `json:"rootSpanId"`
	RootDurationMs       *float64        `json:"rootDurationMs,omitempty"`
	RootOutcome          string          `json:"rootOutcome"`
	SpanIDs              []string        `json:"spanIds"`
	MissingParentSpanIDs []string        `json:"missingParentSpanIds"`
	UnattributedSpanIDs  []string        `json:"unattributedSpanIds"`
	Spans                []OperationSpan `json:"spans"`
	StructuralStatus     string          `json:"structuralStatus"` // linked-sample or gaps-detected, never complete
	Truncated            bool            `json:"truncated"`
	// Dimensions are the business dimension values the entry span carried,
	// itself or from its closest ancestor; a slice keeps the traces they match.
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

type OperationSpan struct {
	SpanID       string              `json:"spanId"`
	ParentSpanID string              `json:"parentSpanId,omitempty"`
	NodeID       string              `json:"nodeId"`
	Kind         string              `json:"kind"`
	StartedAt    time.Time           `json:"startedAt,omitzero"`
	EndedAt      time.Time           `json:"endedAt,omitzero"`
	Links        []OperationSpanLink `json:"links"`
	// Dimensions are this span's own business dimension values, or inherited
	// from its closest ancestor: those of the call it stands for.
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

type OperationSpanLink struct {
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
}

type OperationUnattributed struct {
	Source  string `json:"source"`
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
	NodeID  string `json:"nodeId"`
	Reason  string `json:"reason"`
	// Dimensions place the span in a slice, as for attributed spans.
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

func EmptyOperations() *OperationContract {
	return &OperationContract{Version: 1, Mode: "operation", Sampling: OperationSampling{Policy: "unknown"},
		Sources: []OperationSource{}, Scopes: []OperationScope{}, Unattributed: []OperationUnattributed{}}
}

func OperationID(source, service, operation string) string {
	h := sha256.Sum256([]byte(source + "\x00" + service + "\x00" + operation))
	return "op:" + hex.EncodeToString(h[:12])
}
