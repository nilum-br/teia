# Resource diagnostics and their limits

Teia attaches readings to identities already known to the graph. Exporter metrics do not discover client
connections or prove consumer ownership. A missing optional metric produces a `probe-capability` finding
with its prerequisites; no synthetic zero is added. Empty queries can also mean an inapplicable capability
or insufficient samples. A measured zero remains a reading. An optional diagnostic query failure retains
the base measurements and marks collection degraded. Capability findings describe the queried source;
they do not claim completeness for every instance when only some instances export a metric.

## Cache pressure

The Redis probe reads `increase(redis_evicted_keys_total[window])` and
`increase(redis_rejected_connections_total[window])`, in addition to memory/client saturation. It requires
the default `redis_` namespace and scrape labels naming the cache (`service`, `server`, `instance`, `pod`
or `job`). At least two samples are needed for these counter estimates. No key scanning, `MONITOR`, Lua
scripts or key values are requested. The mapping is defined in
[redis_exporter's metric map](https://github.com/oliver006/redis_exporter/blob/master/exporter/exporter.go).

Evictions can be intended behavior under a bounded cache policy. They are labelled pressure with a
qualified reason, not a failed-request percentage. Defaults are 1/100 evictions and 1/10 rejected
connections per source window, with global and targeted `health.evictions`/`health.rejections` overrides.
Review these budgets for the workload and window. A restart-reset counter is handled by `increase`;
original event time and oldest contributing sample remain unknown.

## Queues: three different quantities

Kafka's `kafka_consumergroup_lag` gives an approximate **count** by `consumergroup`, `topic` and partition.
The **drain estimate with no new arrivals** divides this count by the positive derivative of committed
offsets over the configured window. Offsets are gauges: rewinds/decreases or negative/unset offsets
suppress the estimate. Constant valid offsets with a positive backlog report a window without progress.
This does not locate the oldest message. It assumes the observed consumption slope continues and says
nothing about a finite catch-up time under continuing production. When arrivals equal/exceed consumption,
no finite catch-up estimate is justified. Teia does not currently publish that net-consumption estimate.
The [exporter's metric and ACL contract](https://github.com/danielqsj/kafka_exporter#metrics) requires group
and topic describe access; absent groups need not mean zero consumers.

RabbitMQ uses its built-in Prometheus plugin, with per-object scraping or `/metrics/detailed` including
`queue_coarse_metrics` for ready/unacknowledged counts and `queue_metrics` for head timestamps. These
families are documented for RabbitMQ 3.13 and current 4.3. The detailed name is preferred over the ordinary
name when both exist, avoiding double counting. The `vhost` and `queue` labels are required to retain queue
scope; nondefault virtual hosts use the `vhost/queue` identity and need an alias if the graph uses another
name. Configure one broker-cluster scope per Teia/Prometheus source to avoid colliding names across clusters.
[RabbitMQ's monitoring reference](https://www.rabbitmq.com/docs/prometheus) describes the families and cost.

The **head-message timestamp age (publisher clock)** is evaluation time minus a positive supplied head
timestamp. It is distinct from drain time and from the age of every queued message. Unsupported queue
types, missing publisher timestamps, paged-out heads, empty queues and future clocks do not become zero age.
RabbitMQ documents the optional [head timestamp property](https://www.rabbitmq.com/docs/man/rabbitmqctl.8#head_message_timestamp).
Scrape age
comes separately from raw metric timestamps. No queue name is guessed to be a dead-letter queue: the
operator must identify that queue and alias its regular depth reading. Broker-wide dead-letter counters
do not carry a per-queue ownership contract, so Teia does not attribute them to an arbitrary queue.

## PostgreSQL

The exporter probe reads `pg_stat_activity_max_tx_duration` as the oldest open transaction age and
`pg_replication_lag_seconds` only when `pg_replication_is_replica == 1`. The `stat_activity` and `replication`
collectors must be enabled and the exporter's login must see the relevant statistics (normally
`pg_monitor`/`pg_read_all_stats`). Maximums aggregate durations; they are not summed. The replica metric is
the exporter's estimate from receive/replay LSNs and last replay time, not the primary's acknowledgement
latency. Raw metric and applicability timestamps are required to claim sample age. See the upstream
[activity collector](https://github.com/prometheus-community/postgres_exporter/blob/master/collector/pg_stat_activity.go)
and [replication collector](https://github.com/prometheus-community/postgres_exporter/blob/master/collector/pg_replication.go).

The direct `postgres` explorer retains client discovery, connection saturation and lock blockers. With
statistics privileges, one additional read-only query reads the oldest client transaction and the maximum
non-null `pg_stat_replication.replay_lag` on the primary. These are labelled separately. A NULL replay delay
is explicitly unavailable/not applicable, including no replicas and idle/caught-up states; it is never
coalesced to zero. A transaction start measures transaction age, not the last call time. It does not renew
edge observation timestamps. See [PostgreSQL statistics semantics](https://www.postgresql.org/docs/current/monitoring-stats.html).

## Explorer decisions

| Candidate | Gap it could close | Access and cost | Decision |
|---|---|---|---|
| MySQL | Current sessions with client host/user when no tracing exists | A dedicated account needs `PROCESS` to see other users and the relevant Performance Schema access; reads scan current sessions. Avoid collecting SQL text. | Useful next explorer; exporter saturation/locks remain available today. Do not claim session discovery is implemented. |
| Redis | Current TCP clients and optional client names | `CLIENT LIST` needs explicit ACL permission and is O(number of connections); names are optional and proxies/NAT hide origins. Avoid keys and `MONITOR`. | Useful bounded, opt-in future explorer. Exporter client counts alone cannot identify clients. |
| Kafka | Active group members and assigned topics | Describe permissions for groups/topics; calls scale with groups/partitions, rebalance states can omit members, producer ownership remains unknown. | Evaluate an opt-in bounded group explorer; lag metrics alone are not ownership discovery. |

The access/cost assessment follows [MySQL processlist](https://dev.mysql.com/doc/refman/9.7/en/performance-schema-processlist-table.html),
[Redis CLIENT LIST](https://redis.io/docs/latest/commands/client-list/) and
[Kafka authorization](https://kafka.apache.org/42/security/authorization-and-acls/).

## Remaining unknown times

Raw RabbitMQ gauge timing now joins the other mapped gauge readings. PromQL rate/increase/derivative,
histogram quantiles, custom PromQL/LogQL and restart counts still do not establish the time of each input
or event. `timestamp(derived_expression)` would only date its evaluation and is not used for that purpose.
The PromQL `ts_of_first_over_time` family still requires an experimental feature flag; Teia does not enable
it globally or fabricate a range's first sample by subtracting the lookback. Direct database current-state
queries and inventory creation times likewise do not establish the last application call. See
[Prometheus function semantics](https://prometheus.io/docs/prometheus/latest/querying/functions/).

## Controlled validation

`TestResourcePromQL` runs the production expressions through an actual `promtool` binary supplied by
`PROMTOOL`. Fixtures cover growing lag, stopped consumers, offset rewinds/unset offsets, duplicate RabbitMQ
metric names, unacknowledged counts, invalid/empty/future head times, measured-zero client rejections,
evictions, oldest transactions and primary-versus-replica applicability. HTTP regressions separately check
missing metrics, optional query failure and evidence attached to count-based health reasons.

`TestResourceDiagnosticsPostgres`, enabled with `TEIA_TEST_POSTGRES_URL` pointing at a disposable database,
creates dedicated monitor/limited roles and transaction-scoped advisory locks. It checks a blocked client,
an old open transaction, rule/evidence provenance, NULL replication delay and restricted-role blindness.
It does not query or modify application tables. These integrations were run with Prometheus/promtool
3.15.0 and PostgreSQL 18.6. CI's `resource-integration` job supplies both tools explicitly, waits for the
database's TCP healthcheck and runs these cases with the race detector and cache disabled. Ordinary local
Go tests skip only the integrations when those explicit fixtures are absent.
