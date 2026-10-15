package kubernetes

import (
	"strings"
	"testing"

	"github.com/nilum-br/teia/internal/model"
)

func TestInventoryDistinguishesExplicitAndInferredNamesPerPod(t *testing.T) {
	obs := discover(t, inventoryClient(t,
		pod("shop", "orders-hash-a", "10.0.0.1", ctrl("ReplicaSet", "orders-hash"), map[string]string{"pod-template-hash": "hash"}, nil),
		pod("shop", "orders-hash-b", "10.0.0.2", ctrl("ReplicaSet", "orders-hash"), map[string]string{"pod-template-hash": "hash"}, nil),
		pod("other", "db-0", "10.0.0.3", ctrl("StatefulSet", "db"), nil, map[string]string{annotationPrefix + "node-id": "other/db"}),
		pod("shop", "worker-0", "10.0.0.4", ctrl("StatefulSet", "worker"), nil, nil),
	))
	if len(obs.Nodes) != 4 {
		t.Fatalf("nodes = %+v", obs.Nodes)
	}
	for _, node := range obs.Nodes {
		switch node.ID {
		case "orders":
			if node.IdentityMethod != model.IdentityInferred || !strings.Contains(node.IdentityBasis, "owner object was not read") {
				t.Fatalf("derived Deployment presented as explicit identity: %+v", node)
			}
			podName := "orders-hash-a"
			if node.Addresses[0] == "10.0.0.2" {
				podName = "orders-hash-b"
			}
			if !strings.Contains(node.IdentityBasis, "shop/"+podName) {
				t.Fatalf("another pod's provenance copied: %+v", node)
			}
		case "other/db":
			if node.IdentityMethod != model.IdentityExplicit || !strings.Contains(node.IdentityBasis, annotationPrefix+"node-id") {
				t.Fatalf("explicit annotation lost: %+v", node)
			}
		case "worker":
			if node.IdentityMethod != model.IdentityName {
				t.Fatalf("reported owner name = %+v", node)
			}
		}
	}
}
