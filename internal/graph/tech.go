package graph

import (
	"regexp"
	"strings"

	"github.com/nilum-br/teia/internal/model"
)

// Tech is a technology teia recognises: its category drives the icon, its
// kind refines the node kind when sources only knew "unknown".
type Tech struct {
	ID       string // stable id, e.g. "postgresql"
	Category string // icon family, e.g. "relational"
	Kind     model.NodeKind
}

// Categories, one icon each in the UI.
const (
	CatService    = "service"
	CatFrontend   = "frontend"
	CatGateway    = "gateway"
	CatJob        = "job"
	CatFunction   = "function"
	CatRelational = "relational"
	CatDocument   = "document"
	CatWideColumn = "widecolumn"
	CatSearch     = "search"
	CatTimeSeries = "timeseries"
	CatGraphDB    = "graphdb"
	CatCache      = "cache"
	CatStream     = "stream"
	CatQueue      = "queue"
	CatStorage    = "storage"
	CatIdentity   = "identity"
	CatExternal   = "external"
)

// techs maps the names technologies go by (db.system and messaging.system
// values, image names, common aliases) to a technology.
var techs = func() map[string]Tech {
	m := map[string]Tech{}
	add := func(cat string, kind model.NodeKind, names ...string) {
		for _, n := range names {
			m[n] = Tech{ID: names[0], Category: cat, Kind: kind}
		}
	}
	add(CatRelational, model.KindDatabase, "postgresql", "postgres", "pg", "pgsql", "timescaledb", "cockroachdb", "cockroach", "yugabytedb")
	add(CatRelational, model.KindDatabase, "mysql", "percona")
	add(CatRelational, model.KindDatabase, "mariadb")
	add(CatRelational, model.KindDatabase, "mssql", "sqlserver", "microsoft.sql_server")
	add(CatRelational, model.KindDatabase, "oracle", "oracledb")
	add(CatRelational, model.KindDatabase, "db2", "ibm.db2")
	add(CatRelational, model.KindDatabase, "sqlite")
	add(CatDocument, model.KindDatabase, "mongodb", "mongo")
	add(CatDocument, model.KindDatabase, "couchdb", "couchbase")
	add(CatDocument, model.KindDatabase, "dynamodb", "aws.dynamodb")
	add(CatDocument, model.KindDatabase, "cosmosdb", "azure.cosmosdb")
	add(CatDocument, model.KindDatabase, "firestore")
	add(CatWideColumn, model.KindDatabase, "cassandra")
	add(CatWideColumn, model.KindDatabase, "scylladb", "scylla")
	add(CatWideColumn, model.KindDatabase, "hbase")
	add(CatWideColumn, model.KindDatabase, "bigtable")
	add(CatSearch, model.KindDatabase, "elasticsearch", "elastic")
	add(CatSearch, model.KindDatabase, "opensearch")
	add(CatSearch, model.KindDatabase, "solr")
	add(CatSearch, model.KindDatabase, "meilisearch")
	add(CatTimeSeries, model.KindDatabase, "clickhouse")
	add(CatTimeSeries, model.KindDatabase, "influxdb", "influx")
	add(CatTimeSeries, model.KindDatabase, "druid")
	add(CatGraphDB, model.KindDatabase, "neo4j")
	add(CatGraphDB, model.KindDatabase, "neptune")
	add(CatCache, model.KindCache, "redis")
	add(CatCache, model.KindCache, "valkey")
	add(CatCache, model.KindCache, "memcached")
	add(CatCache, model.KindCache, "keydb")
	add(CatCache, model.KindCache, "dragonfly", "dragonflydb")
	add(CatCache, model.KindCache, "hazelcast")
	add(CatStream, model.KindQueue, "kafka", "confluent", "msk")
	add(CatStream, model.KindQueue, "redpanda")
	add(CatStream, model.KindQueue, "kinesis", "aws.kinesis")
	add(CatStream, model.KindQueue, "pulsar")
	add(CatStream, model.KindQueue, "eventhubs", "azure.eventhubs")
	add(CatQueue, model.KindQueue, "rabbitmq", "amqp")
	add(CatQueue, model.KindQueue, "sqs", "aws.sqs", "aws_sqs")
	add(CatQueue, model.KindQueue, "sns", "aws.sns")
	add(CatQueue, model.KindQueue, "activemq", "artemis")
	add(CatQueue, model.KindQueue, "nats", "jetstream")
	add(CatQueue, model.KindQueue, "pubsub", "gcp_pubsub", "gcp.pubsub")
	add(CatQueue, model.KindQueue, "servicebus", "azure.servicebus")
	add(CatStorage, model.KindDatabase, "s3", "aws.s3")
	add(CatStorage, model.KindDatabase, "minio")
	add(CatStorage, model.KindDatabase, "gcs")
	add(CatStorage, model.KindDatabase, "azureblob")
	add(CatGateway, model.KindService, "nginx", "ingress-nginx")
	add(CatGateway, model.KindService, "envoy")
	add(CatGateway, model.KindService, "haproxy")
	add(CatGateway, model.KindService, "traefik")
	add(CatGateway, model.KindService, "kong")
	add(CatGateway, model.KindService, "apisix")
	add(CatIdentity, model.KindService, "keycloak")
	add(CatIdentity, model.KindService, "auth0")
	add(CatIdentity, model.KindService, "okta")
	add(CatIdentity, model.KindService, "dex")
	// workload shapes rather than products
	add(CatFrontend, model.KindService, "frontend", "web", "spa", "bff")
	add(CatJob, model.KindService, "job", "cronjob", "batch", "worker")
	add(CatFunction, model.KindService, "function", "lambda", "aws.lambda", "faas")
	add(CatService, model.KindService, "service", "microservice", "api")
	add(CatExternal, model.KindExternal, "external", "saas")
	return m
}()

// LookupTech finds a technology by any of its names.
func LookupTech(name string) (Tech, bool) {
	t, ok := techs[strings.ToLower(strings.TrimSpace(name))]
	return t, ok
}

var imageWord = regexp.MustCompile(`[a-z0-9]+(?:\.[a-z0-9]+)?`)

// techFromImage reads a container image reference such as
// "docker.io/bitnami/postgresql:16" or "redis:8.2-alpine".
func techFromImage(image string) (Tech, bool) {
	image = strings.ToLower(image)
	if i := strings.LastIndex(image, "/"); i >= 0 {
		image = image[i+1:]
	}
	if i := strings.IndexAny(image, ":@"); i >= 0 {
		image = image[:i]
	}
	if t, ok := techs[image]; ok && t.Category != CatService && t.Category != CatFrontend && t.Category != CatJob {
		return t, true
	}
	for _, w := range imageWord.FindAllString(image, -1) {
		if t, ok := techs[w]; ok && t.Category != CatService && t.Category != CatFrontend && t.Category != CatJob && t.Category != CatExternal {
			return t, true
		}
	}
	return Tech{}, false
}

// ApplyTech sets each node's technology from, in order of trust: an explicit
// teia.tech label, the protocol other services use to reach it (db.system,
// messaging.system), its container image, and the workload shape inventory
// reports (Kubernetes jobs). It also turns "unknown" kinds into the kind the
// technology implies.
func ApplyTech(g *Graph) {
	inbound := map[string][]string{}
	for _, e := range g.Edges {
		if e.Protocol != "" {
			inbound[e.To] = append(inbound[e.To], e.Protocol)
		}
	}
	for id, n := range g.Nodes {
		t, ok := detectTech(n, inbound[id])
		if !ok {
			continue
		}
		n.Tech = t.ID
		if n.Kind == model.KindUnknown || n.Kind == "" {
			n.Kind = t.Kind
		}
	}
}

func detectTech(n *model.Node, protocols []string) (Tech, bool) {
	if t, ok := LookupTech(n.Labels["teia.tech"]); ok {
		return t, true
	}
	for _, p := range protocols {
		if t, ok := LookupTech(p); ok && t.Category != CatService && t.Category != CatFrontend {
			return t, true
		}
	}
	if t, ok := techFromImage(n.Labels["image"]); ok {
		return t, true
	}
	switch n.Labels["k8s.workload"] {
	case "CronJob", "Job":
		return techs["job"], true
	}
	return Tech{}, false
}
