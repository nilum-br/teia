package review

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source/declared"
)

// Flows writes only explicitly accepted additions, with operator-supplied
// rationale. Evidence is a YAML comment, not a declaration of future observation
// or a permission grant. Existing files and unrelated flows are untouched.
func Flows(w io.Writer, report Report, snapshot model.Snapshot, accepted map[string]string) error {
	if report.Mode != "contract" {
		return fmt.Errorf("flows output requires a contract comparison")
	}
	if len(accepted) == 0 {
		return fmt.Errorf("flows output requires --accept 'caller->callee=owning-team rationale'")
	}
	candidates := map[string]Candidate{}
	for _, candidate := range report.Candidates {
		candidates[candidate.ID] = candidate
	}
	byOwner := map[string][]Candidate{}
	for key, reason := range accepted {
		candidate, ok := candidates[key]
		if !ok {
			return fmt.Errorf("accepted connection %q is not an observed contract addition", key)
		}
		if candidate.Action != "add" {
			return fmt.Errorf("%s conflicts with an existing flow; edit that declaration instead of adding a duplicate", key)
		}
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("accepted connection %s needs a nonempty owning-team rationale", key)
		}
		if candidate.Identity == "unresolved-address" {
			return fmt.Errorf("%s has an unresolved address identity; resolve inventory/aliases before generating a stable contract", key)
		}
		byOwner[candidate.Edge.From] = append(byOwner[candidate.Edge.From], candidate)
	}
	nodeMap := nodes(snapshot)
	owners := make([]string, 0, len(byOwner))
	for owner := range byOwner {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	// Build and validate everything before writing, avoiding a partially emitted
	// review artifact when a later owner has invalid protocol/port/name metadata.
	var documents []yaml.Node
	for _, owner := range owners {
		candidates := byOwner[owner]
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
		doc := declared.Document{APIVersion: declared.APIVersion, Kind: declared.KindFlows, Metadata: declared.Metadata{Name: owner}, Spec: declared.Spec{Owner: reviewRef(nodeMap[owner], owner)}}
		if parsed, _ := declared.ParseRef(doc.Spec.Owner); parsed != owner {
			return fmt.Errorf("owner %q cannot round-trip through a Flows ref; resolve its canonical identity first", owner)
		}
		if kind := nodeMap[owner].Kind; kind != "" && kind != model.KindUnknown && kind != model.KindService {
			doc.Spec.Kind = string(kind)
		}
		for _, candidate := range candidates {
			e := candidate.Edge
			flow := declared.Flow{To: reviewRef(nodeMap[e.To], e.To), Protocol: e.Protocol, Port: e.Port, Reason: strings.TrimSpace(accepted[candidate.ID])}
			if parsed, _ := declared.ParseRef(flow.To); parsed != e.To {
				return fmt.Errorf("destination %q cannot round-trip through a Flows ref; resolve its canonical identity first", e.To)
			}
			if n := nodeMap[e.To]; n.Kind != "" && n.Kind != model.KindUnknown && n.Kind != model.KindService {
				flow.Kind = string(n.Kind)
			}
			doc.Spec.Egress = append(doc.Spec.Egress, flow)
		}
		if errs := declared.Validate(doc); len(errs) > 0 {
			return fmt.Errorf("generated contract for %s: %w", owner, errs[0])
		}
		var node yaml.Node
		if err := node.Encode(doc); err != nil {
			return err
		}
		node.HeadComment = "Review required: additive contract fragment; merge into the owner's existing spec.egress. Does not deploy or authorize traffic."
		if spec := yamlField(&node, "spec"); spec != nil {
			if egress := yamlField(spec, "egress"); egress != nil {
				for i, flow := range egress.Content {
					flow.HeadComment = evidenceComment(candidates[i], snapshot.GeneratedAt)
				}
			}
		}
		documents = append(documents, node)
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	for i := range documents {
		if err := enc.Encode(&documents[i]); err != nil {
			return err
		}
	}
	return enc.Close()
}

func reviewRef(n model.Node, id string) string {
	if ref := n.Labels["backstage.ref"]; ref != "" {
		if parsed, _ := declared.ParseRef(ref); parsed == id {
			return ref
		}
	}
	switch n.Kind {
	case model.KindExternal:
		return "external:" + id
	case model.KindCache, model.KindDatabase, model.KindQueue:
		return "resource:default/" + id
	default:
		return "component:default/" + id
	}
}

func yamlField(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func evidenceComment(c Candidate, snapshotAt time.Time) string {
	lines := []string{"Accepted observation: " + c.ID, "Identity: " + c.Identity}
	if !snapshotAt.IsZero() {
		lines = append(lines, "Snapshot captured at: "+snapshotAt.Format(time.RFC3339Nano)+" (not original call time)")
	}
	if len(c.Edge.Sources) > 0 {
		lines = append(lines, "Sources: "+strings.Join(c.Edge.Sources, ", "))
	}
	if len(c.Edge.Evidence) == 0 {
		lines = append(lines, "Original source/time evidence unavailable in this snapshot.")
	}
	for _, ev := range c.Edge.Evidence {
		line := fmt.Sprintf("Evidence: source=%s signal=%s window=%gs retained=%t", ev.Source, ev.Signal, ev.WindowSeconds, ev.Retained)
		if !ev.ObservedAt.IsZero() {
			line += " observedAt=" + ev.ObservedAt.Format(time.RFC3339Nano)
		} else {
			line += " originalTime=unknown"
		}
		if !ev.ObservedFrom.IsZero() {
			line += " observedFrom=" + ev.ObservedFrom.Format(time.RFC3339Nano)
		}
		if !ev.EvaluatedAt.IsZero() {
			line += " evaluatedAt=" + ev.EvaluatedAt.Format(time.RFC3339Nano)
		}
		if !ev.CollectedAt.IsZero() {
			line += " collectedAt=" + ev.CollectedAt.Format(time.RFC3339Nano)
		}
		if ev.Origin != "" {
			line += " origin=" + ev.Origin
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
