package config

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
)

// HealthOverride matches one entity class. IDs are the canonical API identities;
// selectors use exact, case-sensitive label equality with all keys required.
type HealthOverride struct {
	Name       string            `yaml:"name"`
	Target     string            `yaml:"target"`
	ID         string            `yaml:"id"`
	Priority   int               `yaml:"priority"`
	Labels     map[string]string `yaml:"labels"`
	FromLabels map[string]string `yaml:"fromLabels"`
	ToLabels   map[string]string `yaml:"toLabels"`
	Limits     HealthLimits      `yaml:"limits"`
}

// HealthOverrides compiles validated settings without depending on YAML order.
// Each configured signal inherits only from global limits, never another match.
func (c *Config) HealthOverrides() []graph.HealthOverride {
	out := make([]graph.HealthOverride, 0, len(c.Health.Overrides))
	for _, o := range c.Health.Overrides {
		out = append(out, graph.HealthOverride{Name: o.Name, Target: o.Target, ID: o.ID, Priority: o.Priority,
			Labels: o.Labels, FromLabels: o.FromLabels, ToLabels: o.ToLabels,
			Causes: healthLimitCauses(o.Limits), Limits: healthThresholds(o.Limits, c.HealthThresholds())})
	}
	return out
}

func healthLimitCauses(h HealthLimits) []model.Cause {
	raw := healthThresholds(h, graph.HealthThresholds{})
	var causes []model.Cause
	for _, cause := range []model.Cause{model.CauseErrors, model.CauseLatency, model.CauseSaturation,
		model.CauseLocks, model.CauseBacklog, model.CauseLag, model.CauseRestarts, model.CauseEvictions, model.CauseRejections} {
		d, f, _ := raw.LimitsFor(cause)
		if d > 0 || f > 0 {
			causes = append(causes, cause)
		}
	}
	return causes
}

func healthValues(h HealthLimits) []struct {
	key                        string
	degraded, failing, maximum float64
} {
	return []struct {
		key                        string
		degraded, failing, maximum float64
	}{
		{"errors", h.Degraded, h.Failing, 1},
		{"latency", float64(h.Latency.Degraded), float64(h.Latency.Failing), 0},
		{"saturation", h.Saturation.Degraded, h.Saturation.Failing, 1},
		{"locks", h.Locks.Degraded, h.Locks.Failing, 0},
		{"backlog", h.Backlog.Degraded, h.Backlog.Failing, 0},
		{"lag", float64(h.Lag.Degraded), float64(h.Lag.Failing), 0},
		{"restarts", h.Restarts.Degraded, h.Restarts.Failing, 0},
		{"evictions", h.Evictions.Degraded, h.Evictions.Failing, 0},
		{"rejections", h.Rejections.Degraded, h.Rejections.Failing, 0},
	}
}

func (c *Config) validateHealthOverrides() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	// YAML accepts .nan and .inf; neither is a valid threshold or JSON value.
	for _, v := range healthValues(c.Health.HealthLimits) {
		if math.IsNaN(v.degraded) || math.IsNaN(v.failing) || math.IsInf(v.degraded, 0) || math.IsInf(v.failing, 0) {
			bad("health.%s limits must be finite", v.key)
		}
	}
	names := map[string]bool{}
	compiled := c.HealthOverrides()
	for i, o := range c.Health.Overrides {
		key := fmt.Sprintf("health.overrides[%d]", i)
		if o.Name == "" || strings.TrimSpace(o.Name) != o.Name || o.Name == "global" {
			bad("%s.name must be nonempty, trimmed and different from reserved name global", key)
		} else if names[o.Name] {
			bad("%s.name duplicates %q", key, o.Name)
		}
		names[o.Name] = true
		if o.Target != "node" && o.Target != "edge" {
			bad("%s.target must be node or edge", key)
		}
		if o.ID != strings.TrimSpace(o.ID) {
			bad("%s.id must be trimmed and match the canonical API identity", key)
		}
		if o.Target == "edge" && o.ID != "" {
			parts := strings.Split(o.ID, "->")
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				bad("%s.id must be a connection identity in the form caller->callee", key)
			}
		}
		selectors := len(o.Labels) + len(o.FromLabels) + len(o.ToLabels)
		if (o.ID == "" && selectors == 0) || (o.ID != "" && selectors > 0) {
			bad("%s must use exactly one matching mode: id or nonempty label selectors", key)
		}
		if o.Target == "node" && (len(o.FromLabels) > 0 || len(o.ToLabels) > 0) {
			bad("%s: node rules use labels, not fromLabels/toLabels", key)
		}
		if o.Target == "edge" && len(o.Labels) > 0 {
			bad("%s: edge rules use fromLabels/toLabels, not labels", key)
		}
		for _, selector := range []map[string]string{o.Labels, o.FromLabels, o.ToLabels} {
			for label := range selector {
				if strings.TrimSpace(label) == "" || strings.TrimSpace(label) != label {
					bad("%s has an empty or untrimmed label key", key)
				}
			}
		}
		for _, v := range healthValues(o.Limits) {
			switch {
			case math.IsNaN(v.degraded) || math.IsNaN(v.failing) || math.IsInf(v.degraded, 0) || math.IsInf(v.failing, 0):
				bad("%s.limits.%s must be finite", key, v.key)
			case v.degraded < 0 || v.failing < 0:
				bad("%s.limits.%s must not be negative", key, v.key)
			case v.maximum > 0 && (v.degraded > v.maximum || v.failing > v.maximum):
				bad("%s.limits.%s must be between 0 and 1", key, v.key)
			}
		}
		if len(compiled[i].Causes) == 0 {
			bad("%s.limits must set at least one positive threshold", key)
		}
		for _, cause := range compiled[i].Causes {
			d, f, unit := compiled[i].Limits.LimitsFor(cause)
			if d > f {
				bad("%s.limits.%s: degraded (%g %s) must not be above failing (%g %s), global inheritance included", key, cause, d, unit, f, unit)
			}
		}
		for j := 0; j < i; j++ {
			other := c.Health.Overrides[j]
			if o.Target != other.Target || o.Priority != other.Priority || (o.ID == "") != (other.ID == "") {
				continue
			}
			if o.ID != "" && o.ID != other.ID {
				continue
			}
			if !healthSelectorsOverlap(o.Labels, other.Labels) || !healthSelectorsOverlap(o.FromLabels, other.FromLabels) || !healthSelectorsOverlap(o.ToLabels, other.ToLabels) {
				continue
			}
			for _, cause := range compiled[i].Causes {
				if slices.Contains(compiled[j].Causes, cause) {
					bad("%s (%q) and health.overrides[%d] (%q) are ambiguous for %s: overlapping matches at priority %d; use different priorities or disjoint selectors", key, o.Name, j, other.Name, cause, o.Priority)
				}
			}
		}
	}
	return errors.Join(errs...)
}

func healthSelectorsOverlap(a, b map[string]string) bool {
	for key, value := range a {
		if other, exists := b[key]; exists && other != value {
			return false
		}
	}
	return true
}
