package kubernetes

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

func pod(ns, name, ip string, owner *metav1.OwnerReference, labels, annotations map[string]string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels, Annotations: annotations},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIPs: []corev1.PodIP{{IP: ip}}},
	}
	if owner != nil {
		p.OwnerReferences = []metav1.OwnerReference{*owner}
	}
	return p
}

func startedAt(p *corev1.Pod, t time.Time) *corev1.Pod {
	p.Status.StartTime = &metav1.Time{Time: t}
	return p
}

func ctrl(kind, name string) *metav1.OwnerReference {
	yes := true
	return &metav1.OwnerReference{Kind: kind, Name: name, Controller: &yes}
}

// Exercise the real client serialization and HTTP error handling without
// importing client-go's fake object tracker and its legacy JSON patch module.
func testClient(t *testing.T, handler http.HandlerFunc) rest.Interface {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := inventoryRESTClient(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeAPIResponse(t *testing.T, w http.ResponseWriter, status int, obj any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(obj); err != nil {
		t.Errorf("encoding API response: %v", err)
	}
}

func inventoryClient(t *testing.T, objects ...runtime.Object) rest.Interface {
	t.Helper()
	return testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("inventory must only read, got %s %s", r.Method, r.URL.Path)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/api/v1/pods":
			list := &corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}}
			for _, obj := range objects {
				if p, ok := obj.(*corev1.Pod); ok {
					list.Items = append(list.Items, *p)
				}
			}
			writeAPIResponse(t, w, http.StatusOK, list)
		case "/api/v1/services":
			list := &corev1.ServiceList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceList"}}
			for _, obj := range objects {
				if svc, ok := obj.(*corev1.Service); ok {
					list.Items = append(list.Items, *svc)
				}
			}
			writeAPIResponse(t, w, http.StatusOK, list)
		default:
			t.Errorf("unexpected inventory request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
}

func discover(t *testing.T, client rest.Interface) source.Observation {
	t.Helper()
	obs, err := NewWithClient("kubernetes", Config{}, client).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return obs
}

// byID folds the per-pod observations back into one per node, as the graph
// does.
func byID(obs []source.NodeObservation) map[string]source.NodeObservation {
	out := map[string]source.NodeObservation{}
	for _, n := range obs {
		m, ok := out[n.ID]
		if !ok {
			m = n
			m.Addresses = nil
		}
		m.Addresses = append(m.Addresses, n.Addresses...)
		m.Ignore = m.Ignore || n.Ignore
		out[n.ID] = m
	}
	return out
}

func TestInventory(t *testing.T) {
	ordersLabels := map[string]string{
		"app": "orders", "pod-template-hash": "7d9f",
		"traefik.http.middlewares.auth.basicauth.users": "admin:$apr1$x",
	}
	client := inventoryClient(t,
		pod("shop", "orders-7d9f-abcde", "10.1.0.5", ctrl("ReplicaSet", "orders-7d9f"), ordersLabels, nil),
		pod("shop", "orders-7d9f-fghij", "10.1.0.6", ctrl("ReplicaSet", "orders-7d9f"), ordersLabels, nil),
		pod("shop", "orders-db-0", "10.1.0.7", ctrl("StatefulSet", "orders-db"), map[string]string{"app": "orders-db"}, map[string]string{"teia.nilum.io/kind": "database"}),
		pod("shop", "report-29384756-xk2p1", "10.1.0.8", ctrl("Job", "report-29384756"), nil, nil),
		pod("shop", "debug", "10.1.0.9", nil, nil, map[string]string{"teia.nilum.io/ignore": "true"}),
		pod("kube-system", "coredns-abc", "10.1.0.2", ctrl("ReplicaSet", "coredns-abc"), nil, nil),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "orders"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "orders"}, ClusterIPs: []string{"10.96.0.10"}},
		},
	)
	nodes := byID(discover(t, client).Nodes)
	for _, want := range []string{"orders", "orders-db", "report", "debug"} {
		if _, ok := nodes[want]; !ok {
			t.Errorf("missing node %s in %+v", want, nodes)
		}
	}
	if _, ok := nodes["coredns"]; ok {
		t.Error("kube-system should be excluded by default")
	}
	orders := nodes["orders"]
	if len(orders.Addresses) != 3 || orders.Labels["k8s.workload"] != "Deployment" || orders.Labels["k8s.service"] != "orders" {
		t.Errorf("orders = %+v (want both pod IPs plus the service ClusterIP)", orders)
	}
	if orders.Kind != model.KindUnknown {
		t.Errorf("a pod without the kind annotation must not claim a kind, got %s", orders.Kind)
	}
	for _, k := range []string{"pod-template-hash", "traefik.http.middlewares.auth.basicauth.users"} {
		if _, ok := orders.Labels[k]; ok {
			t.Errorf("label %s should have been dropped: %v", k, orders.Labels)
		}
	}
	if orders.Labels["app"] != "orders" {
		t.Errorf("user label lost: %v", orders.Labels)
	}
	if nodes["orders-db"].Kind != model.KindDatabase {
		t.Error("kind annotation ignored")
	}
	if nodes["report"].Labels["k8s.workload"] != "CronJob" {
		t.Errorf("cronjob not recognised: %+v", nodes["report"])
	}
	if !nodes["debug"].Ignore {
		t.Error("ignore annotation not honoured")
	}
}

// During a rollout the old pods still run: their addresses still count, but
// the workload's labels are the new pods' (a label added in this rollout,
// such as the journey, must not flicker away until the old pods are gone).
func TestRolloutTakesLabelsFromTheNewPods(t *testing.T) {
	gone := metav1.Now()
	old := pod("shop", "web-1111-aaaaa", "10.1.0.5", ctrl("ReplicaSet", "web-1111"), map[string]string{"app": "web", "pod-template-hash": "1111"}, nil)
	old.CreationTimestamp = metav1.Time{Time: time.Now().Add(-time.Hour)}
	old.DeletionTimestamp = &gone
	older := pod("shop", "web-0000-zzzzz", "10.1.0.4", ctrl("ReplicaSet", "web-0000"), map[string]string{"app": "web", "pod-template-hash": "0000"}, nil)
	older.CreationTimestamp = metav1.Time{Time: time.Now().Add(-2 * time.Hour)}
	fresh := pod("shop", "web-2222-bbbbb", "10.1.0.6", ctrl("ReplicaSet", "web-2222"), map[string]string{"app": "web", "app.kubernetes.io/part-of": "shop", "pod-template-hash": "2222"}, nil)
	fresh.CreationTimestamp = metav1.Time{Time: time.Now().Add(-time.Minute)}
	web := byID(discover(t, inventoryClient(t, old, older, fresh)).Nodes)["web"]
	if web.Labels["app.kubernetes.io/part-of"] != "shop" {
		t.Errorf("labels should come from the newest pod, got %v", web.Labels)
	}
	if len(web.Addresses) != 3 {
		t.Errorf("every pod still holds its address, got %+v", web.Addresses)
	}
}

// A rollout or scale-up must not make the addresses of the workload's older
// pods look recently reassigned.
func TestAddressesCarryTheirOwnSince(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old, fresh, created := now.Add(-time.Hour), now.Add(-time.Minute), now.Add(-30*24*time.Hour)
	labels := map[string]string{"app": "orders", "pod-template-hash": "7d9f"}
	client := inventoryClient(t,
		startedAt(pod("shop", "orders-7d9f-abcde", "10.1.0.5", ctrl("ReplicaSet", "orders-7d9f"), labels, nil), old),
		startedAt(pod("shop", "orders-7d9f-fghij", "10.1.0.6", ctrl("ReplicaSet", "orders-7d9f"), labels, nil), fresh),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "orders", CreationTimestamp: metav1.Time{Time: created}},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "orders"}, ClusterIPs: []string{"10.96.0.10"}},
		},
	)
	since := map[string]time.Time{}
	for _, n := range discover(t, client).Nodes {
		if n.ID != "orders" {
			t.Errorf("unexpected node %s", n.ID)
		}
		for _, a := range n.Addresses {
			since[a] = n.Since
		}
	}
	want := map[string]time.Time{"10.1.0.5": old, "10.1.0.6": fresh, "10.96.0.10": created}
	for addr, w := range want {
		if got, ok := since[addr]; !ok || !got.Equal(w) {
			t.Errorf("%s held since %v, want %v", addr, got, w)
		}
	}
}

func TestListPaginatesFromCache(t *testing.T) {
	var pods []corev1.Pod
	for i := range 5 {
		pods = append(pods, *pod("shop", fmt.Sprintf("p%d", i), fmt.Sprintf("10.1.0.%d", i), nil, nil, nil))
	}
	var calls []metav1.ListOptions
	var callsMu sync.Mutex
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/services" {
			writeAPIResponse(t, w, http.StatusOK, &corev1.ServiceList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceList"}})
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/pods" {
			t.Errorf("unexpected inventory request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		limit, err := strconv.ParseInt(query.Get("limit"), 10, 64)
		if err != nil {
			t.Errorf("invalid list limit: %v", err)
		}
		opts := metav1.ListOptions{ResourceVersion: query.Get("resourceVersion"), Continue: query.Get("continue"), Limit: limit}
		callsMu.Lock()
		calls = append(calls, opts)
		callsMu.Unlock()
		start := 0
		if opts.Continue != "" {
			start, _ = strconv.Atoi(opts.Continue)
		}
		end := min(start+2, len(pods)) // a server may return fewer items than the limit
		list := &corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}, Items: pods[start:end]}
		if end < len(pods) {
			list.Continue = strconv.Itoa(end)
		}
		writeAPIResponse(t, w, http.StatusOK, list)
	})

	nodes := byID(discover(t, client).Nodes)
	if len(nodes) != len(pods) {
		t.Errorf("got %d nodes, want %d: %v", len(nodes), len(pods), nodes)
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if len(calls) != 3 {
		t.Fatalf("got %d list calls, want 3: %+v", len(calls), calls)
	}
	if first := calls[0]; first.ResourceVersion != "0" || first.Limit != pageSize || first.Continue != "" {
		t.Errorf("first page should come from the cache with a limit: %+v", first)
	}
	for _, c := range calls[1:] {
		if c.Continue == "" || c.ResourceVersion != "" || c.Limit != pageSize {
			t.Errorf("later pages follow the continue token without a resource version: %+v", c)
		}
	}
}

func TestForbiddenIsAnRBACFinding(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeAPIResponse(t, w, http.StatusForbidden, &metav1.Status{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
			Status:   metav1.StatusFailure, Reason: metav1.StatusReasonForbidden,
			Message: "pods is forbidden: service account cannot list pods", Code: http.StatusForbidden,
		})
	})
	obs, err := NewWithClient("kubernetes", Config{}, client).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Findings) != 1 || obs.Findings[0].Rule != "kubernetes-rbac" || !obs.Degraded {
		t.Fatalf("unexpected: %+v", obs)
	}
	if !strings.Contains(obs.Findings[0].Detail, "service account cannot list pods") {
		t.Errorf("API status detail lost: %q", obs.Findings[0].Detail)
	}
}

func TestRESTInventoryPreservesNamespacesTLSAndTokenFile(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("mounted-service-account-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 4)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer mounted-service-account-token" {
			t.Errorf("request did not preserve read-only token authentication: method=%s", r.Method)
		}
		if q := r.URL.Query(); q.Get("resourceVersion") != "0" || q.Get("limit") != strconv.Itoa(pageSize) {
			t.Errorf("missing cache/pagination query: %v", q)
		}
		if strings.HasSuffix(r.URL.Path, "/pods") {
			writeAPIResponse(t, w, http.StatusOK, &corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}})
		} else {
			writeAPIResponse(t, w, http.StatusOK, &corev1.ServiceList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceList"}})
		}
	}))
	defer server.Close()
	config := &rest.Config{
		Host: server.URL, BearerToken: "fallback-must-not-override-file", BearerTokenFile: tokenPath,
		TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})},
		Timeout:         time.Second,
	}
	client, err := inventoryRESTClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewWithClient("kubernetes", Config{Namespaces: []string{"shop", "workers"}}, client).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/api/v1/namespaces/shop/pods", "/api/v1/namespaces/shop/services", "/api/v1/namespaces/workers/pods", "/api/v1/namespaces/workers/services"} {
		select {
		case got := <-requests:
			if got != want {
				t.Errorf("request path %q, want %q", got, want)
			}
		default:
			t.Fatalf("missing request %s", want)
		}
	}
	// Keep the token file in client-go's transport so its normal refresh behavior
	// handles projected service-account rotation. Do not replace it with a token
	// loaded once by Teia, or mutate the caller's reusable configuration.
	if config.BearerTokenFile != tokenPath || config.Timeout != time.Second || config.Insecure || config.GroupVersion != nil {
		t.Errorf("REST client altered caller configuration: tokenFile=%q timeout=%s", config.BearerTokenFile, config.Timeout)
	}
}

func TestRESTInventoryPreservesTimeoutAndCancellation(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		name := "timeout"
		if cancelRequest {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			config := &rest.Config{Host: server.URL, Timeout: 30 * time.Millisecond}
			ctx := context.Background()
			want := context.DeadlineExceeded
			if cancelRequest {
				config.Timeout = time.Second
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				go func() {
					select {
					case <-started:
						cancel()
					case <-ctx.Done():
					}
				}()
				want = context.Canceled
			}
			client, err := inventoryRESTClient(config)
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewWithClient("kubernetes", Config{}, client).Discover(ctx)
			if !errors.Is(err, want) {
				t.Errorf("Discover returned %v, want %v", err, want)
			}
		})
	}
}
