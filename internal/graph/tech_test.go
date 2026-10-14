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

func TestTechFromImage(t *testing.T) {
	cases := []struct {
		image, tech string // tech "" means not recognised
	}{
		// the technology itself, from any registry, with any tag or digest
		{"postgres:18.6-alpine", "postgresql"},
		{"docker.io/bitnami/postgresql:16", "postgresql"},
		{"ghcr.io/cloudnative-pg/postgresql:17.2", "postgresql"},
		{"registry.local:5000/redis", "redis"},
		{"redis@sha256:0123abcd", "redis"},
		{"Docker.io/Library/Mongo:8", "mongodb"},
		{"rabbitmq:4-management", "rabbitmq"},
		{"traefik:v3.5", "traefik"},
		{"haproxy:3.2", "haproxy"},
		// flavours that start with the technology
		{"redis/redis-stack-server:7.4", "redis"},
		{"timescale/timescaledb-ha:pg17", "postgresql"},
		{"mongodb/mongodb-community-server:8.0-ubi9", "mongodb"},
		{"clickhouse/clickhouse-server:25.8", "clickhouse"},
		{"apache/kafka-native:4.1", "kafka"},
		{"bitnami/redis-sentinel:8", "redis"},
		// known vendor variants
		{"confluentinc/cp-kafka:8.0", "kafka"},
		{"confluentinc/cp-server:8.0", "kafka"},
		{"percona/percona-server-mongodb:8.0", "mongodb"},
		{"pgvector/pgvector:pg17", "postgresql"},
		{"mcr.microsoft.com/mssql/server:2022-latest", "mssql"},
		// companions that carry the name without being it
		{"quay.io/prometheuscommunity/postgres-exporter:v0.18", ""},
		{"oliver006/redis_exporter:v1.77", ""},
		{"provectuslabs/kafka-ui:latest", ""},
		{"mongo-express:1", ""},
		{"rediscommander/redis-commander:latest", ""},
		{"example/pg-bouncer:1.24", ""},
		{"bitnami/pgbouncer:1.24", ""},
		{"dpage/pgadmin4:9", ""},
		{"opensearchproject/opensearch-dashboards:3", ""},
		{"bitnami/rabbitmq-cluster-operator:2", ""},
		{"bitnami/keycloak-config-cli:6", ""},
		{"confluentinc/cp-kafka-connect:8.0", ""},
		{"mysql/mysql-router:8.0", ""},
		{"natsio/nats-box:0.18", ""},
		{"ghcr.io/acme/kafka-consumer:1.4", ""},
		{"ghcr.io/cloudnative-pg/cloudnative-pg:1.27", ""},
		// workload shapes and plain apps
		{"ghcr.io/acme/orders:1.2", ""},
		{"acme/api:1", ""},
		{"acme/web:1", ""},
		{"", ""},
	}
	for _, c := range cases {
		got, ok := techFromImage(c.image)
		if got.ID != c.tech || ok != (c.tech != "") {
			t.Errorf("techFromImage(%q) = %q, %v; want %q", c.image, got.ID, ok, c.tech)
		}
	}
}

func TestVendorImagesAreKnownTechs(t *testing.T) {
	for image, id := range vendorImages {
		if _, ok := techs[id]; !ok {
			t.Errorf("vendorImages[%q] = %q is not a known technology", image, id)
		}
	}
}
