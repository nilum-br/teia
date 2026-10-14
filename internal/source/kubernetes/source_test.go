package kubernetes

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

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

func discover(t *testing.T, client *fake.Clientset) source.Observation {
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
	client := fake.NewSimpleClientset(
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

// A rollout or scale-up must not make the addresses of the workload's older
// pods look recently reassigned.
func TestAddressesCarryTheirOwnSince(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	old, fresh, created := now.Add(-time.Hour), now.Add(-time.Minute), now.Add(-30*24*time.Hour)
	labels := map[string]string{"app": "orders", "pod-template-hash": "7d9f"}
	client := fake.NewSimpleClientset(
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
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		opts := a.(interface{ GetListOptions() metav1.ListOptions }).GetListOptions()
		calls = append(calls, opts)
		start := 0
		if opts.Continue != "" {
			start, _ = strconv.Atoi(opts.Continue)
		}
		end := min(start+2, len(pods)) // a server may return fewer items than the limit
		list := &corev1.PodList{Items: pods[start:end]}
		if end < len(pods) {
			list.Continue = strconv.Itoa(end)
		}
		return true, list, nil
	})

	nodes := byID(discover(t, client).Nodes)
	if len(nodes) != len(pods) {
		t.Errorf("got %d nodes, want %d: %v", len(nodes), len(pods), nodes)
	}
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
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", nil)
	})
	obs, err := NewWithClient("kubernetes", Config{}, client).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Findings) != 1 || obs.Findings[0].Rule != "kubernetes-rbac" || !obs.Degraded {
		t.Fatalf("unexpected: %+v", obs)
	}
}
