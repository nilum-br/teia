// Package advisor turns the merged graph into findings: where teia is
// blind, what permission is missing, and where intent and reality disagree.
// Every finding carries the concrete next step, so each round of fixes
// widens what the next discovery round can see.
package advisor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

// Input is everything a rule may look at.
type Input struct {
	Graph    *graph.Graph
	Statuses []model.SourceStatus
}

// hasSourceType reports whether a source of that type is configured.
func (in Input) hasSourceType(t ...string) bool {
	for _, s := range in.Statuses {
		for _, want := range t {
			if s.Type == want {
				return true
			}
		}
	}
	return false
}

// explored returns node IDs some source can look inside.
func (in Input) explored() map[string]bool {
	out := map[string]bool{}
	for _, s := range in.Statuses {
		for _, id := range s.Explores {
			out[id] = true
		}
	}
	return out
}

// Rule inspects the input and returns findings without IDs.
type Rule struct {
	Name  string
	Check func(Input) []model.Finding
}

// Rules is the default rule set, in no particular order.
var Rules = []Rule{
	{"source-error", sourceErrors},
	{"no-telemetry", noTelemetry},
	{"datastore-unexplored", datastoreUnexplored},
	{"unresolved-address", unresolvedAddress},
	{"undeclared-flow", undeclaredFlows},
	{"stale-flow", staleFlows},
	{"no-inventory", noInventory},
	{"unknown-callers", unknownCallers},
	{"unmatched-reading", unmatchedReadings},
}

// Advise runs every rule, adds the findings sources raised themselves,
// assigns stable IDs and sorts by urgency.
func Advise(in Input, fromSources []model.Finding) []model.Finding {
	all := append([]model.Finding{}, fromSources...)
	for _, r := range Rules {
		for _, f := range r.Check(in) {
			if f.Rule == "" {
				f.Rule = r.Name
			}
			all = append(all, f)
		}
	}
	for i := range all {
		all[i].ID = all[i].Rule + ":" + all[i].Subject
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() < b.Severity.Rank()
		}
		if a.Kind != b.Kind {
			return kindOrder[a.Kind] < kindOrder[b.Kind]
		}
		return a.ID < b.ID
	})
	return all
}

var kindOrder = map[model.FindingKind]int{
	model.FindingSource:     0,
	model.FindingPermission: 1,
	model.FindingBlindSpot:  2,
	model.FindingDrift:      3,
}

func sourceErrors(in Input) []model.Finding {
	var out []model.Finding
	for _, s := range in.Statuses {
		if s.State != model.SourceError {
			continue
		}
		out = append(out, model.Finding{
			Kind:     model.FindingSource,
			Severity: model.SeverityCritical,
			Subject:  s.Name,
			Title:    fmt.Sprintf("Source %s (%s) failed", s.Name, s.Type),
			Detail:   s.Message,
			Fix:      &model.Fix{Summary: "Check that teia can reach it and that the address in the config is right. Everything this source would show is missing from the graph until then."},
		})
	}
	return out
}

func isWorkload(n *model.Node) bool {
	if source.IsAddressID(n.ID) {
		return false
	}
	return n.Kind == model.KindService || n.Kind == model.KindUnknown
}

// onlyDeclared reports whether nothing but flows files knows about n, i.e.
// it may not even be deployed.
func (in Input) onlyDeclared(n model.Node) bool {
	types := map[string]string{}
	for _, s := range in.Statuses {
		types[s.Name] = s.Type
	}
	for _, src := range n.Sources {
		if types[src] != "declared" {
			return false
		}
	}
	return true
}

func noTelemetry(in Input) []model.Finding {
	var out []model.Finding
	for _, n := range in.Graph.SortedNodes() {
		if !isWorkload(&n) || n.Visibility == model.VisibilityFull {
			continue
		}
		if n.Visibility == model.VisibilityNone && in.onlyDeclared(n) {
			// Declared but never seen anywhere: stale-flow speaks for it.
			continue
		}
		f := model.Finding{
			Kind:     model.FindingBlindSpot,
			Severity: model.SeverityWarning,
			Subject:  n.ID,
			Title:    fmt.Sprintf("%s has no telemetry of its own", n.ID),
			Fix: &model.Fix{
				Summary:  "Instrument it with an OpenTelemetry SDK or agent pointing at your collector. If changing the app is not an option, Grafana Beyla (eBPF) instruments it without code changes.",
				Language: "shell",
				Snippet: fmt.Sprintf(`OTEL_SERVICE_NAME=%s
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
OTEL_TRACES_EXPORTER=otlp`, n.ID),
			},
		}
		if n.Visibility == model.VisibilityPartial {
			f.Detail = fmt.Sprintf("teia only knows about it through what others report (%s). Anything it calls that those sources cannot see is missing from the graph.", strings.Join(n.Sources, ", "))
		} else {
			f.Detail = fmt.Sprintf("It exists (reported by %s) but no traffic to or from it was seen.", strings.Join(n.Sources, ", "))
		}
		out = append(out, f)
	}
	return out
}

func datastoreUnexplored(in Input) []model.Finding {
	explored := in.explored()
	var out []model.Finding
	for _, n := range in.Graph.SortedNodes() {
		switch n.Kind {
		case model.KindDatabase, model.KindCache, model.KindQueue:
		default:
			continue
		}
		if explored[n.ID] {
			continue
		}
		callers := in.Graph.Callers(n.ID)
		system := protocolInto(in.Graph, n.ID)
		f := model.Finding{
			Kind:     model.FindingBlindSpot,
			Severity: model.SeverityInfo,
			Subject:  n.ID,
			Title:    fmt.Sprintf("Reached %s %s but cannot look inside it", n.Kind, n.ID),
			Detail: fmt.Sprintf("teia knows %s from its instrumented callers (%s). Clients without telemetry, such as batch jobs or legacy apps, stay invisible until teia can ask the %s itself who is connected.",
				n.ID, strings.Join(callers, ", "), n.Kind),
		}
		switch system {
		case "postgresql", "postgres":
			f.Fix = &model.Fix{
				Summary:  "Create a monitoring login for teia and add a postgres explorer for this node.",
				Language: "yaml",
				Snippet: fmt.Sprintf(`# in the database:
#   CREATE ROLE teia LOGIN PASSWORD 'change-me';
#   GRANT pg_monitor TO teia;
sources:
  postgres:
    - node: %s
      dsn: postgres://teia:${TEIA_PG_PASSWORD}@%s:5432/postgres`, n.ID, n.ID),
			}
		default:
			label := system
			if label == "" {
				label = "this kind of datastore"
			}
			f.Fix = &model.Fix{Summary: fmt.Sprintf("teia has no explorer for %s yet. Declare its known clients in their flows files meanwhile; an explorer is a welcome contribution.", label)}
		}
		out = append(out, f)
	}
	return out
}

func protocolInto(g *graph.Graph, id string) string {
	for _, e := range g.SortedEdges() {
		if e.To == id && e.Protocol != "" {
			return e.Protocol
		}
	}
	return ""
}

func unresolvedAddress(in Input) []model.Finding {
	inventory := in.hasSourceType("docker", "kubernetes")
	var out []model.Finding
	for _, n := range in.Graph.SortedNodes() {
		if !source.IsAddressID(n.ID) {
			continue
		}
		addr := strings.TrimPrefix(n.ID, source.AddressPrefix)
		var targets []string
		for _, e := range in.Graph.SortedEdges() {
			if e.From == n.ID {
				targets = append(targets, e.To)
			}
		}
		f := model.Finding{
			Kind:     model.FindingBlindSpot,
			Severity: model.SeverityWarning,
			Subject:  n.ID,
			Title:    fmt.Sprintf("Unknown client %s talks to %s", addr, strings.Join(targets, ", ")),
		}
		if inventory {
			f.Detail = "The address is not in any inventory source, so it is probably outside the platform teia watches (a VM, a laptop, another cluster)."
			f.Fix = &model.Fix{
				Summary:  "Name it with an alias, and declare it in a flows file if the communication is expected.",
				Language: "yaml",
				Snippet:  fmt.Sprintf("aliases:\n  %q: name-of-that-client", n.ID),
			}
		} else {
			f.Detail = "teia has no inventory source, so it cannot map addresses back to workloads."
			f.Fix = &model.Fix{
				Summary:  "Enable an inventory source, so addresses map back to workloads.",
				Language: "yaml",
				Snippet:  "sources:\n  kubernetes: {}   # or docker: {}",
			}
		}
		out = append(out, f)
	}
	return out
}

// declarable says whether a node can own a flows file.
func declarable(n *model.Node) bool {
	return n != nil && !source.IsAddressID(n.ID) && n.Kind != model.KindExternal
}

func undeclaredFlows(in Input) []model.Finding {
	byOwner := map[string][]model.Edge{}
	for _, e := range in.Graph.SortedEdges() {
		if e.State == model.EdgeGhost && declarable(in.Graph.Nodes[e.From]) {
			byOwner[e.From] = append(byOwner[e.From], e)
		}
	}
	owners := make([]string, 0, len(byOwner))
	for o := range byOwner {
		owners = append(owners, o)
	}
	sort.Strings(owners)

	var out []model.Finding
	for _, owner := range owners {
		edges := byOwner[owner]
		node := in.Graph.Nodes[owner]
		if node.Labels["teia.declared"] != "true" {
			out = append(out, model.Finding{
				Kind:     model.FindingDrift,
				Severity: model.SeverityWarning,
				Subject:  owner,
				Title:    fmt.Sprintf("%s has no flows file", owner),
				Detail:   fmt.Sprintf("teia observed %d outgoing communication(s) from %s and none is declared. The file below was generated from what was observed; review the reasons and commit it to the %s repository.", len(edges), owner, owner),
				Fix: &model.Fix{
					Summary:  "Commit a flows file generated from observed traffic.",
					Language: "yaml",
					Snippet:  FlowsFile(in.Graph, owner, edges),
				},
			})
			continue
		}
		for _, e := range edges {
			out = append(out, model.Finding{
				Kind:     model.FindingDrift,
				Severity: model.SeverityWarning,
				Subject:  e.Key(),
				Title:    fmt.Sprintf("%s calls %s but it is not declared", e.From, e.To),
				Detail:   "Either the flows file is out of date or this communication was never meant to exist. Both are worth knowing.",
				Fix: &model.Fix{
					Summary:  fmt.Sprintf("If it is expected, add it to %s's flows file.", e.From),
					Language: "yaml",
					Snippet:  egressEntry(in.Graph, e),
				},
			})
		}
	}
	return out
}

func staleFlows(in Input) []model.Finding {
	var out []model.Finding
	for _, e := range in.Graph.SortedEdges() {
		if e.State != model.EdgeStale {
			continue
		}
		from := in.Graph.Nodes[e.From]
		if from == nil || from.Visibility != model.VisibilityFull {
			// We cannot tell silence from blindness; no-telemetry covers it.
			continue
		}
		out = append(out, model.Finding{
			Kind:     model.FindingDrift,
			Severity: model.SeverityInfo,
			Subject:  e.Key(),
			Title:    fmt.Sprintf("%s declares a call to %s that was not observed", e.From, e.To),
			Detail:   fmt.Sprintf("%s reports its own telemetry and made no call to %s in the discovery window.", e.From, e.To),
			Fix:      &model.Fix{Summary: "Remove it from the flows file if the dependency is gone. If it only happens rarely (a monthly batch, a fallback), keep it: teia will show it as stale until it runs."},
		})
	}
	return out
}

func noInventory(in Input) []model.Finding {
	if in.hasSourceType("docker", "kubernetes") {
		return nil
	}
	return []model.Finding{{
		Kind:     model.FindingBlindSpot,
		Severity: model.SeverityInfo,
		Subject:  "inventory",
		Title:    "No inventory source configured",
		Detail:   "Without an inventory teia only knows workloads that produce traffic data, and cannot turn IP addresses into names.",
		Fix: &model.Fix{
			Summary:  "Enable an inventory: Kubernetes (read-only list of pods and services) or Docker.",
			Language: "yaml",
			Snippet:  "sources:\n  kubernetes: {}   # in-cluster or current kubeconfig context\n  # docker: {}     # DOCKER_HOST or the OS default",
		},
	}}
}

var signalWording = map[string]string{
	source.SignalTraces:  "callers that emit no traces",
	source.SignalMesh:    "callers without a mesh proxy",
	source.SignalNetwork: "callers the network telemetry cannot name",
}

func unknownCallers(in Input) []model.Finding {
	var ids []string
	for id := range in.Graph.UnknownCallers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []model.Finding
	for _, id := range ids {
		var signals []string
		for sig := range in.Graph.UnknownCallers[id] {
			signals = append(signals, sig)
		}
		sort.Strings(signals)
		for _, sig := range signals {
			u := in.Graph.UnknownCallers[id][sig]
			if explainedBy(in.Graph, id, sig) {
				continue
			}
			wording := signalWording[sig]
			if wording == "" {
				wording = "callers it cannot identify"
			}
			out = append(out, model.Finding{
				Kind:     model.FindingBlindSpot,
				Severity: model.SeverityWarning,
				Subject:  id,
				Title:    fmt.Sprintf("%s receives calls from callers teia cannot identify", id),
				Detail: fmt.Sprintf("%.2f req/s reach %s from %s (seen by %s), and no other source names them. "+
					"They could be another team's job, a partner integration or a person with curl.", u.Rate, id, wording, strings.Join(u.Sources, ", ")),
				Fix: &model.Fix{
					Summary: "Name the callers from what " + id + " already records: its access logs (in Loki, via a query source), " +
						"a mesh proxy, or Beyla (eBPF) on its host. Any of them is enough.",
					Language: "yaml",
					Snippet: `sources:
  queries:
    - name: access-logs
      dialect: loki
      url: http://loki:3100
      query: sum by (service, remote_addr) (rate({service="` + id + `"} | json [$window]))
      from: remote_addr
      fromAddress: true
      to: service
      protocol: http`,
				},
			})
		}
	}
	return out
}

// explainedBy reports whether some observed caller of id is invisible to
// signal, and so may be the one that signal reported as unknown. A caller
// with an SDK explains the mesh's "unknown"; it does not explain the
// "user" of the service graph.
func explainedBy(g *graph.Graph, id, signal string) bool {
	for _, e := range g.Edges {
		if e.To != id || !e.Observed || e.From == id {
			continue
		}
		if from := g.Nodes[e.From]; from != nil && from.Labels[source.SignalLabel(signal)] != "true" {
			return true
		}
	}
	return false
}

// unmatchedReadings explains exporter readings that fit no node: their
// labels guess at a name the graph does not use, so an alias fixes it.
func unmatchedReadings(in Input) []model.Finding {
	var out []model.Finding
	seen := map[string]bool{}
	for _, u := range in.Graph.Unmatched {
		if len(u.Candidates) == 0 {
			continue
		}
		subject := u.Candidates[0]
		if len(u.To) > 0 {
			subject += "->" + u.To[0]
		}
		if seen[subject] {
			continue // one finding per thing measured, not per reading
		}
		seen[subject] = true
		what := "no node is called " + strings.Join(u.Candidates, ", ")
		if len(u.To) > 0 {
			what = fmt.Sprintf("no edge goes from %s to %s", strings.Join(u.Candidates, " or "), strings.Join(u.To, " or "))
		}
		out = append(out, model.Finding{
			Kind:     model.FindingSource,
			Severity: model.SeverityInfo,
			Subject:  subject,
			Title:    fmt.Sprintf("Health readings from %s fit nothing on the map", u.Origin),
			Detail: fmt.Sprintf("%s reads them, but %s, so their saturation, backlog or locks are not shown. "+
				"Exporters label metrics with their own names (the exporter, its job, its address).", u.Source, what),
			Fix: &model.Fix{
				Summary:  "Tell teia which node they measure with an alias.",
				Language: "yaml",
				Snippet:  fmt.Sprintf("aliases:\n  %q: the-node-it-measures", u.Candidates[0]),
			},
		})
	}
	return out
}
