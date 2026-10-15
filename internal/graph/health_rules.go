package graph

import (
	"slices"
	"strings"

	"github.com/nilum-br/teia/internal/model"
)

// HealthOverride is validated configuration, with omitted limits already
// inherited from the global pair. Selection is independent for each cause.
type HealthOverride struct {
	Name, Target, ID             string
	Priority                     int
	Labels, FromLabels, ToLabels map[string]string
	Causes                       []model.Cause
	Limits                       HealthThresholds
}

var healthCauses = []model.Cause{model.CauseErrors, model.CauseLatency, model.CauseSaturation,
	model.CauseLocks, model.CauseBacklog, model.CauseLag, model.CauseRestarts, model.CauseEvictions, model.CauseRejections}

// LimitsFor names the units used by the health judge, including normalized
// saturation shares rather than the original byte/count capacity readings.
func (t HealthThresholds) LimitsFor(c model.Cause) (degraded, failing float64, unit string) {
	switch c {
	case model.CauseErrors:
		return t.Degraded, t.Failing, model.UnitShare
	case model.CauseLatency:
		return t.LatencyDegradedMs, t.LatencyFailingMs, "milliseconds"
	case model.CauseSaturation:
		return t.SaturationDegraded, t.SaturationFailing, model.UnitShare
	case model.CauseLocks:
		return t.LocksDegraded, t.LocksFailing, "count"
	case model.CauseBacklog:
		return t.BacklogDegraded, t.BacklogFailing, "count"
	case model.CauseLag:
		return t.LagDegradedSeconds, t.LagFailingSeconds, model.UnitSeconds
	case model.CauseRestarts:
		return t.RestartsDegraded, t.RestartsFailing, "count"
	case model.CauseEvictions:
		return t.EvictionsDegraded, t.EvictionsFailing, "count"
	case model.CauseRejections:
		return t.RejectionsDegraded, t.RejectionsFailing, "count"
	}
	return 0, 0, ""
}

func (t *HealthThresholds) setLimits(c model.Cause, degraded, failing float64) {
	switch c {
	case model.CauseErrors:
		t.Degraded, t.Failing = degraded, failing
	case model.CauseLatency:
		t.LatencyDegradedMs, t.LatencyFailingMs = degraded, failing
	case model.CauseSaturation:
		t.SaturationDegraded, t.SaturationFailing = degraded, failing
	case model.CauseLocks:
		t.LocksDegraded, t.LocksFailing = degraded, failing
	case model.CauseBacklog:
		t.BacklogDegraded, t.BacklogFailing = degraded, failing
	case model.CauseLag:
		t.LagDegradedSeconds, t.LagFailingSeconds = degraded, failing
	case model.CauseRestarts:
		t.RestartsDegraded, t.RestartsFailing = degraded, failing
	case model.CauseEvictions:
		t.EvictionsDegraded, t.EvictionsFailing = degraded, failing
	case model.CauseRejections:
		t.RejectionsDegraded, t.RejectionsFailing = degraded, failing
	}
}

func matchesHealthLabels(want map[string]string, n *model.Node) bool {
	if n == nil {
		return len(want) == 0
	}
	for key, value := range want {
		if got, exists := n.Labels[key]; !exists || got != value {
			return false
		}
	}
	return true
}

// globalRules is the rule of every cause under the global thresholds, in
// healthCauses order. Its capacity is its length, so an append never writes
// into it: components and connections without an override share it.
func globalRules(global HealthThresholds) []model.HealthRule {
	rules := make([]model.HealthRule, 0, len(healthCauses))
	for _, cause := range healthCauses {
		d, f, unit := global.LimitsFor(cause)
		rules = append(rules, model.HealthRule{Cause: cause, Name: "global", Match: "global", Degraded: d, Failing: f, Unit: unit})
	}
	return slices.Clip(rules)
}

// resolveHealth picks each cause's limits for a node or an edge: the best
// matching override, else the global thresholds. Without any override it
// returns shared (globalRules) itself rather than a copy per subject.
func resolveHealth(g *Graph, global HealthThresholds, overrides []HealthOverride, shared []model.HealthRule, node *model.Node, edge *model.Edge) (HealthThresholds, []model.HealthRule) {
	limits := global
	var rules []model.HealthRule
	for i, cause := range healthCauses {
		var chosen *HealthOverride
		for j := range overrides {
			o := &overrides[j]
			if !slices.Contains(o.Causes, cause) {
				continue
			}
			matches := false
			if node != nil && o.Target == "node" {
				matches = (o.ID == "" || o.ID == node.ID) && matchesHealthLabels(o.Labels, node)
			} else if edge != nil && o.Target == "edge" {
				matches = (o.ID == "" || o.ID == edge.Key()) && matchesHealthLabels(o.FromLabels, g.Nodes[edge.From]) && matchesHealthLabels(o.ToLabels, g.Nodes[edge.To])
			}
			if !matches {
				continue
			}
			if chosen == nil || (o.ID != "" && chosen.ID == "") ||
				((o.ID == "") == (chosen.ID == "") && (o.Priority > chosen.Priority || (o.Priority == chosen.Priority && o.Name < chosen.Name))) {
				chosen = o
			}
		}
		if chosen == nil {
			continue
		}
		if rules == nil {
			rules = slices.Clone(shared)
		}
		rule := &rules[i]
		rule.Name, rule.Priority, rule.Match = chosen.Name, chosen.Priority, "selector"
		if chosen.ID != "" {
			rule.Match = "identity"
		}
		rule.Degraded, rule.Failing, _ = chosen.Limits.LimitsFor(cause)
		limits.setLimits(cause, rule.Degraded, rule.Failing)
	}
	if rules == nil {
		return limits, shared
	}
	return limits, rules
}

func annotateHealth(reasons []model.Reason, rules []model.HealthRule, evidence []model.Evidence) {
	for i := range reasons {
		r := &reasons[i]
		for _, rule := range rules {
			if rule.Cause == r.Cause {
				r.Rule, r.Unit = rule.Name, rule.Unit
				break
			}
		}
		metric := string(r.Cause)
		if r.Cause == model.CauseErrors {
			metric = "errorRate"
		}
		for _, ev := range evidence {
			if ev.Retained {
				continue
			}
			for _, m := range ev.Metrics {
				if m == metric || (r.What != "" && m == metric+": "+r.What) || (r.What == "" && strings.HasPrefix(m, metric+": ")) {
					r.Evidence = append(r.Evidence, ev)
					break
				}
			}
		}
	}
}
