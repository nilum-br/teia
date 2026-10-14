package advisor

import (
	"fmt"
	"strings"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
)

// ref renders a node as the Backstage-style ref used in flows files.
func ref(g *graph.Graph, id string) string {
	n := g.Nodes[id]
	if n != nil && n.Labels["backstage.ref"] != "" {
		return n.Labels["backstage.ref"]
	}
	kind := model.KindUnknown
	if n != nil {
		kind = n.Kind
	}
	switch kind {
	case model.KindExternal:
		return "external:" + id
	case model.KindDatabase, model.KindCache, model.KindQueue:
		return "resource:default/" + id
	default:
		return "component:default/" + id
	}
}

func egressEntry(g *graph.Graph, e model.Edge) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- to: %s\n", ref(g, e.To))
	if n := g.Nodes[e.To]; n != nil && n.Kind != model.KindService && n.Kind != model.KindUnknown {
		fmt.Fprintf(&b, "  kind: %s\n", n.Kind)
	}
	if e.Protocol != "" {
		fmt.Fprintf(&b, "  protocol: %s\n", e.Protocol)
	}
	b.WriteString("  reason: TODO why this call exists\n")
	return b.String()
}

// FlowsFile generates a flows file for owner from observed edges.
func FlowsFile(g *graph.Graph, owner string, edges []model.Edge) string {
	var b strings.Builder
	fmt.Fprintf(&b, `apiVersion: teia.nilum.io/v1alpha1
kind: Flows
metadata:
  name: %s
spec:
  owner: %s
  egress:
`, owner, ref(g, owner))
	for _, e := range edges {
		for _, line := range strings.Split(strings.TrimRight(egressEntry(g, e), "\n"), "\n") {
			b.WriteString("    " + line + "\n")
		}
	}
	return b.String()
}
