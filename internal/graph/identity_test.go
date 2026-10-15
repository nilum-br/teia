package graph

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

func TestIdentityExplainsNamesAliasesAndUnresolvedAddresses(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	g := BuildAt([]Result{
		result("flows", "declared", source.Observation{Edges: []source.EdgeObservation{{From: " Orders ", To: "payments", Declared: true}}}),
		result("inventory", "docker", source.Observation{Nodes: []source.NodeObservation{{ID: "payments", IdentityMethod: model.IdentityExplicit, IdentityBasis: "Docker label teia.node"}}}),
		result("traces", "tempo", source.Observation{Edges: []source.EdgeObservation{{From: "Old-Orders", To: "Payments"}, {From: "payments", To: "ip:10.0.0.9"}}}),
	}, map[string]string{"old-orders": "orders"}, nil, now)
	e := g.Edges["orders->payments"]
	if e == nil || e.Identity == nil || len(e.Identity.From) != 2 || e.Identity.From[0].Method != model.IdentityExplicit || e.Identity.From[1].Method != model.IdentityAlias {
		t.Fatalf("declaration and alias provenance lost: %+v", e)
	}
	alias := e.Identity.From[1]
	if alias.Input != "Old-Orders" || alias.ResolvedID != "orders" || alias.Source != "traces" || alias.SourceType != "tempo" || alias.TimeBasis != "not-applicable" {
		t.Fatalf("alias cannot be explained: %+v", alias)
	}
	if !alias.CollectedAt.IsZero() || !alias.EvaluatedAt.IsZero() || !alias.ObservedAt.IsZero() {
		t.Fatal("snapshot time fabricated an identity timestamp")
	}
	if e.Identity.To[1].Method != model.IdentityName || e.Identity.To[1].Input != "Payments" {
		t.Fatal("source-reported name treated as explicit configuration")
	}
	if got := g.Edges["payments->ip:10.0.0.9"].Identity.To[0]; got.Method != model.IdentityUnresolved || len(got.Candidates) != 0 {
		t.Fatalf("unowned address = %+v", got)
	}
	if !slices.ContainsFunc(g.Nodes["payments"].Identity, func(r model.IdentityResolution) bool {
		return r.Method == model.IdentityExplicit && r.Basis == "Docker label teia.node"
	}) {
		t.Fatal("inventory's explicit naming provenance was lost")
	}
}

func TestIdentityAddressCandidatesDoNotDependOnInventoryOrder(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	a := result("cluster-a", "kubernetes", source.Observation{Nodes: []source.NodeObservation{{ID: "orders", Addresses: []string{"10.0.0.1"}, Since: now.Add(-time.Hour)}}})
	b := result("cluster-b", "docker", source.Observation{Nodes: []source.NodeObservation{{ID: "payments", Addresses: []string{"10.0.0.1"}, Since: now.Add(-time.Hour)}}})
	traffic := result("network", "prometheus", source.Observation{EvaluatedAt: now, Edges: []source.EdgeObservation{{From: "ip:10.0.0.1", To: "db", Window: time.Minute}}})
	first := BuildAt([]Result{a, b, traffic}, nil, nil, now)
	second := BuildAt([]Result{b, a, traffic}, nil, nil, now)
	for _, g := range []*Graph{first, second} {
		if g.Edges["orders->db"] != nil || g.Edges["payments->db"] != nil {
			t.Fatal("ambiguous address chose an inventory owner")
		}
		e := g.Edges["ip:10.0.0.1->db"]
		if e == nil || e.Identity.From[0].Method != model.IdentityAmbiguous || len(e.Identity.From[0].Candidates) != 2 {
			t.Fatalf("conflict hidden: %+v", e)
		}
	}
	if !reflect.DeepEqual(first.Edges["ip:10.0.0.1->db"].Identity, second.Edges["ip:10.0.0.1->db"].Identity) {
		t.Fatal("source order changed provenance")
	}
	// A failed discovery is not an address claim. An explicit alias can also
	// select a name intentionally without pretending to verify an IP lease.
	b.Err = errors.New("inventory failed")
	if g := BuildAt([]Result{a, b, traffic}, nil, nil, now); g.Edges["orders->db"] == nil {
		t.Fatal("failed inventory influenced identity")
	}
	b.Err = nil
	if g := BuildAt([]Result{a, b, traffic}, map[string]string{"ip:10.0.0.1": "selected"}, nil, now); g.Edges["selected->db"].Identity.From[0].Method != model.IdentityAlias {
		t.Fatal("explicit address alias not recorded")
	}
}

func TestIdentityIPTimeEvidenceAndMissingTimes(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                        string
		since, evaluated, collected time.Time
		retained                    bool
		sample                      int
		want                        model.IdentityMethod
		basis                       string
	}{
		{"known window", now.Add(-time.Hour), now, now.Add(time.Second), false, 0, model.IdentityIPTemporal, "evaluation-window"},
		{"collection is only window anchor", now.Add(-time.Hour), time.Time{}, now, false, 0, model.IdentityIPTemporal, "collection-window"},
		{"no collection or evaluation time", now.Add(-time.Hour), time.Time{}, time.Time{}, false, 0, model.IdentityInferred, "unknown"},
		{"unknown ownership start", time.Time{}, now, now, false, 0, model.IdentityInferred, "evaluation-window"},
		{"retained inventory", now.Add(-time.Hour), now, now, true, 0, model.IdentityInferred, "evaluation-window"},
		{"sample with missing original time", now.Add(-time.Hour), now, now, false, 2, model.IdentityInferred, "evaluation-window"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inventory := result("inventory", "kubernetes", source.Observation{Nodes: []source.NodeObservation{{ID: "orders", Addresses: []string{"10.0.0.1"}, Since: tc.since}}})
			inventory.CollectedAt, inventory.Retained = now.Add(-time.Minute), tc.retained
			traffic := result("traffic", "prometheus", source.Observation{EvaluatedAt: tc.evaluated, Edges: []source.EdgeObservation{{From: "ip:10.0.0.1", To: "db", Window: time.Minute, SampleCount: tc.sample}}})
			traffic.CollectedAt = tc.collected
			g := BuildAt([]Result{inventory, traffic}, nil, nil, now.Add(time.Second))
			r := g.Edges["orders->db"].Identity.From[0]
			if r.Method != tc.want || r.TimeBasis != tc.basis || !r.CollectedAt.Equal(tc.collected) || !r.EvaluatedAt.Equal(tc.evaluated) || !r.ObservedAt.IsZero() {
				t.Fatalf("time evidence = %+v", r)
			}
			c := r.Candidates[0]
			if c.Retained != tc.retained || !c.Since.Equal(tc.since) || !c.CollectedAt.Equal(inventory.CollectedAt) {
				t.Fatalf("inventory provenance = %+v", c)
			}
		})
	}
}

func TestIdentityWithheldRecycledIPHasCandidateExplanation(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, fromIP := range []bool{false, true} {
		edge := source.EdgeObservation{From: "caller", To: "ip:10.0.0.1", Window: time.Minute, ObservedFrom: now.Add(-time.Hour), ObservedAt: now.Add(-time.Second), SampleCount: 2}
		if fromIP {
			edge.From, edge.To = edge.To, "callee"
		}
		g := BuildAt([]Result{
			result("inventory", "docker", source.Observation{Nodes: []source.NodeObservation{{ID: "new-owner", Addresses: []string{"10.0.0.1"}, Since: now.Add(-30 * time.Minute)}}}),
			result("tempo", "tempo", source.Observation{EvaluatedAt: now, Edges: []source.EdgeObservation{edge}}),
		}, nil, nil, now)
		if len(g.Edges) != 0 {
			t.Fatal("a recycled address created current topology")
		}
		entries := g.Nodes["new-owner"].Identity
		if len(entries) != 2 {
			t.Fatalf("withheld attribution has no explanation: %+v", entries)
		}
		r := entries[1]
		if r.Method != model.IdentityAmbiguous || r.ResolvedID != "ip:10.0.0.1" || !r.ObservedFrom.Equal(edge.ObservedFrom) || !strings.Contains(r.Detail, "withheld") {
			t.Fatalf("recycled address = %+v", r)
		}
	}
}

func TestIdentityRetentionDoesNotMutateOrRefreshOriginalDecision(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	inventory := result("inventory", "docker", source.Observation{Nodes: []source.NodeObservation{{ID: "orders", Addresses: []string{"10.0.0.1"}, Since: now.Add(-time.Hour)}}})
	traffic := result("traffic", "prometheus", source.Observation{EvaluatedAt: now, Edges: []source.EdgeObservation{{From: "ip:10.0.0.1", To: "db", Window: time.Minute}}})
	inventory.CollectedAt, traffic.CollectedAt = now, now
	first := BuildAt([]Result{inventory, traffic}, nil, nil, now)
	memory := Memory{Retention: time.Minute}
	memory.Retain(first, now)
	later := BuildAt(nil, nil, nil, now.Add(30*time.Second))
	memory.Retain(later, now.Add(30*time.Second))
	old, kept := first.Edges["orders->db"].Identity.From[0], later.Edges["orders->db"].Identity.From[0]
	if old.Retained || old.Candidates[0].Retained || !kept.Retained || !kept.Candidates[0].Retained {
		t.Fatal("retention aliased the original snapshot or lost retained status")
	}
	if kept.Method != model.IdentityIPTemporal || !kept.CollectedAt.Equal(now) || !kept.Candidates[0].CollectedAt.Equal(now) {
		t.Fatalf("retention rewrote original decision: %+v", kept)
	}
	if !later.Nodes["orders"].Identity[0].Retained {
		t.Fatal("recreated node lost retained identity")
	}
	last := BuildAt(nil, nil, nil, now.Add(61*time.Second))
	memory.Retain(last, now.Add(61*time.Second))
	if len(last.Edges) != 0 {
		t.Fatal("identity metadata renewed topology retention")
	}
}
