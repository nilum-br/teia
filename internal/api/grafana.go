package api

import (
	"encoding/json"
	"fmt"
	"strings"

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
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Subtitle      string  `json:"subtitle"`
	MainStat      string  `json:"mainstat"`
	SecondaryStat string  `json:"secondarystat"`
	Color         string  `json:"color"`
	Kind          string  `json:"kind"`
	Tech          string  `json:"tech"`
	Health        string  `json:"health"`
	Visibility    string  `json:"visibility"`
	RequestRate   float64 `json:"requestRate"`
	ErrorRate     float64 `json:"errorRate"`
	LatencyP50Ms  float64 `json:"latencyP50Ms"`
	LatencyP95Ms  float64 `json:"latencyP95Ms"`
	Reasons       string  `json:"reasons"`  // JSON array of model.Reason
	Measures      string  `json:"measures"` // JSON array of model.Measure
	DetailWhy     string  `json:"detail__why"`
	Sources       string  `json:"sources"`
	Addresses     string  `json:"addresses"`
	Labels        string  `json:"labels"` // JSON object
	Findings      int     `json:"findings"`
	DetailSources string  `json:"detail__sources"`
}

type grafanaEdge struct {
	ID              string  `json:"id"`
	Source          string  `json:"source"`
	Target          string  `json:"target"`
	MainStat        string  `json:"mainstat"`
	SecondaryStat   string  `json:"secondarystat"`
	Color           string  `json:"color"`
	StrokeDasharray string  `json:"strokeDasharray"`
	Thickness       float64 `json:"thickness"`
	State           string  `json:"state"`
	Health          string  `json:"health"`
	Protocol        string  `json:"protocol"`
	Port            int     `json:"port"`
	Reason          string  `json:"reason"`
	Declared        bool    `json:"declared"`
	Observed        bool    `json:"observed"`
	RequestRate     float64 `json:"requestRate"`
	ErrorRate       float64 `json:"errorRate"`
	LatencyP50Ms    float64 `json:"latencyP50Ms"`
	LatencyP95Ms    float64 `json:"latencyP95Ms"`
	Reasons         string  `json:"reasons"`  // JSON array of model.Reason
	Measures        string  `json:"measures"` // JSON array of model.Measure
	DetailWhy       string  `json:"detail__why"`
	Signals         string  `json:"signals"`
	Sources         string  `json:"sources"`
	DetailState     string  `json:"detail__state"`
	DetailSources   string  `json:"detail__sources"`
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
			ID: n.ID, Title: n.ID, Subtitle: subtitle(n), MainStat: main, SecondaryStat: secondary,
			Color: healthColor[n.Health], Kind: string(n.Kind), Tech: n.Tech, Health: string(n.Health), Visibility: string(n.Visibility),
			RequestRate: n.RequestRate, ErrorRate: n.ErrorRate, LatencyP50Ms: n.LatencyP50Ms, LatencyP95Ms: n.LatencyP95Ms,
			Reasons: jsonList(n.Reasons), Measures: jsonList(n.Measures), DetailWhy: whyText(n.Reasons),
			Sources: strings.Join(n.Sources, ","), Addresses: strings.Join(n.Addresses, ","), Labels: string(labels),
			Findings: findings[n.ID], DetailSources: strings.Join(n.Sources, ", "),
		})
	}
	return out
}

func grafanaEdges(s model.Snapshot) []grafanaEdge {
	out := make([]grafanaEdge, 0, len(s.Edges))
	for _, e := range s.Edges {
		ge := grafanaEdge{
			ID: e.Key(), Source: e.From, Target: e.To, MainStat: string(e.State),
			Color: healthColor[e.Health], StrokeDasharray: stateDash[e.State], Thickness: 1,
			State: string(e.State), Health: string(e.Health), Protocol: e.Protocol, Port: e.Port, Reason: e.Reason,
			Declared: e.Declared, Observed: e.Observed, RequestRate: e.RequestRate, ErrorRate: e.ErrorRate,
			LatencyP50Ms: e.LatencyP50Ms, LatencyP95Ms: e.LatencyP95Ms,
			Reasons: jsonList(e.Reasons), Measures: jsonList(e.Measures), DetailWhy: whyText(e.Reasons),
			Signals: strings.Join(e.Signals, ","), Sources: strings.Join(e.Sources, ","),
			DetailState: string(e.State), DetailSources: strings.Join(e.Sources, ", "),
		}
		if e.RequestRate > 0 {
			ge.MainStat = fmt.Sprintf("%.2f req/s", e.RequestRate)
			ge.SecondaryStat = fmt.Sprintf("%.1f%% errors", e.ErrorRate*100)
			ge.Thickness = 2
		}
		if e.LatencyP95Ms > 0 {
			ge.SecondaryStat = strings.TrimPrefix(ge.SecondaryStat+" · p95 "+model.FormatMs(e.LatencyP95Ms), " · ")
		}
		if e.State == model.EdgeStale {
			ge.Color = healthColor[model.HealthUnknown]
		}
		out = append(out, ge)
	}
	return out
}

type grafanaFinding struct {
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
			ID: f.ID, Rule: f.Rule, Severity: string(f.Severity), Kind: string(f.Kind),
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
	Name    string `json:"name"`
	Type    string `json:"type"`
	State   string `json:"state"`
	Message string `json:"message"`
	Nodes   int    `json:"nodes"`
	Edges   int    `json:"edges"`
}

func grafanaSources(s model.Snapshot) []grafanaSource {
	out := make([]grafanaSource, 0, len(s.Sources))
	for _, src := range s.Sources {
		out = append(out, grafanaSource{Name: src.Name, Type: src.Type, State: string(src.State), Message: src.Message, Nodes: src.Nodes, Edges: src.Edges})
	}
	return out
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
