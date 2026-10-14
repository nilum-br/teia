package graph

import (
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

func TestApplyTech(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*model.Node{
			"orders-db":    {ID: "orders-db", Kind: model.KindDatabase, Labels: map[string]string{"image": "postgres:18.6-alpine"}},
			"cache":        {ID: "cache", Kind: model.KindUnknown, Labels: map[string]string{"image": "docker.io/bitnami/valkey:8"}},
			"order-events": {ID: "order-events", Kind: model.KindUnknown},
			"catalog-db":   {ID: "catalog-db", Kind: model.KindUnknown},
			"web":          {ID: "web", Kind: model.KindService, Labels: map[string]string{"teia.tech": "frontend", "image": "teia-lab/demo:dev"}},
			"report":       {ID: "report", Kind: model.KindService, Labels: map[string]string{"k8s.workload": "CronJob"}},
			"orders":       {ID: "orders", Kind: model.KindService, Labels: map[string]string{"image": "ghcr.io/acme/orders:1.2"}},
		},
		Edges: map[string]*model.Edge{
			"orders->order-events": {From: "orders", To: "order-events", Protocol: "kafka"},
			"catalog->catalog-db":  {From: "catalog", To: "catalog-db", Protocol: "mongodb"},
		},
	}
	ApplyTech(g)
	want := map[string][2]string{
		"orders-db":    {"postgresql", "database"},
		"cache":        {"valkey", "cache"},
		"order-events": {"kafka", "queue"},
		"catalog-db":   {"mongodb", "database"},
		"web":          {"frontend", "service"},
		"report":       {"job", "service"},
		"orders":       {"", "service"},
	}
	for id, w := range want {
		n := g.Nodes[id]
		if n.Tech != w[0] || string(n.Kind) != w[1] {
			t.Errorf("%s: tech %q kind %q, want %q %q", id, n.Tech, n.Kind, w[0], w[1])
		}
	}
}
