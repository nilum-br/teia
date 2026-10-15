package graph

import (
	"reflect"
	"testing"

	"github.com/nilum-br/teia/internal/source"
)

func TestJourneyMembershipSurvivesIdentityMerge(t *testing.T) {
	g := Build([]Result{result("k8s", "kubernetes", source.Observation{Nodes: []source.NodeObservation{
		{ID: "Web", Journeys: []string{"browse", "checkout"}},
		{ID: "web", Journeys: []string{"browse", "checkout"}},
	}})}, map[string]string{"web": "frontend"}, nil)
	if got := g.Nodes["frontend"].Journeys; !reflect.DeepEqual(got, []string{"browse", "checkout"}) {
		t.Errorf("merged journeys = %v", got)
	}
}
