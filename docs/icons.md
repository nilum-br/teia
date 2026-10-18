# The teia icon library

Every map uses the same pictograms: one per **category** of component. The icon's color is the component's
**health** (green, amber, red, grey), and an amber or red card is outlined in that color too, with a triangle or a
cross next to the icon, so the icon tells *what it is* and the color and shape tell *how it is doing*.

teia draws its own icons instead of product logos or an icon kit, so the map looks like itself and the project
stays clear of trademark issues. The technology's name is written on the card (`PostgreSQL · 2.0/s`).

![The teia icon library](assets/icon-library.svg)

The image is generated from the map component itself (`node scripts/gen-docs-assets.mjs`), so it never drifts.

## Drawing a new icon

- A 20 px grid with 1 px of padding; the drawing fits one of the usual shapes (circle, square, upright or wide
  rectangle) so every icon looks the same size.
- A 1.5 px stroke with **square ends and mitred corners**, no fill. Corners of boxes are rounded by 1 px at most.
- A filled dot is a **unit of work**: a request, a message, a record, an event. It is the same dot that travels
  the map's lines.
- Datastores share the cylinder; what is unusual about one is drawn inside it or next to it.
- One concept per icon, named by the object, readable at 14 px. No letters, no unicode glyphs, no gradients and
  no colored tile behind the icon.
- Check it in the map at 1x, in both themes and in Grafana, next to the icons it will sit beside.

See [the map's visual language](design.md) for the lines, cards and colors around the icons.

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
