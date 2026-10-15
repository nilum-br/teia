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
//	teia.nilum.io/journeys: JSON array of ownership journey names
//	teia.nilum.io/ignore:  "true" to keep it out of the graph
//
// Without the kind annotation a workload reports kind "unknown": a pod
// running is no evidence of what it is, so flows files, explorers and the
// image decide.
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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/pager"

	"github.com/nilum-br/teia/internal/journey"
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
	client rest.Interface
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
	s.client, s.err = inventoryRESTClient(rc)
	return s
}

// NewWithClient is used by tests.
func NewWithClient(name string, cfg Config, client rest.Interface) *Source {
	if cfg.ExcludeNamespaces == nil {
		cfg.ExcludeNamespaces = DefaultExcludedNamespaces
	}
	return &Source{name: name, cfg: cfg, client: client}
}

// Inventory only needs the core API's list operations. Use client-go's REST
// transport directly instead of its generated clientset, which also imports
// mutation/apply machinery and a legacy JSON patch implementation. Authentication,
// kubeconfig exec credentials, TLS and request handling remain owned by client-go.
func inventoryRESTClient(config *rest.Config) (rest.Interface, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("registering Kubernetes core API: %w", err)
	}
	cfg := rest.CopyConfig(config)
	groupVersion := corev1.SchemeGroupVersion
	cfg.GroupVersion = &groupVersion
	cfg.APIPath = "/api"
	cfg.NegotiatedSerializer = serializer.NewCodecFactory(scheme).WithoutConversion()
	if cfg.UserAgent == "" {
		cfg.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	return rest.RESTClientFor(cfg)
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

	// A workload's labels and annotations come from its newest pod that is
	// not on its way out: during a rollout the old pods still run and hold
	// their addresses, but no longer say what the workload is now.
	sort.SliceStable(pods, func(i, j int) bool {
		a, b := &pods[i], &pods[j]
		if (a.DeletionTimestamp == nil) != (b.DeletionTimestamp == nil) {
			return a.DeletionTimestamp == nil
		}
		return b.CreationTimestamp.Before(&a.CreationTimestamp)
	})

	excluded := map[string]bool{}
	for _, ns := range s.cfg.ExcludeNamespaces {
		excluded[ns] = true
	}

	// A workload's pods start at different times (rollouts, scale-ups), and
	// an address is only trusted over a time window if its holder had it for
	// the whole window. So every pod, and every Service IP, is its own
	// observation of the workload, carrying its own addresses and since when
	// it holds them; the graph merges them back into one node.
	workloads := map[string]*source.NodeObservation{} // node ID -> kind, labels, ignore
	held := map[string][]source.NodeObservation{}     // node ID -> addresses and since when
	podWorkload := map[string]string{}                // namespace/pod -> node ID
	podsByNamespace := map[string][]*corev1.Pod{}
	for i := range pods {
		p := &pods[i]
		if excluded[p.Namespace] || p.Status.Phase != corev1.PodRunning {
			continue
		}
		id, workloadKind := workloadOf(p)
		w, ok := workloads[id]
		if !ok {
			w = &source.NodeObservation{
				ID:     id,
				Kind:   model.KindUnknown,
				Labels: userLabels(p.Labels),
			}
			if k := strings.TrimSpace(p.Annotations[annotationPrefix+"kind"]); k != "" {
				w.Kind = model.NodeKind(strings.ToLower(k))
			}
			w.Labels["k8s.namespace"] = p.Namespace
			w.Labels["k8s.workload"] = workloadKind
			if len(p.Spec.Containers) > 0 {
				w.Labels["image"] = p.Spec.Containers[0].Image
			}
			if t := p.Annotations[annotationPrefix+"tech"]; t != "" {
				w.Labels["teia.tech"] = t
			}
			if raw, present := p.Annotations[journey.Annotation]; present {
				memberships, err := journey.ParseAnnotation(raw)
				if err != nil {
					obs.Findings = append(obs.Findings, model.Finding{
						ID: "invalid-journeys:" + id, Rule: "invalid-journeys", Kind: model.FindingSource,
						Severity: model.SeverityWarning, Subject: id, Title: "Journey annotation is invalid",
						Detail: journey.Annotation + " " + err.Error() + "; existing journey labels still apply.",
					})
				} else {
					w.Journeys = memberships
				}
			}
			workloads[id] = w
		}
		if p.Annotations[annotationPrefix+"ignore"] == "true" {
			w.Ignore = true
		}
		h := source.NodeObservation{ID: id}
		if p.Status.StartTime != nil {
			h.Since = p.Status.StartTime.Time
		}
		if !p.Spec.HostNetwork { // host-network pods share the node IP
			for _, ip := range p.Status.PodIPs {
				h.Addresses = append(h.Addresses, ip.IP)
			}
		}
		held[id] = append(held[id], h)
		podWorkload[p.Namespace+"/"+p.Name] = id
		podsByNamespace[p.Namespace] = append(podsByNamespace[p.Namespace], p)
	}

	// A Service whose selector picks exactly one workload lends it its
	// ClusterIP, so connections to the Service IP resolve too. The ClusterIP
	// belongs to the Service since it was created, whatever its pods do.
	for i := range services {
		svc := &services[i]
		if excluded[svc.Namespace] || len(svc.Spec.Selector) == 0 {
			continue
		}
		sel := labels.SelectorFromSet(svc.Spec.Selector)
		owners := map[string]bool{}
		for _, p := range podsByNamespace[svc.Namespace] {
			if sel.Matches(labels.Set(p.Labels)) {
				owners[podWorkload[p.Namespace+"/"+p.Name]] = true
			}
		}
		if len(owners) != 1 {
			continue
		}
		for id := range owners {
			h := source.NodeObservation{ID: id, Since: svc.CreationTimestamp.Time}
			for _, ip := range svc.Spec.ClusterIPs {
				if ip != "" && ip != corev1.ClusterIPNone {
					h.Addresses = append(h.Addresses, ip)
				}
			}
			if len(h.Addresses) > 0 {
				held[id] = append(held[id], h)
			}
			workloads[id].Labels["k8s.service"] = svc.Name
		}
	}

	ids := make([]string, 0, len(workloads))
	for id := range workloads {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		w := workloads[id]
		for _, h := range held[id] {
			n := *w // every observation describes the whole workload
			n.Addresses, n.Since = h.Addresses, h.Since
			obs.Nodes = append(obs.Nodes, n)
		}
	}
	obs.Message = fmt.Sprintf("%d workloads from %d pods", len(workloads), len(podWorkload))
	return obs, nil
}

// pageSize bounds each list response, so a large cluster is read in chunks
// rather than one huge response.
const pageSize = 500

// list reads pods and services every round. ResourceVersion "0" lets the API
// server answer from its watch cache instead of a quorum read from etcd, and
// the pager follows Continue tokens (falling back to a full list if a token
// expires). The next step for very large clusters is shared informers, which
// watch for changes instead of listing every round.
func (s *Source) list(ctx context.Context) ([]corev1.Pod, []corev1.Service, error) {
	namespaces := s.cfg.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceAll}
	}
	var pods []corev1.Pod
	var services []corev1.Service
	for _, ns := range namespaces {
		err := listAll(ctx, func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
			result := &corev1.PodList{}
			err := s.client.Get().Namespace(ns).Resource("pods").VersionedParams(&opts, metav1.ParameterCodec).Do(ctx).Into(result)
			return result, err
		}, func(obj runtime.Object) error {
			p, ok := obj.(*corev1.Pod)
			if !ok {
				return fmt.Errorf("unexpected %T in a pod list", obj)
			}
			pods = append(pods, *p)
			return nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("listing pods: %w", err)
		}
		err = listAll(ctx, func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
			result := &corev1.ServiceList{}
			err := s.client.Get().Namespace(ns).Resource("services").VersionedParams(&opts, metav1.ParameterCodec).Do(ctx).Into(result)
			return result, err
		}, func(obj runtime.Object) error {
			svc, ok := obj.(*corev1.Service)
			if !ok {
				return fmt.Errorf("unexpected %T in a service list", obj)
			}
			services = append(services, *svc)
			return nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("listing services: %w", err)
		}
	}
	return pods, services, nil
}

// listAll pages through one list from the API server's cache and calls each
// for every item.
func listAll(ctx context.Context, page pager.ListPageFunc, each func(runtime.Object) error) error {
	p := pager.New(page)
	p.PageSize = pageSize
	list, _, err := p.List(ctx, metav1.ListOptions{ResourceVersion: "0"})
	if err != nil {
		return err
	}
	return meta.EachListItem(list, each)
}

var cronJobSuffix = regexp.MustCompile(`^(.+)-\d{8,}$`)

// machineLabels are set by controllers and differ per pod; they would only
// add noise to grouping.
var machineLabels = map[string]bool{
	"pod-template-hash": true, "controller-revision-hash": true, "statefulset.kubernetes.io/pod-name": true,
	"job-name": true, "controller-uid": true, "batch.kubernetes.io/job-name": true,
	"batch.kubernetes.io/controller-uid": true, "apps.kubernetes.io/pod-index": true,
}

// userLabels keeps the labels people set, minus those that may carry
// credentials (see source.SafeLabels).
func userLabels(in map[string]string) map[string]string {
	out := source.SafeLabels(in)
	for k := range out {
		if machineLabels[k] {
			delete(out, k)
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
