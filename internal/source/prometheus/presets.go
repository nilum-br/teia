package prometheus

import (
	"fmt"
	"strings"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

// Edge is what a preset reads out of one series.
type Edge struct {
	From, To         string
	FromKind, ToKind model.NodeKind
	// FromSelf/ToSelf: that end emitted the telemetry (its SDK, its sidecar,
	// an eBPF probe on its node), so teia sees it first-hand.
	FromSelf, ToSelf bool
	// FromNS/ToNS are Kubernetes namespaces when the metrics carry them.
	FromNS, ToNS string
	Protocol     string
}

// Result is the reading of one series.
type Result struct {
	Edge *Edge
	// UnknownCallerOf names a callee that received traffic from a caller the
	// metrics cannot identify (Istio's "unknown", the service graph "user").
	UnknownCallerOf string
	// UnnamedTargetOf names a caller whose callee the metrics cannot name.
	UnnamedTargetOf string
}

// Preset knows one family of metrics.
type Preset struct {
	Name        string
	Description string
	// Signal is the kind of telemetry behind the metrics (source.Signal*).
	Signal string
	// Detect is a series selector whose presence enables the preset in auto
	// mode. It is specific enough not to match look-alike metrics.
	Detect string
	// Requests returns one series per edge with its request (or connection)
	// rate; Failures, when set, the same grouping restricted to failures.
	Requests func(window string) string
	Failures func(window string) string
	// NoRate: the series value is not a request rate (bytes, flows), so it
	// proves the edge exists but is not reported as req/s.
	NoRate bool
	Map    func(labels map[string]string) Result
}

// Presets are tried in this order; earlier ones win on protocol and kind.
var Presets = []*Preset{
	servicegraphPreset,
	istioPreset, istioTCPPreset,
	linkerdPreset, linkerdTCPPreset,
	beylaNetworkPreset("beyla-network", "beyla_network_flow_bytes_total"),
	beylaNetworkPreset("obi-network", "obi_network_flow_bytes_total"),
	hubbleTCPPreset, hubbleHTTPPreset,
	retinaPreset,
}

// Lookup finds a preset by name.
func Lookup(name string) *Preset {
	for _, p := range Presets {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// Names lists every preset name.
func Names() []string {
	out := make([]string, len(Presets))
	for i, p := range Presets {
		out[i] = p.Name
	}
	return out
}

func dbKind(system string) model.NodeKind {
	switch strings.ToLower(system) {
	case "":
		return ""
	case "redis", "memcached", "valkey":
		return model.KindCache
	}
	return model.KindDatabase
}

// firstOf returns the first non-empty, non-"unknown" value among keys.
func firstOf(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" && v != "unknown" {
			return v
		}
	}
	return ""
}

// or joins the same aggregation over several selectors with PromQL "or",
// which keeps the first series for each label set: the way Kiali avoids
// counting an edge twice when both ends report it.
func or(by, metric, window string, selectors ...string) string {
	parts := make([]string, len(selectors))
	for i, sel := range selectors {
		parts[i] = fmt.Sprintf("sum by (%s) (rate(%s{%s}[%s]))", by, metric, sel, window)
	}
	return strings.Join(parts, " or ")
}

// --- traces: OpenTelemetry Collector service_graph / Tempo metrics-generator / Beyla

const (
	sgVirtualClient = "user"
	sgUnnamedServer = "unknown"
	sgLabels        = "client, server, connection_type, client_db_system, server_db_system, client_messaging_system, server_messaging_system, client_service_namespace, server_service_namespace"
)

var servicegraphPreset = &Preset{
	Name:        "servicegraph",
	Description: "service graph metrics built from traces (OpenTelemetry Collector service_graph connector, Tempo metrics-generator, Beyla/OBI)",
	Signal:      source.SignalTraces,
	Detect:      "traces_service_graph_request_total",
	Requests: func(w string) string {
		return fmt.Sprintf("sum by (%s) (rate(traces_service_graph_request_total[%s]))", sgLabels, w)
	},
	Failures: func(w string) string {
		return fmt.Sprintf("sum by (%s) (rate(traces_service_graph_request_failed_total[%s]))", sgLabels, w)
	},
	Map: func(m map[string]string) Result {
		client, server, ctype := m["client"], m["server"], m["connection_type"]
		if client == "" || server == "" {
			return Result{}
		}
		virtualClient := client == sgVirtualClient
		virtualServer := ctype == "virtual_node" && !virtualClient
		if virtualClient {
			return Result{UnknownCallerOf: server}
		}
		if virtualServer && server == sgUnnamedServer {
			return Result{UnnamedTargetOf: client}
		}
		// db.system and messaging.system are set on the client (producer)
		// span and describe the callee.
		system := firstOf(m, "client_db_system", "server_db_system")
		messaging := firstOf(m, "client_messaging_system", "server_messaging_system")
		e := &Edge{
			From: client, To: server, FromSelf: true, ToSelf: !virtualServer,
			FromNS: m["client_service_namespace"], ToNS: m["server_service_namespace"],
			ToKind: dbKind(system), Protocol: strings.ToLower(system),
		}
		if messaging != "" {
			e.ToKind, e.ToSelf, e.Protocol = model.KindQueue, false, strings.ToLower(messaging)
		}
		switch ctype {
		case "database":
			if e.ToKind == "" {
				e.ToKind = model.KindDatabase
			}
			e.ToSelf = false
		case "messaging_system":
			e.ToKind = model.KindQueue
			e.ToSelf = false
			if e.Protocol == "" {
				e.Protocol = "messaging"
			}
		}
		return Result{Edge: e}
	},
}

// --- mesh: Istio (sidecars, ambient ztunnel and waypoints)

const istioBy = "source_workload_namespace, source_workload, destination_service_namespace, destination_service_name, destination_workload_namespace, destination_workload, request_protocol"

var istioPreset = &Preset{
	Name:        "istio",
	Description: "Istio standard metrics (istio_requests_total) from sidecars and ambient waypoints",
	Signal:      source.SignalMesh,
	Detect:      "istio_requests_total",
	Requests: func(w string) string {
		return or(istioBy, "istio_requests_total", w,
			`reporter="source"`, `reporter="waypoint"`, `reporter="destination",source_workload="unknown"`)
	},
	Failures: func(w string) string {
		return or(istioBy, "istio_requests_total", w,
			`reporter="source",response_code=~"5..|0"`, `reporter="waypoint",response_code=~"5..|0"`,
			`reporter="destination",source_workload="unknown",response_code=~"5..|0"`)
	},
	Map: func(m map[string]string) Result { return istioMap(m, strings.ToLower(m["request_protocol"])) },
}

// istioTCPPreset covers plain TCP through the mesh (databases, brokers) and
// ambient mode without waypoints, where ztunnel only reports L4.
var istioTCPPreset = &Preset{
	Name:        "istio-tcp",
	Description: "Istio TCP metrics (istio_tcp_connections_opened_total): databases, brokers, ambient ztunnel",
	Signal:      source.SignalMesh,
	Detect:      "istio_tcp_connections_opened_total",
	Requests: func(w string) string {
		return or(istioBy, "istio_tcp_connections_opened_total", w,
			`reporter="source"`, `reporter="destination",source_workload="unknown"`)
	},
	Map: func(m map[string]string) Result { return istioMap(m, "tcp") },
}

// isWaypoint recognises ambient waypoint proxies by their usual names.
func isWaypoint(workload string) bool {
	return workload == "waypoint" || strings.HasSuffix(workload, "-waypoint")
}

func istioMap(m map[string]string, protocol string) Result {
	src, dst := firstOf(m, "source_workload"), firstOf(m, "destination_workload")
	if src == "" {
		// The caller has no proxy: Istio only knows the request arrived.
		if dst != "" {
			return Result{UnknownCallerOf: dst}
		}
		return Result{}
	}
	if isWaypoint(src) {
		return Result{} // second hop of an ambient waypoint, reported again end to end
	}
	e := &Edge{
		From: src, To: dst, FromSelf: true, ToSelf: true, Protocol: protocol,
		FromNS: firstOf(m, "source_workload_namespace"), ToNS: firstOf(m, "destination_workload_namespace", "destination_service_namespace"),
	}
	if dst == "" || isWaypoint(dst) {
		// Outside the mesh, or the first hop to a waypoint: name the callee
		// after the service the caller asked for.
		e.To, e.ToSelf = shortHost(firstOf(m, "destination_service_name")), dst != ""
		switch e.To {
		case "", "PassthroughCluster", "BlackHoleCluster":
			return Result{UnnamedTargetOf: src}
		}
	}
	return Result{Edge: e}
}

// shortHost turns "orders.shop.svc.cluster.local" into "orders"; external
// hosts such as "api.stripe.com" are kept whole.
func shortHost(h string) string {
	if i := strings.Index(h, ".svc."); i > 0 {
		h = h[:i]
		if j := strings.Index(h, "."); j > 0 {
			h = h[:j]
		}
	}
	return h
}

// --- mesh: Linkerd (outbound side only, so each request counts once)

const linkerdBy = "namespace, deployment, statefulset, daemonset, k8s_job, dst_namespace, dst_service, dst_deployment, dst_statefulset, dst_daemonset, dst_k8s_job"

var linkerdPreset = &Preset{
	Name:        "linkerd",
	Description: "Linkerd proxy metrics (response_total, outbound), with linkerd-viz scrape labels",
	Signal:      source.SignalMesh,
	Detect:      `response_total{direction="outbound",dst_namespace!=""}`,
	Requests: func(w string) string {
		return fmt.Sprintf(`sum by (%s) (rate(response_total{direction="outbound"}[%s]))`, linkerdBy, w)
	},
	Failures: func(w string) string {
		return fmt.Sprintf(`sum by (%s) (rate(response_total{direction="outbound",classification="failure"}[%s]))`, linkerdBy, w)
	},
	Map: func(m map[string]string) Result { return linkerdMap(m, "http") },
}

var linkerdTCPPreset = &Preset{
	Name:        "linkerd-tcp",
	Description: "Linkerd TCP connections (tcp_open_total, outbound)",
	Signal:      source.SignalMesh,
	Detect:      `tcp_open_total{direction="outbound",peer="dst",dst_namespace!=""}`,
	Requests: func(w string) string {
		return fmt.Sprintf(`sum by (%s) (rate(tcp_open_total{direction="outbound",peer="dst"}[%s]))`, linkerdBy, w)
	},
	Map: func(m map[string]string) Result { return linkerdMap(m, "tcp") },
}

func linkerdMap(m map[string]string, protocol string) Result {
	src := firstOf(m, "deployment", "statefulset", "daemonset", "k8s_job")
	dstWorkload := firstOf(m, "dst_deployment", "dst_statefulset", "dst_daemonset", "dst_k8s_job")
	dst := dstWorkload
	if dst == "" {
		dst = shortHost(firstOf(m, "dst_service"))
	}
	if src == "" || dst == "" {
		// Without linkerd-viz relabelling the caller workload is not a label.
		return Result{}
	}
	return Result{Edge: &Edge{
		From: src, To: dst, FromSelf: true, ToSelf: dstWorkload != "", Protocol: protocol,
		FromNS: m["namespace"], ToNS: m["dst_namespace"],
	}}
}

// --- network: Beyla / OBI eBPF flows

func beylaNetworkPreset(name, metric string) *Preset {
	const by = "k8s_src_namespace, k8s_src_owner_type, k8s_src_owner_name, k8s_dst_namespace, k8s_dst_owner_type, k8s_dst_owner_name"
	return &Preset{
		Name:        name,
		Description: fmt.Sprintf("eBPF network flows (%s, direction=request): any TCP/UDP talk, no SDK or mesh needed", metric),
		Signal:      source.SignalNetwork,
		Detect:      fmt.Sprintf(`%s{direction="request"}`, metric),
		NoRate:      true,
		Requests: func(w string) string {
			return fmt.Sprintf(`sum by (%s) (rate(%s{direction="request"}[%s]))`, by, metric, w)
		},
		Map: func(m map[string]string) Result {
			src, dst := firstOf(m, "k8s_src_owner_name"), firstOf(m, "k8s_dst_owner_name")
			if src == "" || dst == "" || m["k8s_src_owner_type"] == "Node" || m["k8s_dst_owner_type"] == "Node" {
				return Result{}
			}
			return Result{Edge: &Edge{
				From: src, To: dst, FromSelf: true, ToSelf: m["k8s_dst_owner_type"] != "Service",
				FromNS: m["k8s_src_namespace"], ToNS: m["k8s_dst_namespace"],
			}}
		},
	}
}

// --- network: Cilium Hubble (labelsContext with source_/destination_workload)

const hubbleBy = "source_namespace, source_workload, destination_namespace, destination_workload"

var hubbleTCPPreset = &Preset{
	Name:        "hubble-tcp",
	Description: "Cilium Hubble TCP SYNs (hubble_tcp_flags_total with labelsContext workloads)",
	Signal:      source.SignalNetwork,
	Detect:      `hubble_tcp_flags_total{flag="SYN",source_workload!=""}`,
	Requests: func(w string) string {
		return or(hubbleBy, "hubble_tcp_flags_total", w, `flag="SYN",traffic_direction="egress"`, `flag="SYN",traffic_direction="ingress"`)
	},
	Map: func(m map[string]string) Result {
		return workloadPair(m, "source_workload", "destination_workload", "source_namespace", "destination_namespace", "tcp")
	},
}

var hubbleHTTPPreset = &Preset{
	Name:        "hubble-http",
	Description: "Cilium Hubble L7 HTTP (hubble_http_requests_total with labelsContext workloads)",
	Signal:      source.SignalNetwork,
	Detect:      `hubble_http_requests_total{source_workload!=""}`,
	Requests: func(w string) string {
		return or(hubbleBy, "hubble_http_requests_total", w, `reporter="client"`, `reporter="server"`)
	},
	Failures: func(w string) string {
		return or(hubbleBy, "hubble_http_requests_total", w, `reporter="client",status=~"5.."`, `reporter="server",status=~"5.."`)
	},
	Map: func(m map[string]string) Result {
		return workloadPair(m, "source_workload", "destination_workload", "source_namespace", "destination_namespace", "http")
	},
}

// --- network: Microsoft Retina (pod-level, remote context)

var retinaPreset = &Preset{
	Name:        "retina",
	Description: "Microsoft Retina advanced metrics with remote context (networkobservability_adv_forward_count)",
	Signal:      source.SignalNetwork,
	Detect:      `networkobservability_adv_forward_count{source_workload_name!=""}`,
	NoRate:      true,
	Requests: func(w string) string {
		return fmt.Sprintf(`sum by (source_namespace, source_workload_name, destination_namespace, destination_workload_name) (rate(networkobservability_adv_forward_count{direction="EGRESS",is_reply="false"}[%s]))`, w)
	},
	Map: func(m map[string]string) Result {
		return workloadPair(m, "source_workload_name", "destination_workload_name", "source_namespace", "destination_namespace", "")
	},
}

func workloadPair(m map[string]string, fromKey, toKey, fromNS, toNS, protocol string) Result {
	src, dst := firstOf(m, fromKey), firstOf(m, toKey)
	if src == "" || dst == "" {
		return Result{}
	}
	return Result{Edge: &Edge{From: src, To: dst, FromSelf: true, ToSelf: true, FromNS: m[fromNS], ToNS: m[toNS], Protocol: protocol}}
}
