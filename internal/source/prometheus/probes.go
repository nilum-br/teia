package prometheus

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

// A Probe reads the health of one kind of component (a cache, a broker, a
// database, containers) from the metrics its standard exporter publishes,
// so the map can say why a node is amber: memory near its limit, a consumer
// minutes behind, sessions waiting on locks.
type Probe struct {
	Name        string
	Description string
	// Detect is a series selector whose presence enables the probe in auto
	// mode.
	Detect   string
	Readings []ProbeReading
}

// ProbeReading is one measure a probe takes, per subject.
type ProbeReading struct {
	Cause model.Cause
	// What names the measure; "%s" is replaced by the window ("restarts in
	// the last %s").
	What string
	// Query returns one series per subject with the value; Max, when set,
	// the capacity per subject, grouped by the same labels. Share says the
	// value is already a share of capacity (0..1).
	Query func(w window) string
	Max   func(w window) string
	Share bool
	// Unit of the value: model.UnitBytes, model.UnitSeconds, or empty.
	Unit string
	// Subject names the node the series measures (candidates, best first)
	// and, for an edge, its target.
	Subject func(labels map[string]string) (candidates, to []string)
}

// window is the probe's look-back, as PromQL and in seconds.
type window struct {
	promql  string
	seconds float64
}

// Probes are tried in this order in auto mode.
var Probes = []*Probe{
	redisProbe, kafkaProbe, rabbitmqProbe,
	mongodbProbe, mysqlProbe, postgresExporterProbe,
	containersProbe, restartsProbe,
}

// LookupProbe finds a probe by name.
func LookupProbe(name string) *Probe {
	for _, p := range Probes {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// ProbeNames lists every probe name.
func ProbeNames() []string {
	out := make([]string, len(Probes))
	for i, p := range Probes {
		out[i] = p.Name
	}
	return out
}

// exporterBy groups an exporter's series by the labels a scrape gives its
// target, so a value and its capacity join on the same set.
const exporterBy = "instance, job, namespace, service, pod, container, server"

func byExporter(expr string) string { return fmt.Sprintf("sum by (%s) (%s)", exporterBy, expr) }

// --- caches

var redisProbe = &Probe{
	Name:        "redis",
	Description: "Redis, Valkey, KeyDB through redis_exporter: memory against maxmemory, clients against maxclients",
	Detect:      "redis_memory_used_bytes",
	Readings: []ProbeReading{
		{Cause: model.CauseSaturation, What: "memory", Unit: model.UnitBytes,
			Query: func(window) string { return byExporter("redis_memory_used_bytes") },
			Max:   func(window) string { return byExporter("redis_memory_max_bytes > 0") }, Subject: exporterSubject},
		{Cause: model.CauseSaturation, What: "clients",
			Query: func(window) string { return byExporter("redis_connected_clients") },
			Max:   func(window) string { return byExporter("redis_config_maxclients > 0") }, Subject: exporterSubject},
	},
}

// --- brokers

const kafkaBy = "consumergroup, topic"

var kafkaProbe = &Probe{
	Name:        "kafka",
	Description: "Kafka consumer groups through kafka_exporter: messages waiting, time behind, no progress",
	Detect:      "kafka_consumergroup_lag",
	Readings: []ProbeReading{
		// On the line from the consumer group to its topic.
		{Cause: model.CauseBacklog, What: "messages waiting", Subject: consumerSubject,
			Query: func(window) string { return fmt.Sprintf("sum by (%s) (kafka_consumergroup_lag)", kafkaBy) }},
		// Lag divided by how fast the group consumes: how long it would take
		// to catch up, which is how far behind it is.
		{Cause: model.CauseLag, What: "behind", Unit: model.UnitSeconds, Subject: consumerSubject,
			Query: func(w window) string {
				return fmt.Sprintf("sum by (%[1]s) (kafka_consumergroup_lag) / sum by (%[1]s) (rate(kafka_consumergroup_current_offset[%[2]s])) > 0",
					kafkaBy, w.promql)
			}},
		// Waiting messages and an offset that did not move over the window.
		{Cause: model.CauseLag, What: "without progress", Unit: model.UnitSeconds, Subject: consumerSubject,
			Query: func(w window) string {
				return fmt.Sprintf("(sum by (%[1]s) (kafka_consumergroup_lag) > 0 and sum by (%[1]s) (increase(kafka_consumergroup_current_offset[%[2]s])) == 0) * 0 + %[3]g",
					kafkaBy, w.promql, w.seconds)
			}},
		// On the topic itself, every group's lag together.
		{Cause: model.CauseBacklog, What: "messages waiting",
			Query: func(window) string { return "sum by (topic) (kafka_consumergroup_lag)" },
			Subject: func(m map[string]string) ([]string, []string) {
				return nonEmpty(m["topic"]), nil
			}},
	},
}

func consumerSubject(m map[string]string) ([]string, []string) {
	return nonEmpty(m["consumergroup"]), nonEmpty(m["topic"])
}

var rabbitmqQueue = `{__name__=~"rabbitmq_(detailed_)?queue_messages_ready"}`

var rabbitmqProbe = &Probe{
	Name:        "rabbitmq",
	Description: "RabbitMQ queues through its built-in Prometheus plugin (per-object metrics): messages waiting",
	Detect:      rabbitmqQueue,
	Readings: []ProbeReading{
		{Cause: model.CauseBacklog, What: "messages waiting",
			Query: func(window) string { return "sum by (vhost, queue) (" + rabbitmqQueue + ")" },
			Subject: func(m map[string]string) ([]string, []string) {
				return nonEmpty(m["queue"]), nil
			}},
	},
}

// --- databases

var mongodbProbe = &Probe{
	Name:        "mongodb",
	Description: "MongoDB through mongodb_exporter: connections against those available, operations queued on locks",
	Detect:      "mongodb_ss_connections",
	Readings: []ProbeReading{
		{Cause: model.CauseSaturation, What: "connections",
			Query: func(window) string { return byExporter(`mongodb_ss_connections{conn_type="current"}`) },
			Max:   func(window) string { return byExporter(`mongodb_ss_connections{conn_type=~"current|available"}`) }, Subject: exporterSubject},
		{Cause: model.CauseLocks, What: "operations queued on locks",
			Query:   func(window) string { return byExporter(`mongodb_ss_globalLock_currentQueue{count_type="total"}`) },
			Subject: exporterSubject},
	},
}

var mysqlProbe = &Probe{
	Name:        "mysql",
	Description: "MySQL and MariaDB through mysqld_exporter: connections against max_connections, row lock waits",
	Detect:      "mysql_global_status_threads_connected",
	Readings: []ProbeReading{
		{Cause: model.CauseSaturation, What: "connections",
			Query: func(window) string { return byExporter("mysql_global_status_threads_connected") },
			Max:   func(window) string { return byExporter("mysql_global_variables_max_connections") }, Subject: exporterSubject},
		{Cause: model.CauseLocks, What: "row lock waits",
			Query:   func(window) string { return byExporter("mysql_global_status_innodb_row_lock_current_waits") },
			Subject: exporterSubject},
	},
}

var postgresExporterProbe = &Probe{
	Name:        "postgres-exporter",
	Description: "PostgreSQL through postgres_exporter: connections against max_connections (the postgres explorer also sees locks)",
	Detect:      "pg_stat_activity_count",
	Readings: []ProbeReading{
		{Cause: model.CauseSaturation, What: "connections",
			Query: func(window) string { return byExporter("pg_stat_activity_count") },
			Max: func(window) string {
				return fmt.Sprintf("max by (%s) (pg_settings_max_connections)", exporterBy)
			}, Subject: exporterSubject},
	},
}

// --- workloads

const containerBy = "namespace, pod, container, name, container_label_com_docker_compose_service"

var containersProbe = &Probe{
	Name:        "containers",
	Description: "containers through cAdvisor (Kubernetes or Docker): CPU throttled by its limit, memory against its limit",
	Detect:      `container_cpu_usage_seconds_total{image!=""}`,
	Readings: []ProbeReading{
		{Cause: model.CauseSaturation, What: "CPU time throttled", Share: true, Subject: workloadSubject,
			Query: func(w window) string {
				return fmt.Sprintf(`sum by (%[1]s) (rate(container_cpu_cfs_throttled_periods_total{image!=""}[%[2]s])) / sum by (%[1]s) (rate(container_cpu_cfs_periods_total{image!=""}[%[2]s]))`,
					containerBy, w.promql)
			}},
		{Cause: model.CauseSaturation, What: "memory", Unit: model.UnitBytes, Subject: workloadSubject,
			Query: func(window) string {
				return fmt.Sprintf(`sum by (%s) (container_memory_working_set_bytes{image!=""})`, containerBy)
			},
			Max: func(window) string {
				return fmt.Sprintf(`sum by (%s) (container_spec_memory_limit_bytes{image!=""} > 0)`, containerBy)
			}},
	},
}

var restartsProbe = &Probe{
	Name:        "restarts",
	Description: "Kubernetes container restarts through kube-state-metrics",
	Detect:      "kube_pod_container_status_restarts_total",
	Readings: []ProbeReading{
		{Cause: model.CauseRestarts, What: "restarts in the last %s", Subject: workloadSubject,
			Query: func(w window) string {
				return fmt.Sprintf("sum by (namespace, pod) (increase(kube_pod_container_status_restarts_total[%s])) > 0", w.promql)
			}},
	},
}

// --- naming the subject

// exporterSuffix is what exporters add to the name of what they measure.
var exporterSuffix = regexp.MustCompile(`([-_](prometheus[-_])?(exporter|metrics|monitor))+$`)

// stripExporter turns "orders-db-exporter", "prometheus-redis-exporter" or
// "catalog-cache-metrics" into the name of what is measured.
func stripExporter(name string) string {
	return strings.TrimPrefix(exporterSuffix.ReplaceAllString(name, ""), "prometheus-")
}

// exporterSubject guesses which node an exporter's series measures, from the
// labels a scrape gives it: the Kubernetes service and pod, the instance's
// host, the job. Names without the exporter's suffix come first, so the
// database wins over its exporter when both are on the map; an address comes
// last, and resolves to a node when the exporter is a sidecar.
func exporterSubject(m map[string]string) ([]string, []string) {
	var named, raw, addresses []string
	add := func(v string) {
		if v == "" {
			return
		}
		named = append(named, stripExporter(v))
		raw = append(raw, v)
	}
	add(m["server"])
	add(m["service"])
	if host := hostOf(m["instance"]); net.ParseIP(host) != nil {
		addresses = append(addresses, source.AddressID(host))
	} else {
		add(host)
	}
	add(workloadOf(m["pod"]))
	add(m["job"])
	return nonEmpty(append(append(named, raw...), addresses...)...), nil
}

// workloadSubject names the workload of a container: its Compose service on
// Docker, its Deployment or StatefulSet (from the pod name) on Kubernetes,
// then the container's own name.
func workloadSubject(m map[string]string) ([]string, []string) {
	return nonEmpty(m["container_label_com_docker_compose_service"], workloadOf(m["pod"]), m["container"], m["name"]), nil
}

// Kubernetes writes the random parts of pod names (a ReplicaSet's template
// hash, the generated suffix) with consonants and digits only, so a word such
// as "exporter" is never mistaken for one.
var (
	deploymentPod  = regexp.MustCompile(`-[bcdfghjklmnpqrstvwxz2456789]{6,10}-[bcdfghjklmnpqrstvwxz2456789]{5}$`)
	statefulSetPod = regexp.MustCompile(`-[0-9]+$`)
	daemonSetPod   = regexp.MustCompile(`-[bcdfghjklmnpqrstvwxz2456789]{5}$`)
)

// workloadOf turns a pod name into its workload's: "orders-7d9f8c6b5-x2k4p"
// into "orders", "orders-db-0" into "orders-db".
func workloadOf(pod string) string {
	for _, re := range []*regexp.Regexp{deploymentPod, statefulSetPod, daemonSetPod} {
		if w := re.ReplaceAllString(pod, ""); w != pod {
			return w
		}
	}
	return pod
}

// hostOf drops the port from an instance label.
func hostOf(instance string) string {
	if host, _, err := net.SplitHostPort(instance); err == nil {
		return host
	}
	return instance
}

// nonEmpty keeps the non-empty values, once each, in order.
func nonEmpty(values ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
