package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// Flat, table-shaped views of the snapshot for Grafana. Each row is a flat
// object, so any JSON data source (Infinity, or the teia data source later)
// turns them into data frames:
//
//	nodes, edges     the Node Graph field names plus teia's own fields, read
//	                 by the teia panel (nilum-teia-panel) and still usable by
//	                 Grafana's built-in Node Graph panel
//	findings, sources, changes
//	                 everything else the teia panel shows; coverage is a
//	                 single row already, so the panel reads /api/v1/coverage

// Colours follow the UI: state is carried by colour and dash.
var (
	healthColor = map[model.Health]string{
		model.HealthHealthy:  "#22c55e",
		model.HealthDegraded: "#f5a524",
		model.HealthFailing:  "#ef4444",
		model.HealthUnknown:  "#64748b",
	}
	stateDash = map[model.EdgeState]string{
		model.EdgeGhost: "8 6",
		model.EdgeStale: "2 6",
	}
)

type grafanaNode struct {
	Operations         string    `json:"operations,omitempty"` // JSON operation contract on first row
	Slice              string    `json:"slice,omitempty"`      // JSON slice scope on first row, when sliced
	Dimensions         string    `json:"dimensions,omitempty"` // JSON dimension values seen, on first row
	Identity           string    `json:"identity"`             // JSON array of identity resolutions
	HistoryID          string    `json:"historyId,omitempty"`  // every historical row identifies its capture
	History            string    `json:"history,omitempty"`    // JSON replay metadata on first node row
	Links              string    `json:"links"`
	TimeMode           string    `json:"timeMode"`
	SnapshotAt         time.Time `json:"snapshotAt"`
	Evidence           string    `json:"evidence"`
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Subtitle           string    `json:"subtitle"`
	MainStat           string    `json:"mainstat"`
	SecondaryStat      string    `json:"secondarystat"`
	Color              string    `json:"color"`
	Kind               string    `json:"kind"`
	Tech               string    `json:"tech"`
	Health             string    `json:"health"`
	Visibility         string    `json:"visibility"`
	RequestRate        float64   `json:"requestRate"`
	ErrorRate          float64   `json:"errorRate"`
	ErrorsUnknown      bool      `json:"errorsUnknown"`
	ErrorCoverage      float64   `json:"errorCoverage"`
	LatencyP50Ms       float64   `json:"latencyP50Ms"`
	LatencyP95Ms       float64   `json:"latencyP95Ms"`
	LatencyApproximate bool      `json:"latencyApproximate"`
	LatencyCoverage    float64   `json:"latencyCoverage"`
	TrafficDirection   string    `json:"trafficDirection"`
	Reasons            string    `json:"reasons"` // JSON array of model.Reason
	HealthRules        string    `json:"healthRules"`
	Measures           string    `json:"measures"` // JSON array of model.Measure
	DetailWhy          string    `json:"detail__why"`
	Sources            string    `json:"sources"`
	Addresses          string    `json:"addresses"`
	Labels             string    `json:"labels"`                    // JSON object
	Journeys           string    `json:"journeys"`                  // JSON array of explicit memberships
	JourneyContract    string    `json:"journeyContract,omitempty"` // JSON object on the first node row only
	Findings           int       `json:"findings"`
	DetailSources      string    `json:"detail__sources"`
}

type grafanaEdge struct {
	Identity                 string    `json:"identity"` // JSON object with from/to resolutions
	HistoryID                string    `json:"historyId,omitempty"`
	ExpectedFrequencySeconds float64   `json:"expectedFrequencySeconds"`
	Frequency                string    `json:"frequency"`
	Links                    string    `json:"links"`
	TimeMode                 string    `json:"timeMode"`
	SnapshotAt               time.Time `json:"snapshotAt"`
	Evidence                 string    `json:"evidence"`
	ID                       string    `json:"id"`
	Source                   string    `json:"source"`
	Target                   string    `json:"target"`
	MainStat                 string    `json:"mainstat"`
	SecondaryStat            string    `json:"secondarystat"`
	Color                    string    `json:"color"`
	StrokeDasharray          string    `json:"strokeDasharray"`
	Thickness                float64   `json:"thickness"`
	State                    string    `json:"state"`
	Health                   string    `json:"health"`
	Protocol                 string    `json:"protocol"`
	Port                     int       `json:"port"`
	Reason                   string    `json:"reason"`
	Declared                 bool      `json:"declared"`
	Observed                 bool      `json:"observed"`
	RequestRate              float64   `json:"requestRate"`
	ErrorRate                float64   `json:"errorRate"`
	ErrorsUnknown            bool      `json:"errorsUnknown"`
	LatencyP50Ms             float64   `json:"latencyP50Ms"`
	LatencyP95Ms             float64   `json:"latencyP95Ms"`
	LatencyApproximate       bool      `json:"latencyApproximate"`
	Reasons                  string    `json:"reasons"` // JSON array of model.Reason
	HealthRules              string    `json:"healthRules"`
	Measures                 string    `json:"measures"` // JSON array of model.Measure
	DetailWhy                string    `json:"detail__why"`
	Signals                  string    `json:"signals"`
	Sources                  string    `json:"sources"`
	DetailState              string    `json:"detail__state"`
	DetailSources            string    `json:"detail__sources"`
}

func grafanaNodes(s model.Snapshot) []grafanaNode {
	findings := map[string]int{}
	for _, f := range s.Findings {
		findings[f.Subject]++
	}
	out := make([]grafanaNode, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		labels, _ := json.Marshal(n.Labels)
		if n.Labels == nil {
			labels = []byte("{}")
		}
		secondary := ""
		if c := findings[n.ID]; c > 0 {
			secondary = fmt.Sprintf("%d finding(s)", c)
		}
		main := string(n.Visibility)
		if n.RequestRate > 0 {
			main = fmt.Sprintf("%.2f req/s", n.RequestRate)
		}
		out = append(out, grafanaNode{
			Identity:  jsonList(n.Identity),
			HistoryID: frameHistoryID(s),
			Links:     jsonList(n.Links),
			TimeMode:  frameTimeMode(s), SnapshotAt: s.GeneratedAt, Evidence: jsonList(n.Evidence),
			ID: n.ID, Title: n.ID, Subtitle: subtitle(n), MainStat: main, SecondaryStat: secondary,
			Color: healthColor[n.Health], Kind: string(n.Kind), Tech: n.Tech, Health: string(n.Health), Visibility: string(n.Visibility),
			RequestRate: n.RequestRate, ErrorRate: n.ErrorRate, ErrorsUnknown: n.ErrorsUnknown, LatencyP50Ms: n.LatencyP50Ms, LatencyP95Ms: n.LatencyP95Ms,
			ErrorCoverage:      n.ErrorCoverage,
			LatencyApproximate: n.LatencyApproximate, LatencyCoverage: n.LatencyCoverage, TrafficDirection: n.TrafficDirection,
			Reasons: jsonList(n.Reasons), HealthRules: jsonList(n.HealthRules), Measures: jsonList(n.Measures), DetailWhy: whyText(n.Reasons),
			Sources: strings.Join(n.Sources, ","), Addresses: strings.Join(n.Addresses, ","), Labels: string(labels),
			Journeys: jsonList(n.Journeys),
			Findings: findings[n.ID], DetailSources: strings.Join(n.Sources, ", "),
		})
	}
	if len(out) > 0 && s.Journeys != nil {
		contract, _ := json.Marshal(s.Journeys)
		out[0].JourneyContract = string(contract)
	}
	if len(out) > 0 && s.Operations != nil {
		contract, _ := json.Marshal(s.Operations)
		out[0].Operations = string(contract)
	}
	if len(out) > 0 && s.History != nil {
		metadata, _ := json.Marshal(s.History)
		out[0].History = string(metadata)
	}
	if len(out) > 0 {
		out[0].Slice, out[0].Dimensions = sliceMetadata(s)
	}
	return out
}

func grafanaEdges(s model.Snapshot) []grafanaEdge {
	out := make([]grafanaEdge, 0, len(s.Edges))
	for _, e := range s.Edges {
		frequency, _ := json.Marshal(e.Frequency)
		identity, _ := json.Marshal(e.Identity)
		ge := grafanaEdge{
			Identity:                 string(identity),
			HistoryID:                frameHistoryID(s),
			ExpectedFrequencySeconds: e.ExpectedFrequencySeconds, Frequency: string(frequency),
			Links:    jsonList(e.Links),
			TimeMode: frameTimeMode(s), SnapshotAt: s.GeneratedAt, Evidence: jsonList(e.Evidence),
			ID: e.Key(), Source: e.From, Target: e.To, MainStat: string(e.State),
			Color: healthColor[e.Health], StrokeDasharray: stateDash[e.State], Thickness: 1,
			State: string(e.State), Health: string(e.Health), Protocol: e.Protocol, Port: e.Port, Reason: e.Reason,
			Declared: e.Declared, Observed: e.Observed, RequestRate: e.RequestRate, ErrorRate: e.ErrorRate, ErrorsUnknown: e.ErrorsUnknown,
			LatencyP50Ms: e.LatencyP50Ms, LatencyP95Ms: e.LatencyP95Ms,
			LatencyApproximate: e.LatencyApproximate,
			Reasons:            jsonList(e.Reasons), HealthRules: jsonList(e.HealthRules), Measures: jsonList(e.Measures), DetailWhy: whyText(e.Reasons),
			Signals: strings.Join(e.Signals, ","), Sources: strings.Join(e.Sources, ","),
			DetailState: string(e.State), DetailSources: strings.Join(e.Sources, ", "),
		}
		if e.RequestRate > 0 {
			ge.MainStat = fmt.Sprintf("%.2f req/s", e.RequestRate)
			ge.SecondaryStat = fmt.Sprintf("%.1f%% errors", e.ErrorRate*100)
			if e.ErrorsUnknown {
				ge.SecondaryStat = "failures not measured"
			}
			ge.Thickness = 2
		}
		if e.LatencyP95Ms > 0 {
			label := "p95 "
			if e.LatencyApproximate {
				label = "p95 approximation "
			}
			ge.SecondaryStat = strings.TrimPrefix(ge.SecondaryStat+" · "+label+model.FormatMs(e.LatencyP95Ms), " · ")
		}
		if e.State == model.EdgeStale {
			ge.Color = healthColor[model.HealthUnknown]
		}
		out = append(out, ge)
	}
	return out
}

// Every historical frame row identifies its original capture. Auxiliary rows
// participate too: mixing findings or coverage from another replay is not one
// coherent historical snapshot.
type grafanaTemporal struct {
	TimeMode   string    `json:"timeMode"`
	HistoryID  string    `json:"historyId,omitempty"`
	SnapshotAt time.Time `json:"snapshotAt"`
}

func frameMetadata(s model.Snapshot) grafanaTemporal {
	return grafanaTemporal{TimeMode: frameTimeMode(s), HistoryID: frameHistoryID(s), SnapshotAt: s.GeneratedAt}
}

type grafanaFinding struct {
	grafanaTemporal
	ID          string `json:"id"`
	Rule        string `json:"rule"`
	Severity    string `json:"severity"`
	Kind        string `json:"kind"`
	Subject     string `json:"subject"`
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	NextStep    string `json:"nextStep"`
	FixSnippet  string `json:"fixSnippet"`
	FixLanguage string `json:"fixLanguage"`
}

func grafanaFindings(s model.Snapshot) []grafanaFinding {
	out := make([]grafanaFinding, 0, len(s.Findings))
	for _, f := range s.Findings {
		g := grafanaFinding{
			grafanaTemporal: frameMetadata(s),
			ID:              f.ID, Rule: f.Rule, Severity: string(f.Severity), Kind: string(f.Kind),
			Subject: f.Subject, Title: f.Title, Detail: f.Detail,
		}
		if f.Fix != nil {
			g.NextStep, g.FixSnippet, g.FixLanguage = f.Fix.Summary, f.Fix.Snippet, f.Fix.Language
		}
		out = append(out, g)
	}
	return out
}

type grafanaSource struct {
	grafanaTemporal
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	State         string    `json:"state"`
	Message       string    `json:"message"`
	Nodes         int       `json:"nodes"`
	Edges         int       `json:"edges"`
	LastSuccessAt time.Time `json:"lastSuccessAt,omitzero"`
	Retained      bool      `json:"retained"`
}

func grafanaSources(s model.Snapshot) []grafanaSource {
	out := make([]grafanaSource, 0, len(s.Sources))
	for _, src := range s.Sources {
		out = append(out, grafanaSource{grafanaTemporal: frameMetadata(s), Name: src.Name, Type: src.Type, State: string(src.State), Message: src.Message, Nodes: src.Nodes, Edges: src.Edges, LastSuccessAt: src.LastSuccessAt, Retained: src.Retained})
	}
	return out
}

type grafanaCoverage struct {
	model.Coverage
	grafanaTemporal
	History    string `json:"history,omitempty"`
	Operations string `json:"operations,omitempty"`
	Slice      string `json:"slice,omitempty"`
	Dimensions string `json:"dimensions,omitempty"`
}

func grafanaCoverageOf(s model.Snapshot) grafanaCoverage {
	row := grafanaCoverage{Coverage: s.Coverage, grafanaTemporal: frameMetadata(s)}
	if s.Operations != nil {
		contract, _ := json.Marshal(s.Operations)
		row.Operations = string(contract)
	}
	if s.History != nil {
		metadata, _ := json.Marshal(s.History)
		row.History = string(metadata)
	}
	row.Slice, row.Dimensions = sliceMetadata(s)
	return row
}

// sliceMetadata tells a panel fed by frames which slice they hold, if any, and
// which dimension values it can offer: frames alone cannot say they were sliced.
func sliceMetadata(s model.Snapshot) (slice, dimensions string) {
	if s.Slice != nil {
		scope, _ := json.Marshal(s.Slice)
		slice = string(scope)
	}
	if len(s.Dimensions) > 0 {
		values, _ := json.Marshal(s.Dimensions)
		dimensions = string(values)
	}
	return slice, dimensions
}

type grafanaChange struct {
	model.Change
	grafanaTemporal
}

func historicalChanges(s model.Snapshot) []grafanaChange {
	out := make([]grafanaChange, 0, len(s.Changes))
	for _, change := range s.Changes {
		out = append(out, grafanaChange{Change: change, grafanaTemporal: frameMetadata(s)})
	}
	return out
}

func frameTimeMode(s model.Snapshot) string {
	if s.TimeMode == "historical" {
		return "historical"
	}
	return "current"
}

func frameHistoryID(s model.Snapshot) string {
	if s.TimeMode == "historical" && s.History != nil {
		return s.History.ID
	}
	return ""
}

// jsonList encodes a list for a frame cell; "[]" when empty.
func jsonList[T any](list []T) string {
	if len(list) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(list) // slices of plain structs always encode
	return string(b)
}

// whyText is the reasons as one line, for Grafana's Node Graph details.
func whyText(reasons []model.Reason) string {
	parts := make([]string, len(reasons))
	for i, r := range reasons {
		parts[i] = r.Summary
	}
	return strings.Join(parts, "; ")
}

func subtitle(n model.Node) string {
	if n.Tech != "" {
		return n.Tech
	}
	return string(n.Kind)
}
