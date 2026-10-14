package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/nilum-br/teia/internal/model"
)

// Diff lists what changed between two rounds, with no memory of earlier
// ones. The first round has no previous one and reports nothing, so a
// restart does not flood anyone.
func Diff(prev, next model.Snapshot, at time.Time) []model.Change {
	var d Differ
	return d.Diff(prev, next, at)
}

// Differ lists what changed between consecutive rounds. It remembers the
// last known health of every node and edge: a round without rates (health
// "unknown") reports nothing, and the next known state is compared with the
// one before the gap, so failing, unknown, failing alerts once.
type Differ struct {
	known map[string]model.Health
}

// Diff lists what changed between prev and next. Removed items come in
// the order of prev (nodes by ID, edges by key, findings by urgency), so
// the same rounds always give the same changes.
func (d *Differ) Diff(prev, next model.Snapshot, at time.Time) []model.Change {
	if d.known == nil {
		d.known = map[string]model.Health{}
	}
	// A fresh differ starts from what the previous round knew.
	for _, n := range prev.Nodes {
		d.seed("node:"+n.ID, n.Health)
	}
	for _, e := range prev.Edges {
		d.seed("edge:"+e.Key(), e.Health)
	}

	first := prev.GeneratedAt.IsZero()
	var out []model.Change
	add := func(c model.Change) {
		if !first {
			c.At = at
			out = append(out, c)
		}
	}
	present := map[string]bool{}

	prevNodes := map[string]model.Node{}
	for _, n := range prev.Nodes {
		prevNodes[n.ID] = n
	}
	nextNodes := map[string]bool{}
	for _, n := range next.Nodes {
		nextNodes[n.ID] = true
		key := "node:" + n.ID
		present[key] = true
		p, existed := prevNodes[n.ID]
		from, changed := d.health(key, existed, n.Health)
		if !existed {
			add(model.Change{Type: model.ChangeNodeAdded, Subject: n.ID, To: string(n.Kind),
				Message: fmt.Sprintf("%s appeared (%s)", n.ID, n.Kind)})
			continue
		}
		if changed {
			add(model.Change{Type: model.ChangeNodeHealth, Subject: n.ID, From: string(from), To: string(n.Health),
				Message: fmt.Sprintf("%s is %s (%s)", n.ID, n.Health, why(n.Reasons, n.ErrorRate))})
		}
		if p.Visibility != n.Visibility {
			add(model.Change{Type: model.ChangeNodeVisibility, Subject: n.ID, From: string(p.Visibility), To: string(n.Visibility),
				Message: fmt.Sprintf("teia now has %s visibility of %s", n.Visibility, n.ID)})
		}
	}
	for _, n := range prev.Nodes {
		if !nextNodes[n.ID] {
			add(model.Change{Type: model.ChangeNodeRemoved, Subject: n.ID, Message: fmt.Sprintf("%s is gone", n.ID)})
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
		present["edge:"+key] = true
		p, existed := prevEdges[key]
		from, changed := d.health("edge:"+key, existed, e.Health)
		switch {
		case !existed && e.State == model.EdgeGhost:
			add(model.Change{Type: model.ChangeEdgeAdded, Subject: key, To: string(e.State),
				Message: fmt.Sprintf("new undeclared communication %s → %s", e.From, e.To)})
		case !existed:
			add(model.Change{Type: model.ChangeEdgeAdded, Subject: key, To: string(e.State),
				Message: fmt.Sprintf("new communication %s → %s (%s)", e.From, e.To, e.State)})
		default:
			if p.State != e.State {
				add(model.Change{Type: model.ChangeEdgeState, Subject: key, From: string(p.State), To: string(e.State),
					Message: fmt.Sprintf("%s → %s went from %s to %s", e.From, e.To, p.State, e.State)})
			}
			if changed {
				add(model.Change{Type: model.ChangeEdgeHealth, Subject: key, From: string(from), To: string(e.Health),
					Message: fmt.Sprintf("%s → %s is %s (%s)", e.From, e.To, e.Health, why(e.Reasons, e.ErrorRate))})
			}
		}
	}
	for _, e := range prev.Edges {
		if key := e.Key(); !nextEdges[key] {
			add(model.Change{Type: model.ChangeEdgeRemoved, Subject: key, From: string(e.State),
				Message: fmt.Sprintf("%s → %s is no longer seen", e.From, e.To)})
		}
	}

	prevFindings := map[string]bool{}
	for _, f := range prev.Findings {
		prevFindings[f.ID] = true
	}
	nextFindings := map[string]bool{}
	for _, f := range next.Findings {
		nextFindings[f.ID] = true
		if !prevFindings[f.ID] {
			add(model.Change{Type: model.ChangeFindingOpened, Subject: f.Subject, ID: f.ID, To: string(f.Severity), Message: f.Title})
		}
	}
	for _, f := range prev.Findings {
		if !nextFindings[f.ID] {
			add(model.Change{Type: model.ChangeFindingResolved, Subject: f.Subject, ID: f.ID, From: string(f.Severity), Message: "resolved: " + f.Title})
		}
	}

	// Forget what is gone, so the memory does not grow with every name
	// that ever passed through.
	for key := range d.known {
		if !present[key] {
			delete(d.known, key)
		}
	}
	return out
}

// seed records a known health for a subject the differ has not seen yet.
func (d *Differ) seed(key string, h model.Health) {
	if _, ok := d.known[key]; !ok && h != model.HealthUnknown && h != "" {
		d.known[key] = h
	}
}

// health records h for key and says whether it is news: a subject that
// existed and is now in a known state that differs from the last known
// one. It returns that last known state (unknown if there was none).
func (d *Differ) health(key string, existed bool, h model.Health) (model.Health, bool) {
	if h == model.HealthUnknown || h == "" {
		return "", false
	}
	last, ok := d.known[key]
	d.known[key] = h
	if !ok {
		last = model.HealthUnknown
	}
	return last, existed && last != h
}

// why says what a health change is about: its reasons, or the error rate
// when it is back to healthy.
func why(reasons []model.Reason, errorRate float64) string {
	if len(reasons) == 0 {
		return model.FormatPercent(errorRate) + " errors"
	}
	parts := make([]string, len(reasons))
	for i, r := range reasons {
		parts[i] = r.Summary
	}
	return strings.Join(parts, "; ")
}
