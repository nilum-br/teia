package engine

import (
	"fmt"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// Diff lists what changed between two rounds. The first round has no
// previous one and reports nothing, so a restart does not flood anyone.
func Diff(prev, next model.Snapshot, at time.Time) []model.Change {
	if prev.GeneratedAt.IsZero() {
		return nil
	}
	var out []model.Change
	add := func(t model.ChangeType, subject, from, to, msg string) {
		out = append(out, model.Change{At: at, Type: t, Subject: subject, From: from, To: to, Message: msg})
	}

	prevNodes := map[string]model.Node{}
	for _, n := range prev.Nodes {
		prevNodes[n.ID] = n
	}
	nextNodes := map[string]model.Node{}
	for _, n := range next.Nodes {
		nextNodes[n.ID] = n
		p, ok := prevNodes[n.ID]
		switch {
		case !ok:
			add(model.ChangeNodeAdded, n.ID, "", string(n.Kind), fmt.Sprintf("%s appeared (%s)", n.ID, n.Kind))
		default:
			if p.Health != n.Health && n.Health != model.HealthUnknown {
				add(model.ChangeNodeHealth, n.ID, string(p.Health), string(n.Health),
					fmt.Sprintf("%s is %s (%.1f%% errors)", n.ID, n.Health, n.ErrorRate*100))
			}
			if p.Visibility != n.Visibility {
				add(model.ChangeNodeVisibility, n.ID, string(p.Visibility), string(n.Visibility),
					fmt.Sprintf("teia now has %s visibility of %s", n.Visibility, n.ID))
			}
		}
	}
	for id := range prevNodes {
		if _, ok := nextNodes[id]; !ok {
			add(model.ChangeNodeRemoved, id, "", "", fmt.Sprintf("%s is gone", id))
		}
	}

	prevEdges := map[string]model.Edge{}
	for _, e := range prev.Edges {
		prevEdges[e.Key()] = e
	}
	nextEdges := map[string]bool{}
	for _, e := range next.Edges {
		key := e.Key()
		nextEdges[key] = true
		p, ok := prevEdges[key]
		switch {
		case !ok && e.State == model.EdgeGhost:
			add(model.ChangeEdgeAdded, key, "", string(e.State), fmt.Sprintf("new undeclared communication %s → %s", e.From, e.To))
		case !ok:
			add(model.ChangeEdgeAdded, key, "", string(e.State), fmt.Sprintf("new communication %s → %s (%s)", e.From, e.To, e.State))
		default:
			if p.State != e.State {
				add(model.ChangeEdgeState, key, string(p.State), string(e.State),
					fmt.Sprintf("%s → %s went from %s to %s", e.From, e.To, p.State, e.State))
			}
			if p.Health != e.Health && e.Health != model.HealthUnknown {
				add(model.ChangeEdgeHealth, key, string(p.Health), string(e.Health),
					fmt.Sprintf("%s → %s is %s (%.1f%% errors)", e.From, e.To, e.Health, e.ErrorRate*100))
			}
		}
	}
	for key, e := range prevEdges {
		if !nextEdges[key] {
			add(model.ChangeEdgeRemoved, key, string(e.State), "", fmt.Sprintf("%s → %s is no longer seen", e.From, e.To))
		}
	}

	prevFindings := map[string]model.Finding{}
	for _, f := range prev.Findings {
		prevFindings[f.ID] = f
	}
	nextFindings := map[string]bool{}
	for _, f := range next.Findings {
		nextFindings[f.ID] = true
		if _, ok := prevFindings[f.ID]; !ok {
			add(model.ChangeFindingOpened, f.Subject, "", string(f.Severity), f.Title)
		}
	}
	for id, f := range prevFindings {
		if !nextFindings[id] {
			add(model.ChangeFindingResolved, f.Subject, string(f.Severity), "", "resolved: "+f.Title)
		}
	}
	return out
}
