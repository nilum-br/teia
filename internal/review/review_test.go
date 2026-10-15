package review

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
	"github.com/nilum-br/teia/internal/source/declared"
)

func TestContractUsesExplicitAliasesAndRejectsLossyRefs(t *testing.T) {
	snapshot := model.Snapshot{Nodes: []model.Node{{ID: "orders"}, {ID: "database"}}, Edges: []model.Edge{{From: "orders", To: "database", Observed: true}}}
	docs := []declared.Document{{Spec: declared.Spec{Owner: "component:default/old-orders", Egress: []declared.Flow{{To: "resource:default/old-db"}}}}}
	report := Contract(docs, snapshot, map[string]string{"old-orders": "orders", "old-db": "database"})
	if len(report.Candidates)+len(report.Unobserved) != 0 {
		t.Fatalf("aliases created false drift: %+v", report)
	}
	snapshot.Edges[0].To = "region/database"
	var output bytes.Buffer
	if err := Flows(&output, Contract(nil, snapshot), snapshot, map[string]string{"orders->region/database": "persist orders"}); err == nil || output.Len() != 0 {
		t.Fatalf("lossy ref generated a different identity: %v %s", err, output.String())
	}
}

func fixture() model.Snapshot {
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	return model.Snapshot{GeneratedAt: at, Nodes: []model.Node{{ID: "orders", Kind: model.KindService}, {ID: "db", Kind: model.KindDatabase}}, Edges: []model.Edge{{From: "orders", To: "db", Protocol: "postgresql", Port: 5432, Observed: true, State: model.EdgeGhost, Sources: []string{"tempo"}, Evidence: []model.Evidence{{Source: "tempo", Signal: "traces", ObservedAt: at.Add(-time.Minute), WindowSeconds: 300, Metrics: []string{"topology"}}}}}}
}

func TestObservedReviewDeclaredReconciles(t *testing.T) {
	snapshot := fixture()
	report := Contract(nil, snapshot)
	if len(report.Candidates) != 1 || report.Candidates[0].Action != "add" {
		t.Fatalf("want an observed contract addition: %+v", report)
	}
	var out bytes.Buffer
	if err := Flows(&out, report, snapshot, map[string]string{"orders->db": "persist orders after payment"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "observedAt=2026-10-15T11:59:00Z") || !strings.Contains(out.String(), "not original call time") || !strings.Contains(out.String(), "Identity:") {
		t.Fatalf("review evidence missing:\n%s", out.String())
	}
	docs, err := declared.Parse(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || len(docs[0].Spec.Egress) != 1 {
		t.Fatalf("not a minimal additive document: %+v", docs)
	}
	flow := docs[0].Spec.Egress[0]
	if flow.To != "resource:default/db" || flow.Protocol != "postgresql" || flow.Port != 5432 || flow.Reason != "persist orders after payment" {
		t.Fatalf("lost contract detail: %+v", flow)
	}
	if errs := declared.Validate(docs[0]); len(errs) > 0 {
		t.Fatal(errs)
	}
	file := filepath.Join(t.TempDir(), "flows.yaml")
	if err := os.WriteFile(file, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	declaredSource := declared.New("declared", []string{file})
	intent, err := declaredSource.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	g := graph.Build([]graph.Result{{Source: declaredSource, Observation: intent}, {Source: fakeSource{}, Observation: source.Observation{Edges: []source.EdgeObservation{{From: "orders", To: "db", Protocol: "postgresql", Port: 5432, RequestRate: 1}}}}}, nil, nil)
	if edge := g.Edges["orders->db"]; edge == nil || edge.State != model.EdgeOK || edge.Port != 5432 || edge.Reason != flow.Reason {
		t.Fatalf("reviewed declaration did not reconcile: %+v", edge)
	}
	if next := Contract(docs, snapshot); len(next.Candidates) != 0 {
		t.Fatalf("accepted flow still proposed: %+v", next)
	}
}

type fakeSource struct{}

func (fakeSource) Name() string { return "observed" }
func (fakeSource) Type() string { return "tempo" }
func (fakeSource) Discover(context.Context) (source.Observation, error) {
	return source.Observation{}, nil
}

func TestMinimalReviewRejectsUnsafeOrConflictingAcceptance(t *testing.T) {
	snapshot := fixture()
	baseline := []declared.Document{{Spec: declared.Spec{Owner: "orders", Egress: []declared.Flow{{To: "db", Protocol: "mysql", Port: 3306}}}}}
	report := Contract(baseline, snapshot)
	if len(report.Candidates) != 1 || report.Candidates[0].Action != "conflict" {
		t.Fatalf("port/protocol conflict hidden: %+v", report)
	}
	for _, tc := range []struct {
		report   Report
		accepted map[string]string
		want     string
	}{
		{report, map[string]string{"orders->db": "review"}, "conflicts"},
		{Contract(nil, snapshot), nil, "--accept"},
		{Contract(nil, snapshot), map[string]string{"orders->db": " "}, "rationale"},
		{Contract(nil, snapshot), map[string]string{"other->db": "why"}, "not an observed"},
	} {
		var out bytes.Buffer
		err := Flows(&out, tc.report, snapshot, tc.accepted)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("error %v, want %s", err, tc.want)
		}
		if out.Len() != 0 {
			t.Fatal("invalid selection emitted a partial artifact")
		}
	}
	snapshot.Edges[0].To = "ip:10.0.0.1"
	if err := Flows(&bytes.Buffer{}, Contract(nil, snapshot), snapshot, map[string]string{"orders->ip:10.0.0.1": "why"}); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("unresolved identity accepted: %v", err)
	}
}

func TestReviewAbsenceNeverSuggestsDeletingDailyJob(t *testing.T) {
	docs := []declared.Document{{Spec: declared.Spec{Owner: "daily", Egress: []declared.Flow{{To: "archive", ExpectedFrequency: "24h"}}}}}
	report := Contract(docs, model.Snapshot{})
	if len(report.Candidates) != 0 || len(report.Unobserved) != 1 || report.Unobserved[0].ExpectedFrequency != "24h" || !strings.Contains(report.Unobserved[0].Detail, "short snapshot") {
		t.Fatalf("daily job wrongly classified: %+v", report)
	}
}

func TestSnapshotDiffIgnoresRefreshButPreservesMeaningfulChanges(t *testing.T) {
	before := fixture()
	after := fixture()
	after.GeneratedAt = after.GeneratedAt.Add(time.Minute)
	after.Edges[0].RequestRate = 42
	if diff := Snapshots(before, after); len(diff.Changes) != 0 {
		t.Fatalf("collection/rate jitter was a topology/state diff: %+v", diff)
	}
	after.Edges[0].Port = 6432
	after.Edges[0].Health = model.HealthFailing
	diff := Snapshots(before, after)
	if len(diff.Changes) != 1 || diff.Changes[0].Type != "edge.changed" || strings.Join(diff.Changes[0].Fields, ",") != "health,port" {
		t.Fatalf("lost meaningful change: %+v", diff)
	}
	if err := ValidateSnapshot(model.Snapshot{Nodes: []model.Node{{ID: "a"}, {ID: "a"}}}); err == nil {
		t.Fatal("duplicate node identity accepted")
	}
}

func TestReviewYAMLKeepsHostileEvidenceInComments(t *testing.T) {
	snapshot := fixture()
	snapshot.Edges[0].Evidence[0].Source = "tempo\nkind: Malicious\n---\nspec: {owner: attacker}"
	var out bytes.Buffer
	if err := Flows(&out, Contract(nil, snapshot), snapshot, map[string]string{"orders->db": "reason: quoted\nwith newline"}); err != nil {
		t.Fatal(err)
	}
	docs, err := declared.Parse(out.Bytes())
	if err != nil || len(docs) != 1 || docs[0].Spec.Owner != "component:default/orders" {
		t.Fatalf("evidence reshaped artifact: %v %s", err, out.String())
	}
}
