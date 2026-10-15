// Package review produces local contract-review artifacts. It does not change
// declarations, open pull requests, or grant network access.
package review

import (
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
	"github.com/nilum-br/teia/internal/source/declared"
)

type Change struct {
	Type    string   `json:"type"`
	Subject string   `json:"subject"`
	Fields  []string `json:"fields,omitempty"`
	Before  any      `json:"before,omitempty"`
	After   any      `json:"after,omitempty"`
}

type Candidate struct {
	ID       string     `json:"id"`
	Action   string     `json:"action"` // add or conflict
	Edge     model.Edge `json:"edge"`
	Identity string     `json:"identity"`
	Detail   string     `json:"detail"`
}

type Unobserved struct {
	ID                string `json:"id"`
	ExpectedFrequency string `json:"expectedFrequency,omitempty"`
	Detail            string `json:"detail"`
}

type Report struct {
	Mode       string       `json:"mode"`
	BeforeAt   time.Time    `json:"beforeAt,omitzero"`
	AfterAt    time.Time    `json:"afterAt,omitzero"`
	Changes    []Change     `json:"changes"`
	Candidates []Candidate  `json:"candidates"`
	Unobserved []Unobserved `json:"unobserved"`
}

// Snapshots compares topology, contract and health states. Rates/percentile jitter
// and collection timestamps alone are not changes. Changed records retain their
// complete original evidence for inspection.
func Snapshots(before, after model.Snapshot) Report {
	r := Report{Mode: "snapshots", BeforeAt: before.GeneratedAt, AfterAt: after.GeneratedAt}
	bn, an := nodes(before), nodes(after)
	for _, id := range unionKeys(bn, an) {
		b, bok := bn[id]
		a, aok := an[id]
		switch {
		case !bok:
			r.Changes = append(r.Changes, Change{Type: "node.added", Subject: id, After: a})
		case !aok:
			r.Changes = append(r.Changes, Change{Type: "node.removed", Subject: id, Before: b})
		default:
			fields := changedFields(map[string]any{"kind": b.Kind, "tech": b.Tech, "visibility": b.Visibility, "health": b.Health, "labels": b.Labels}, map[string]any{"kind": a.Kind, "tech": a.Tech, "visibility": a.Visibility, "health": a.Health, "labels": a.Labels})
			if len(fields) > 0 {
				r.Changes = append(r.Changes, Change{Type: "node.changed", Subject: id, Fields: fields, Before: b, After: a})
			}
		}
	}
	be, ae := edges(before), edges(after)
	for _, id := range unionKeys(be, ae) {
		b, bok := be[id]
		a, aok := ae[id]
		switch {
		case !bok:
			r.Changes = append(r.Changes, Change{Type: "edge.added", Subject: id, After: a})
		case !aok:
			r.Changes = append(r.Changes, Change{Type: "edge.removed", Subject: id, Before: b})
		default:
			fields := changedFields(edgeFields(b), edgeFields(a))
			if len(fields) > 0 {
				r.Changes = append(r.Changes, Change{Type: "edge.changed", Subject: id, Fields: fields, Before: b, After: a})
			}
		}
	}
	return r
}

func edgeFields(e model.Edge) map[string]any {
	return map[string]any{"protocol": e.Protocol, "port": e.Port, "reason": e.Reason, "declared": e.Declared, "observed": e.Observed, "state": e.State, "health": e.Health, "expectedFrequencySeconds": e.ExpectedFrequencySeconds}
}

func changedFields(before, after map[string]any) []string {
	var fields []string
	for field, value := range before {
		if !reflect.DeepEqual(value, after[field]) {
			fields = append(fields, field)
		}
	}
	sort.Strings(fields)
	return fields
}

// Contract compares saved observations with explicitly supplied flows files.
// An absent observation is never a deletion proposal. Existing protocol/port
// conflicts need an edit to the owning file, not a duplicate additive fragment.
func Contract(docs []declared.Document, snapshot model.Snapshot, aliases ...map[string]string) Report {
	r := Report{Mode: "contract", AfterAt: snapshot.GeneratedAt}
	aliasOf := map[string]string{}
	for _, mapping := range aliases {
		for from, to := range mapping {
			aliasOf[graph.NormalizeID(from)] = graph.NormalizeID(to)
		}
	}
	canonical := func(ref string) string {
		id, _ := declared.ParseRef(ref)
		id = graph.NormalizeID(id)
		if alias, ok := aliasOf[id]; ok {
			return alias
		}
		return id
	}
	known := map[string][]declared.Flow{}
	for _, doc := range docs {
		owner := canonical(doc.Spec.Owner)
		for _, flow := range doc.Spec.Egress {
			to := canonical(flow.To)
			key := owner + "->" + to
			known[key] = append(known[key], flow)
		}
	}
	observed := map[string]bool{}
	for _, edge := range snapshot.Edges {
		if !edge.Observed {
			continue
		}
		key := edge.Key()
		observed[key] = true
		flows := known[key]
		compatible := false
		for _, flow := range flows {
			if (flow.Protocol == "" || edge.Protocol == "" || strings.EqualFold(flow.Protocol, edge.Protocol)) && (flow.Port == 0 || edge.Port == 0 || flow.Port == edge.Port) {
				compatible = true
				break
			}
		}
		if compatible {
			continue
		}
		candidate := Candidate{ID: key, Action: "add", Edge: edge, Identity: identity(snapshot, edge), Detail: "Observed communication is absent from the supplied contract; acceptance requires an owning-team rationale."}
		if len(flows) > 0 {
			candidate.Action = "conflict"
			candidate.Detail = "Observed protocol or port conflicts with an existing declaration. Review and edit the owning flow; an additive duplicate would not resolve the conflict."
		}
		r.Candidates = append(r.Candidates, candidate)
	}
	for key, flows := range known {
		if observed[key] {
			continue
		}
		u := Unobserved{ID: key, Detail: "Not observed in this saved snapshot; this is not proof that the dependency is unused."}
		for _, flow := range flows {
			if flow.ExpectedFrequency != "" {
				u.ExpectedFrequency = flow.ExpectedFrequency
				u.Detail = "Rare/batch flow: a short snapshot cannot assess the declared expected frequency. Check original observation history; do not remove this declaration from absence alone."
				break
			}
		}
		r.Unobserved = append(r.Unobserved, u)
	}
	sort.Slice(r.Candidates, func(i, j int) bool { return r.Candidates[i].ID < r.Candidates[j].ID })
	sort.Slice(r.Unobserved, func(i, j int) bool { return r.Unobserved[i].ID < r.Unobserved[j].ID })
	return r
}

func identity(snapshot model.Snapshot, e model.Edge) string {
	if unresolvedAddress(e.From) || unresolvedAddress(e.To) {
		return "unresolved-address"
	}
	nodes := nodes(snapshot)
	if nodes[e.From].Labels["backstage.ref"] != "" && nodes[e.To].Labels["backstage.ref"] != "" {
		return "declared-reference"
	}
	return "named-identity: verify aliases and ownership"
}

func unresolvedAddress(id string) bool {
	if source.IsAddressID(id) {
		return true
	}
	if _, err := netip.ParseAddr(id); err == nil {
		return true
	}
	_, err := netip.ParseAddrPort(id)
	return err == nil
}

func nodes(s model.Snapshot) map[string]model.Node {
	out := map[string]model.Node{}
	for _, n := range s.Nodes {
		out[n.ID] = n
	}
	return out
}
func edges(s model.Snapshot) map[string]model.Edge {
	out := map[string]model.Edge{}
	for _, e := range s.Edges {
		out[e.Key()] = e
	}
	return out
}
func unionKeys[T any](a, b map[string]T) []string {
	out := make([]string, 0, len(a)+len(b))
	for k := range a {
		out = append(out, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// ValidateSnapshot rejects duplicate/empty identities that would otherwise be
// silently overwritten by the map used in a review.
func ValidateSnapshot(s model.Snapshot) error {
	seen := map[string]bool{}
	for _, n := range s.Nodes {
		if strings.TrimSpace(n.ID) == "" || seen[n.ID] {
			return fmt.Errorf("snapshot has empty or duplicate node identity %q", n.ID)
		}
		seen[n.ID] = true
	}
	seen = map[string]bool{}
	for _, e := range s.Edges {
		if strings.TrimSpace(e.From) == "" || strings.TrimSpace(e.To) == "" || seen[e.Key()] {
			return fmt.Errorf("snapshot has empty or duplicate connection identity %q", e.Key())
		}
		seen[e.Key()] = true
	}
	return nil
}
