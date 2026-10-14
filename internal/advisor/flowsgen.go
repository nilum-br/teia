package advisor

import (
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nilum-br/teia/internal/graph"
	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source/declared"
)

// todoReason is the placeholder the owning team replaces with the reason.
const todoReason = "TODO why this call exists"

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

func flow(g *graph.Graph, e model.Edge) declared.Flow {
	f := declared.Flow{To: ref(g, e.To), Protocol: e.Protocol, Reason: todoReason}
	if n := g.Nodes[e.To]; n != nil && n.Kind != model.KindService && n.Kind != model.KindUnknown {
		f.Kind = string(n.Kind)
	}
	return f
}

// egressEntry renders one item to add under spec.egress.
func egressEntry(g *graph.Graph, e model.Edge) string {
	return marshal([]declared.Flow{flow(g, e)})
}

// FlowsFile generates a flows file for owner from observed edges.
func FlowsFile(g *graph.Graph, owner string, edges []model.Edge) string {
	doc := declared.Document{
		APIVersion: declared.APIVersion,
		Kind:       declared.KindFlows,
		Metadata:   declared.Metadata{Name: owner},
		Spec:       declared.Spec{Owner: ref(g, owner)},
	}
	for _, e := range edges {
		doc.Spec.Egress = append(doc.Spec.Egress, flow(g, e))
	}
	return marshal(doc)
}

// marshal renders v with the YAML encoder rather than a template: names come
// from telemetry, and a ':', '#' or newline in one must stay inside its
// value instead of reshaping the file teams are asked to commit.
func marshal(v any) string {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	err := enc.Encode(v)
	if err == nil {
		err = enc.Close()
	}
	if err != nil {
		// Only unsupported Go types fail, and these are plain structs.
		return "# teia could not render this snippet: " + err.Error() + "\n"
	}
	return b.String()
}
