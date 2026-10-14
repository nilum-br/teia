package main

// Lab-only stand-ins for two signals real platforms already have:
//
//   - an access log line per request (what ingress controllers, web servers
//     and frameworks write), shipped to Loki by Alloy;
//   - Istio sidecar telemetry (istio_requests_total), for the part of the
//     shop that runs "in the mesh". The labels follow Istio's standard
//     metrics, including how Envoy reports peers outside the mesh as
//     "unknown". Peer identity, which Istio learns through mTLS metadata
//     exchange, travels here in an X-Sim-Istio-Workload header.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	peerHeader   = "X-Sim-Istio-Workload"
	peerNSHeader = "X-Sim-Istio-Namespace"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// accessLog writes one JSON line per request to stdout.
func accessLog(next http.Handler) http.Handler {
	if env("ACCESS_LOG", "on") != "on" {
		return next
	}
	enc := json.NewEncoder(os.Stdout)
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/metrics" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(map[string]any{
			"msg": "access", "remote_addr": r.RemoteAddr, "method": r.Method,
			"path": r.URL.Path, "status": rec.status,
		})
	})
}

// sidecar simulates an Istio sidecar next to this workload.
type sidecar struct {
	workload, namespace string

	mu     sync.Mutex
	counts map[string]float64 // rendered label set -> requests
}

func newSidecar() *sidecar {
	if env("MESH", "") != "istio" {
		return nil
	}
	return &sidecar{
		workload:  env("SERVICE_NAME", "demo"),
		namespace: env("MESH_NAMESPACE", "default"),
		counts:    map[string]float64{},
	}
}

func (s *sidecar) record(labels map[string]string) {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%q", k, labels[k])
	}
	s.mu.Lock()
	s.counts["{"+strings.Join(parts, ",")+"}"]++
	s.mu.Unlock()
}

// inbound plays the destination-side proxy (reporter="destination").
func (s *sidecar) inbound(next http.Handler) http.Handler {
	if s == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			s.serveMetrics(w)
			return
		}
		w.Header().Set(peerHeader, s.workload)
		w.Header().Set(peerNSHeader, s.namespace)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		src, srcNS := r.Header.Get(peerHeader), r.Header.Get(peerNSHeader)
		if src == "" {
			src, srcNS = "unknown", "unknown" // caller has no sidecar
		}
		s.record(map[string]string{
			"reporter": "destination", "request_protocol": "http", "response_code": strconv.Itoa(rec.status),
			"source_workload": src, "source_workload_namespace": srcNS,
			"destination_workload": s.workload, "destination_workload_namespace": s.namespace,
			"destination_service_name": s.workload,
		})
	})
}

// outbound plays the source-side proxy (reporter="source") around a call.
func (s *sidecar) outbound(req *http.Request, service string, call func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if s == nil {
		return call(req)
	}
	req.Header.Set(peerHeader, s.workload)
	req.Header.Set(peerNSHeader, s.namespace)
	resp, err := call(req)
	code := "0"
	dst, dstNS := "unknown", "unknown" // callee has no sidecar
	if resp != nil {
		code = strconv.Itoa(resp.StatusCode)
		if v := resp.Header.Get(peerHeader); v != "" {
			dst, dstNS = v, resp.Header.Get(peerNSHeader)
		}
	}
	s.record(map[string]string{
		"reporter": "source", "request_protocol": "http", "response_code": code,
		"source_workload": s.workload, "source_workload_namespace": s.namespace,
		"destination_workload": dst, "destination_workload_namespace": dstNS,
		"destination_service_name": service,
	})
	return resp, err
}

func (s *sidecar) serveMetrics(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintln(w, "# TYPE istio_requests_total counter")
	keys := make([]string, 0, len(s.counts))
	for k := range s.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "istio_requests_total%s %g\n", k, s.counts[k])
	}
}
