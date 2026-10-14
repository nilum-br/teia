// Package kubernetes is an inventory source: it lists running pods and
// services so every workload is in the graph (even without traffic) and pod
// or service IPs seen by other sources resolve to workload names.
//
// A pod becomes the node of its owning workload: ReplicaSet -> Deployment,
// StatefulSet, DaemonSet, Job -> CronJob. Annotations on the pod tune it:
//
//	teia.nilum.io/node-id: override the node ID
//	teia.nilum.io/kind:    service | database | cache | queue | external
//	teia.nilum.io/tech:    technology for the icon (postgresql, redis, kafka...)
//	teia.nilum.io/ignore:  "true" to keep it out of the graph
package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nilum-br/teia/internal/model"
	"github.com/nilum-br/teia/internal/source"
)

const annotationPrefix = "teia.nilum.io/"

// DefaultExcludedNamespaces hold cluster plumbing, not applications.
var DefaultExcludedNamespaces = []string{"kube-system", "kube-public", "kube-node-lease"}

type Config struct {
	// Kubeconfig and Context select a cluster; both empty means in-cluster
	// config when running in a pod, else the usual KUBECONFIG loading rules.
	Kubeconfig string
	Context    string
	// Namespaces limits discovery; empty means all namespaces.
	Namespaces []string
	// ExcludeNamespaces defaults to DefaultExcludedNamespaces.
	ExcludeNamespaces []string
}

type Source struct {
	name   string
	cfg    Config
	client k8s.Interface
	err    error
}

func New(name string, cfg Config) *Source {
	if cfg.ExcludeNamespaces == nil {
		cfg.ExcludeNamespaces = DefaultExcludedNamespaces
	}
	s := &Source{name: name, cfg: cfg}
	rc, err := restConfig(cfg)
	if err != nil {
		s.err = fmt.Errorf("kubernetes config: %w", err)
		return s
	}
	s.client, s.err = k8s.NewForConfig(rc)
	return s
}

// NewWithClient is used by tests.
func NewWithClient(name string, cfg Config, client k8s.Interface) *Source {
	if cfg.ExcludeNamespaces == nil {
		cfg.ExcludeNamespaces = DefaultExcludedNamespaces
	}
	return &Source{name: name, cfg: cfg, client: client}
}

func restConfig(cfg Config) (*rest.Config, error) {
	if cfg.Kubeconfig == "" && cfg.Context == "" {
		if rc, err := rest.InClusterConfig(); err == nil {
			return rc, nil
		}
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if cfg.Kubeconfig != "" {
		rules.ExplicitPath = cfg.Kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: cfg.Context}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

func (s *Source) Name() string { return s.name }
func (s *Source) Type() string { return "kubernetes" }

func (s *Source) Discover(ctx context.Context) (source.Observation, error) {
	var obs source.Observation
	if s.err != nil {
		return obs, s.err
	}
	pods, services, err := s.list(ctx)
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
			obs.Degraded = true
			obs.Message = "missing RBAC to list pods and services"
			obs.Findings = append(obs.Findings, s.rbacFinding(err))
			return obs, nil
		}
		return obs, err
	}

	excluded := map[string]bool{}
	for _, ns := range s.cfg.ExcludeNamespaces {
		excluded[ns] = true
	}

	nodes := map[string]*source.NodeObservation{}
	podWorkload := map[string]string{} // namespace/pod -> node ID
	for i := range pods {
		p := &pods[i]
		if excluded[p.Namespace] || p.Status.Phase != corev1.PodRunning {
			continue
		}
		id, workloadKind := workloadOf(p)
		n, ok := nodes[id]
		if !ok {
			n = &source.NodeObservation{
				ID:     id,
				Kind:   model.NodeKind(p.Annotations[annotationPrefix+"kind"]),
				Labels: userLabels(p.Labels),
			}
			n.Labels["k8s.namespace"] = p.Namespace
			n.Labels["k8s.workload"] = workloadKind
			if len(p.Spec.Containers) > 0 {
				n.Labels["image"] = p.Spec.Containers[0].Image
			}
			if t := p.Annotations[annotationPrefix+"tech"]; t != "" {
				n.Labels["teia.tech"] = t
			}
			if n.Kind == "" {
				n.Kind = model.KindService
			}
			nodes[id] = n
		}
		if p.Annotations[annotationPrefix+"ignore"] == "true" {
			n.Ignore = true
		}
		if p.Status.StartTime != nil && p.Status.StartTime.Time.After(n.Since) {
			n.Since = p.Status.StartTime.Time
		}
		if !p.Spec.HostNetwork { // host-network pods share the node IP
			for _, ip := range p.Status.PodIPs {
				n.Addresses = append(n.Addresses, ip.IP)
			}
		}
		podWorkload[p.Namespace+"/"+p.Name] = id
	}

	// A Service whose selector picks exactly one workload lends it its
	// ClusterIP, so connections to the Service IP resolve too.
	for i := range services {
		svc := &services[i]
		if excluded[svc.Namespace] || len(svc.Spec.Selector) == 0 {
			continue
		}
		sel := labels.SelectorFromSet(svc.Spec.Selector)
		owners := map[string]bool{}
		for j := range pods {
			p := &pods[j]
			if p.Namespace == svc.Namespace && sel.Matches(labels.Set(p.Labels)) {
				if id, ok := podWorkload[p.Namespace+"/"+p.Name]; ok {
					owners[id] = true
				}
			}
		}
		if len(owners) != 1 {
			continue
		}
		for id := range owners {
			for _, ip := range svc.Spec.ClusterIPs {
				if ip != "" && ip != corev1.ClusterIPNone {
					nodes[id].Addresses = append(nodes[id].Addresses, ip)
				}
			}
			nodes[id].Labels["k8s.service"] = svc.Name
		}
	}

	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		obs.Nodes = append(obs.Nodes, *nodes[id])
	}
	obs.Message = fmt.Sprintf("%d workloads from %d pods", len(nodes), len(podWorkload))
	return obs, nil
}

func (s *Source) list(ctx context.Context) ([]corev1.Pod, []corev1.Service, error) {
	namespaces := s.cfg.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}
	var pods []corev1.Pod
	var services []corev1.Service
	for _, ns := range namespaces {
		pl, err := s.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, nil, err
		}
		pods = append(pods, pl.Items...)
		sl, err := s.client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, nil, err
		}
		services = append(services, sl.Items...)
	}
	return pods, services, nil
}

var cronJobSuffix = regexp.MustCompile(`^(.+)-\d{8,}$`)

// machineLabels are set by controllers and differ per pod; they would only
// add noise to grouping.
var machineLabels = map[string]bool{
	"pod-template-hash": true, "controller-revision-hash": true, "statefulset.kubernetes.io/pod-name": true,
	"job-name": true, "controller-uid": true, "batch.kubernetes.io/job-name": true,
	"batch.kubernetes.io/controller-uid": true, "apps.kubernetes.io/pod-index": true,
}

func userLabels(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if !machineLabels[k] {
			out[k] = v
		}
	}
	return out
}

// workloadOf names the workload a pod belongs to, without reading the
// owners themselves: ReplicaSet names are "<deployment>-<pod-template-hash>"
// and CronJob jobs are "<cronjob>-<scheduled timestamp>".
func workloadOf(p *corev1.Pod) (id, kind string) {
	if v := p.Annotations[annotationPrefix+"node-id"]; v != "" {
		return v, "Annotated"
	}
	for _, ref := range p.OwnerReferences {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}
		switch ref.Kind {
		case "ReplicaSet":
			if hash := p.Labels["pod-template-hash"]; hash != "" && strings.HasSuffix(ref.Name, "-"+hash) {
				return strings.TrimSuffix(ref.Name, "-"+hash), "Deployment"
			}
			return ref.Name, "ReplicaSet"
		case "Job":
			if m := cronJobSuffix.FindStringSubmatch(ref.Name); m != nil {
				return m[1], "CronJob"
			}
			return ref.Name, "Job"
		default:
			return ref.Name, ref.Kind
		}
	}
	return p.Name, "Pod"
}

func (s *Source) rbacFinding(err error) model.Finding {
	var status apierrors.APIStatus
	detail := err.Error()
	if errors.As(err, &status) {
		detail = status.Status().Message
	}
	return model.Finding{
		Rule:     "kubernetes-rbac",
		Kind:     model.FindingPermission,
		Severity: model.SeverityWarning,
		Subject:  s.name,
		Title:    "teia cannot list pods and services",
		Detail:   detail + ". Without the inventory, workloads without traffic stay invisible and pod IPs seen by other sources cannot be named.",
		Fix: &model.Fix{
			Summary:  "Bind teia's service account to a read-only role. It reads metadata only: no secrets, no exec, no logs.",
			Language: "yaml",
			Snippet: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: teia-inventory
rules:
  - apiGroups: [""]
    resources: [pods, services, namespaces]
    verbs: [get, list, watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: teia-inventory
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: teia-inventory
subjects:
  - kind: ServiceAccount
    name: teia
    namespace: teia`,
		},
	}
}
