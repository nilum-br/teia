// Package journey defines ownership scopes independently of any renderer.
package journey

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/nilum-br/teia/internal/model"
)

const (
	MaxDepth   = 2
	MaxNodes   = 200
	Annotation = "teia.nilum.io/journeys"
)

var legacyKeys = []string{"journey", "app.kubernetes.io/part-of", "system"}

// ParseAnnotation accepts a JSON array, never a comma-delimited Kubernetes
// label. Bound metadata before decoding and reject malformed values as a whole.
func ParseAnnotation(raw string) ([]string, error) {
	if len(raw) > 8192 {
		return nil, fmt.Errorf("must be at most 8192 bytes")
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil || names == nil {
		return nil, fmt.Errorf("must be a JSON array of journey names")
	}
	if len(names) > 64 {
		return nil, fmt.Errorf("must contain at most 64 journey names")
	}
	for i, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || len(name) > 128 || strings.ContainsAny(name, ",:=") || strings.ContainsFunc(name, unicode.IsControl) {
			return nil, fmt.Errorf("journey at index %d must be 1–128 bytes without commas, colons, equals signs or control characters", i)
		}
		names[i] = name
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

func labelValues(n model.Node, key string) []string {
	var out []string
	for _, value := range strings.Split(n.Labels[key], ",") {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func defaultKey(nodes []model.Node) string {
	for _, key := range legacyKeys {
		for _, n := range nodes {
			if len(labelValues(n, key)) > 0 {
				return key
			}
		}
	}
	return legacyKeys[0]
}

// Build computes the default contract, or a custom label scope when key is set.
// Explicit annotation membership augments the detected legacy journey label;
// choosing an unrelated custom label deliberately switches the scope definition.
func Build(s model.Snapshot, key string) *model.JourneyContract {
	detected := defaultKey(s.Nodes)
	if key == "" {
		key = detected
	}
	c := &model.JourneyContract{Version: 1, Mode: "ownership", Key: key, MaxDepth: MaxDepth, MaxNodes: MaxNodes, Scopes: []model.JourneyScope{}}
	nodes := map[string]model.Node{}
	memberships := map[string][]string{}
	names := map[string]bool{}
	out := map[string][]string{}
	for _, n := range s.Nodes {
		nodes[n.ID] = n
		values := labelValues(n, key)
		if key == detected {
			values = append(values, n.Journeys...)
		}
		for i := range values {
			values[i] = strings.ToLower(strings.TrimSpace(values[i]))
		}
		slices.Sort(values)
		memberships[n.ID] = slices.Compact(values)
		for _, value := range values {
			if value != "" {
				names[value] = true
			}
		}
	}
	for _, e := range s.Edges {
		if _, ok := nodes[e.From]; !ok {
			continue
		}
		if _, ok := nodes[e.To]; !ok {
			continue
		}
		out[e.From] = append(out[e.From], e.To)
	}
	for id := range out {
		slices.Sort(out[id])
		out[id] = slices.Compact(out[id])
	}
	for _, name := range sortedKeys(names) {
		c.Scopes = append(c.Scopes, buildScope(s, nodes, memberships, out, key, name, MaxDepth, MaxNodes))
	}
	return c
}

func buildScope(s model.Snapshot, nodes map[string]model.Node, memberships map[string][]string, out map[string][]string, key, name string, maxDepth, maxNodes int) model.JourneyScope {
	scope := model.JourneyScope{Name: name, Key: key, Roles: map[string]string{}, EntryPoints: []string{}, EntryEdges: []string{}, BoundaryPorts: []model.JourneyBoundary{}, Health: model.HealthUnknown}
	type step struct {
		id    string
		depth int
	}
	var queue []step
	for _, id := range sortedKeys(nodes) {
		if !slices.Contains(memberships[id], name) {
			continue
		}
		if len(scope.Roles) >= maxNodes {
			scope.Truncated = true
			continue
		}
		scope.Roles[id] = "member"
		queue = append(queue, step{id, 0})
	}
	for i := 0; i < len(queue); i++ {
		at := queue[i]
		if nodes[at.id].Kind != model.KindService {
			continue
		}
		for _, to := range out[at.id] {
			if scope.Roles[to] != "" {
				continue
			}
			n := nodes[to]
			if n.Kind == model.KindService && len(memberships[to]) > 0 {
				continue
			}
			depth := at.depth + 1
			if n.Kind == model.KindService && depth > maxDepth {
				scope.Truncated = true
				continue
			}
			if len(scope.Roles) >= maxNodes {
				scope.Truncated = true
				continue
			}
			scope.Roles[to] = "dependency"
			queue = append(queue, step{to, depth})
		}
	}
	called := map[string]bool{}
	for _, e := range s.Edges {
		called[e.To] = true
	}
	entries := map[string]bool{}
	for id := range scope.Roles {
		if nodes[id].Health.Rank() > scope.Health.Rank() {
			scope.Health = nodes[id].Health
		}
		if nodes[id].Kind == model.KindService && !called[id] {
			entries[id] = true
		}
	}
	for _, e := range s.Edges {
		from, to := scope.Roles[e.From] != "", scope.Roles[e.To] != ""
		if from && to && e.Health.Rank() > scope.Health.Rank() {
			scope.Health = e.Health
		}
		if from != to {
			kind := "exit"
			if to {
				kind = "caller"
				if isResource(nodes[e.To]) {
					kind = "tenant"
				} else {
					entries[e.To] = true
				}
			}
			scope.BoundaryPorts = append(scope.BoundaryPorts, model.JourneyBoundary{Kind: kind, EdgeID: e.Key(), From: e.From, To: e.To})
		}
		if to && ((!from && !isResource(nodes[e.To])) || (from && !called[e.From])) {
			scope.EntryEdges = append(scope.EntryEdges, e.Key())
		}
	}
	scope.EntryPoints = sortedKeys(entries)
	slices.Sort(scope.EntryEdges)
	slices.SortFunc(scope.BoundaryPorts, func(a, b model.JourneyBoundary) int { return strings.Compare(a.EdgeID, b.EdgeID) })
	return scope
}

func isResource(n model.Node) bool {
	return n.Kind == model.KindDatabase || n.Kind == model.KindCache || n.Kind == model.KindQueue || n.Kind == model.KindExternal
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
