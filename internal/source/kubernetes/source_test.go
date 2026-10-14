package kubernetes

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/nilum-br/teia/internal/model"
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

func ctrl(kind, name string) *metav1.OwnerReference {
	yes := true
	return &metav1.OwnerReference{Kind: kind, Name: name, Controller: &yes}
}

func TestInventory(t *testing.T) {
	client := fake.NewSimpleClientset(
		pod("shop", "orders-7d9f-abcde", "10.1.0.5", ctrl("ReplicaSet", "orders-7d9f"), map[string]string{"app": "orders", "pod-template-hash": "7d9f"}, nil),
		pod("shop", "orders-7d9f-fghij", "10.1.0.6", ctrl("ReplicaSet", "orders-7d9f"), map[string]string{"app": "orders", "pod-template-hash": "7d9f"}, nil),
		pod("shop", "orders-db-0", "10.1.0.7", ctrl("StatefulSet", "orders-db"), map[string]string{"app": "orders-db"}, map[string]string{"teia.nilum.io/kind": "database"}),
		pod("shop", "report-29384756-xk2p1", "10.1.0.8", ctrl("Job", "report-29384756"), nil, nil),
		pod("shop", "debug", "10.1.0.9", nil, nil, map[string]string{"teia.nilum.io/ignore": "true"}),
		pod("kube-system", "coredns-abc", "10.1.0.2", ctrl("ReplicaSet", "coredns-abc"), nil, nil),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "orders"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "orders"}, ClusterIPs: []string{"10.96.0.10"}},
		},
	)
	obs, err := NewWithClient("kubernetes", Config{}, client).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]int{}
	for i, n := range obs.Nodes {
		byID[n.ID] = i
	}
	for _, want := range []string{"orders", "orders-db", "report", "debug"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("missing node %s in %+v", want, obs.Nodes)
		}
	}
	if _, ok := byID["coredns"]; ok {
		t.Error("kube-system should be excluded by default")
	}
	orders := obs.Nodes[byID["orders"]]
	if len(orders.Addresses) != 3 || orders.Labels["k8s.workload"] != "Deployment" || orders.Labels["k8s.service"] != "orders" {
		t.Errorf("orders = %+v (want both pod IPs plus the service ClusterIP)", orders)
	}
	if obs.Nodes[byID["orders-db"]].Kind != model.KindDatabase {
		t.Error("kind annotation ignored")
	}
	if obs.Nodes[byID["report"]].Labels["k8s.workload"] != "CronJob" {
		t.Errorf("cronjob not recognised: %+v", obs.Nodes[byID["report"]])
	}
	if !obs.Nodes[byID["debug"]].Ignore {
		t.Error("ignore annotation not honoured")
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
