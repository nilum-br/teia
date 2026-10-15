package journey

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

func TestSharedOwnershipFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/shared.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Snapshot model.Snapshot                `json:"snapshot"`
		Expected map[string]model.JourneyScope `json:"expected"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	contract := Build(fixture.Snapshot, "")
	if contract.Version != 1 || contract.Mode != "ownership" || len(contract.Scopes) != 2 {
		t.Fatalf("contract = %+v", contract)
	}
	for _, scope := range contract.Scopes {
		want := fixture.Expected[scope.Name]
		want.Name, want.Key = scope.Name, contract.Key
		if !reflect.DeepEqual(scope, want) {
			t.Errorf("%s:\ngot  %+v\nwant %+v", scope.Name, scope, want)
		}
	}
	// Source, node, and edge order cannot change membership or the scope cap.
	slices.Reverse(fixture.Snapshot.Nodes)
	slices.Reverse(fixture.Snapshot.Edges)
	if got := Build(fixture.Snapshot, ""); !reflect.DeepEqual(got, contract) {
		t.Error("scope depends on source iteration order")
	}
}

func TestMembershipCompatibilityAndCustomLabels(t *testing.T) {
	s := model.Snapshot{Nodes: []model.Node{
		{ID: "web", Kind: model.KindService, Journeys: []string{"Checkout"}, Labels: map[string]string{"app.kubernetes.io/part-of": "browse", "team": "platform"}},
	}}
	c := Build(s, "")
	if c.Key != "app.kubernetes.io/part-of" || len(c.Scopes) != 2 || c.Scopes[0].Name != "browse" || c.Scopes[1].Name != "checkout" {
		t.Fatalf("annotation must augment existing label: %+v", c)
	}
	c = Build(s, "team")
	if len(c.Scopes) != 1 || c.Scopes[0].Name != "platform" {
		t.Fatalf("custom label must not mix ownership memberships: %+v", c)
	}
}

func TestCycleHasNoInventedEntryPoint(t *testing.T) {
	s := model.Snapshot{Nodes: []model.Node{
		{ID: "a", Kind: model.KindService, Labels: map[string]string{"journey": "cycle"}, Health: model.HealthUnknown},
		{ID: "b", Kind: model.KindService, Health: model.HealthUnknown},
	}, Edges: []model.Edge{{From: "a", To: "b"}, {From: "b", To: "a"}}}
	j := Build(s, "").Scopes[0]
	if len(j.Roles) != 2 || len(j.EntryPoints) != 0 || len(j.EntryEdges) != 0 || j.Truncated || j.Health != model.HealthUnknown {
		t.Fatalf("cycle without telemetry = %+v", j)
	}
}

func TestScopeCapIncludesMembers(t *testing.T) {
	s := model.Snapshot{}
	for i := 0; i < MaxNodes+10; i++ {
		s.Nodes = append(s.Nodes, model.Node{ID: fmt.Sprintf("n%03d", i), Kind: model.KindService, Labels: map[string]string{"journey": "large"}})
	}
	j := Build(s, "").Scopes[0]
	if len(j.Roles) != MaxNodes || !j.Truncated || j.Roles["n000"] != "member" || j.Roles["n200"] != "" {
		t.Fatalf("unbounded or unstable member cap: %d nodes, truncated=%v", len(j.Roles), j.Truncated)
	}
}

func TestParseAnnotation(t *testing.T) {
	got, err := ParseAnnotation(`["Checkout", " browse ", "checkout"]`)
	if err != nil || !reflect.DeepEqual(got, []string{"browse", "checkout"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, raw := range []string{`checkout,browse`, `null`, `[1]`, `["checkout,browse"]`, `[""]`, `["from:orders"]`, strings.Repeat("x", 8193)} {
		if _, err := ParseAnnotation(raw); err == nil {
			t.Errorf("accepted malformed annotation %q", raw)
		}
	}
	if got, err := ParseAnnotation(`[]`); err != nil || len(got) != 0 {
		t.Errorf("empty array: %v %v", got, err)
	}
}
