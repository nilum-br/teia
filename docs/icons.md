# The teia icon library

Every map uses the same pictograms: one per **category** of component, drawn on the same 24 px grid with the
same 1.8 px stroke. The card's tile color is the component's **health** (green, amber, red, grey), so the icon
tells *what it is* and the color tells *how it is doing*.

teia draws its own icons instead of product logos, so the map looks consistent and the project stays clear of
trademark issues. The technology's name is written on the card (`PostgreSQL · 2.0/s`).

![The teia icon library](assets/icon-library.svg)

The image is generated from the map component itself (`python scripts/gen-docs-assets.py`), so it never drifts.

| Category | Technologies recognised |
|---|---|
| Microservice | any service (default) |
| Frontend | `teia.tech: frontend` |
| Gateway / proxy | NGINX, Envoy, HAProxy, Traefik, Kong, APISIX |
| Job / worker | Kubernetes Jobs and CronJobs, `teia.tech: job` |
| Function | `teia.tech: function` (Lambda and friends) |
| Relational database | PostgreSQL, MySQL, MariaDB, SQL Server, Oracle, Db2, SQLite, CockroachDB, TimescaleDB |
| Document database (NoSQL) | MongoDB, CouchDB/Couchbase, DynamoDB, Cosmos DB, Firestore |
| Wide-column database | Cassandra, ScyllaDB, HBase, Bigtable |
| Search engine | Elasticsearch, OpenSearch, Solr, Meilisearch |
| Time-series / analytics | ClickHouse, InfluxDB, Druid |
| Graph database | Neo4j, Neptune |
| Cache / key-value | Redis, Valkey, Memcached, KeyDB, Dragonfly, Hazelcast |
| Event stream | Kafka (incl. MSK, Confluent), Redpanda, Kinesis, Pulsar, Event Hubs |
| Message queue | RabbitMQ, SQS, SNS, ActiveMQ, NATS, Pub/Sub, Service Bus |
| Object storage | S3, MinIO, Cloud Storage, Blob Storage |
| Identity | Keycloak, Auth0, Okta, Dex |
| External / SaaS | anything outside the platform |

Open **icon library** in the map's legend to see them all.

## How teia knows the technology

In order of trust:

1. **An explicit label**: `teia.tech: postgresql` on a container, or the `teia.nilum.io/tech` annotation on a pod.
2. **How others reach it**: `db.system` and `messaging.system` on client spans, which is how managed services
   (Atlas, MSK, RDS) show up.
3. **Its image**: `postgres:18`, `bitnami/redis`, `confluentinc/cp-kafka`.
4. **Its workload shape**: Kubernetes Jobs and CronJobs are jobs.

The full list of names lives in [`internal/graph/tech.go`](../internal/graph/tech.go) and the drawings in
[`internal/api/ui/teia-ui.js`](../internal/api/ui/teia-ui.js) (`CATEGORY`). Adding a technology is one line in
each, and contributions are welcome.
