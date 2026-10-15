# Running teia on Kubernetes

teia runs in your cluster as **one pod**. It reads the telemetry you already run, the cluster's inventory and the
flows files teams declare, and serves the map, the API the Grafana panel reads, and its own metrics. This page is for
whoever installs and operates it: what runs, what it touches, and how to install it with the Helm chart
([`charts/teia`](../charts/teia)).

## What runs

One Deployment with one replica: the `teia` binary in a distroless image, as a non-root user, on a read-only
filesystem, with no Linux capabilities. teia does **not** install:

- a CRD, an operator or an admission webhook;
- a DaemonSet, an agent, or a sidecar in your workloads;
- a database or a volume: the map lives in memory and is rebuilt from your telemetry within one round (30 s by
  default) after a restart.

| The chart creates | What for |
|---|---|
| Deployment `teia` | the pod |
| Service `teia` (port 8080) | the UI, the API and `/metrics` |
| ConfigMap `teia` | `teia.yaml`, built from the chart's `config` value |
| ConfigMap `teia-flows` | the flows files (unless you bring your own) |
| ServiceAccount, ClusterRole, ClusterRoleBinding | read-only inventory (below) |
| Secret `teia-auth` (optional) | the API token, when the chart is given one |
| Ingress, ServiceMonitor (optional) | when enabled |

`helm uninstall` removes all of it. Nothing is left behind: no CRDs, no volumes.

## What it reads, and what it needs

teia only reads. It never writes to your cluster, your telemetry or your databases.

| Source | What teia does | What it needs |
|---|---|---|
| Kubernetes API | lists pods, Services and namespaces: names, labels, owners, IPs | `get`, `list`, `watch` on those three, cluster-wide (the chart's ClusterRole). No Secrets, no exec, no logs. |
| Prometheus, Thanos, Mimir, VictoriaMetrics | PromQL over the HTTP API | network access to it; a header if it asks for one |
| Tempo | trace search over the HTTP API | the same |
| Loki | LogQL queries you configure (`sources.queries`) | the same |
| PostgreSQL (optional) | reads `pg_stat_activity` to see every client | a login with `pg_monitor`; its password from a Secret, through `env` |
| Flows files | reads `/etc/teia/flows` every round | nothing: the chart mounts them |

So the pod needs to reach the Kubernetes API and your telemetry backends, and nothing else. If you use
NetworkPolicies, allow its egress to those and its ingress from Grafana.

## What it exposes

Port 8080 serves everything:

- `/` is the map;
- `/api/v1/` is the API the Grafana panel reads;
- `/metrics` exposes teia's own metrics, including edge and node health, for alerts;
- `/healthz` is for the probes and is always open.

With a token, everything else needs `Authorization: Bearer <token>`. Browsers open `/?token=<token>` once and keep a
session cookie.

## Install

**1. The image.** Releases publish `ghcr.io/nilum-br/teia`, the chart's default. Until the first release, build it
and push it to your registry:

```sh
docker build -t registry.example.com/platform/teia:0.1.0-dev .
docker push registry.example.com/platform/teia:0.1.0-dev
```

**2. Your values.** Point teia at what you run. Keep the token in a Secret you manage:

```sh
kubectl create namespace teia
kubectl -n teia create secret generic teia-auth --from-literal=token="$(openssl rand -hex 32)"
```

```yaml
# my-values.yaml
image:
  repository: registry.example.com/platform/teia   # only until the first release
  tag: 0.1.0-dev
auth:
  existingSecret: teia-auth
config:
  sources:
    prometheus:
      - name: prometheus
        url: http://prometheus-operated.monitoring:9090
    tempo:
      - name: tempo
        url: http://tempo.monitoring:3200
```

Not sure what your Prometheus or Tempo offer? Install first, then ask teia from inside the cluster. `detect` prints
the sources that work, ready to paste under `config.sources`:

```sh
kubectl -n teia exec deploy/teia -- teia detect --prometheus http://prometheus-operated.monitoring:9090 --kubernetes
```

**3. Install.**

```sh
helm install teia ./charts/teia -n teia -f my-values.yaml
helm test teia -n teia
kubectl -n teia port-forward svc/teia 8480:8080      # then open http://localhost:8480/?token=...
```

After the first tagged release, the chart will also be available as a release asset and in GHCR. For example,
once `v0.1.0-alpha.1` is published:

```sh
helm upgrade --install teia oci://ghcr.io/nilum-br/charts/teia --version 0.1.0-alpha.1 \
  -n teia --create-namespace -f my-values.yaml
```

The packaged chart's version and default image tag match the release. Until then, use the local chart and your
own image as above.

The **Next steps** tab lists what teia cannot see yet and how to fix each one, such as a missing permission or a
service with no telemetry of its own.

Every value is described in [`charts/teia/values.yaml`](../charts/teia/values.yaml). Every `config` key is in
[configuration.md](configuration.md). The chart validates its values, so a typo fails the install instead of being
ignored.

## Flows: files, not a CRD

A flows document looks like a Kubernetes manifest:

```yaml
apiVersion: teia.nilum.io/v1alpha1
kind: Flows
```

It is not one. `apiVersion` and `kind` version the **file format**, the same convention Backstage's
`catalog-info.yaml` and a kubeconfig follow. Nothing is registered in the cluster, and `kubectl apply` on a flows
file fails, because the cluster has no such resource. teia reads flows files from a directory, `/etc/teia/flows` in
the pod.

They belong in each service's repository, next to its `catalog-info.yaml`, and are reviewed with the code that adds
a call. To bring them into the cluster:

- **A few services:** list them under `flows:` in your values, one key per file.
- **Many teams:** build a ConfigMap from the services' repositories in CI or GitOps, and set `flowsConfigMap` to its
  name. For example:

  ```sh
  kubectl -n teia create configmap teia-flows-all --from-file=flows/ --dry-run=client -o yaml | kubectl apply -f -
  ```

teia picks changes up without a restart, within about a minute: the time the kubelet takes to update the mounted
files, plus one round. No flows yet? Start without them. The map then shows every communication as **not
declared**, and the advisor writes a flows file per service from what it observed, ready to review and commit.

Check files before committing them with `teia validate path/to/flows.yaml`, or let an editor check them against
[`schema/flows.schema.json`](../schema/flows.schema.json). The format is described in [flows-spec.md](flows-spec.md).

## Several clusters from one teia

One teia can map every cluster, each **on its own** (see [`clusters`](configuration.md#clusters)). Each cluster's
inventory, Prometheus and Tempo build a separate graph, so `app-a` in two clusters is two components with their own
traffic, errors and health: a failure in a quiet cluster is not hidden behind a busy one. The UI shows the cluster
next to the logo, with a picker when there are several; `?cluster=` selects one in links and in the API, and every
answer names its cluster.

For each remote cluster, create a read-only account there (the same ClusterRole as the chart's: `get`, `list` and
`watch` on pods, services and namespaces) and a kubeconfig that uses its token. Put one kubeconfig per cluster in a
Secret where teia runs, and point the chart at it:

```sh
kubectl -n teia create secret generic teia-kubeconfigs \
  --from-file=prod-east.yaml --from-file=prod-west.yaml
```

```yaml
# my-values.yaml
kubeconfigSecret: teia-kubeconfigs   # mounted read-only at /etc/teia/kube
config:
  clusters:
    - name: prod-east
      sources:
        kubernetes: {kubeconfig: /etc/teia/kube/prod-east.yaml}
        prometheus: [{url: https://prometheus.prod-east.example.com}]
        tempo: [{url: https://tempo.prod-east.example.com, headers: {X-Scope-OrgID: prod-east}}]
    - name: prod-west
      sources:
        kubernetes: {} # the cluster teia runs in: its own ServiceAccount
        prometheus: [{url: http://prometheus-operated.monitoring:9090}]
```

With `config.clusters`, the chart drops its default top-level inventory and adds the shared flows; any other
top-level source is rejected, since it would mix clusters.

The cluster teia runs in can use `kubernetes: {}` (its in-cluster account). The pod needs network access to each
cluster's API server and telemetry backends.

## Journey membership

Single membership can use the existing `journey`, `app.kubernetes.io/part-of` or `system` label. For a workload
shared by several journeys, place `teia.nilum.io/journeys: '["checkout", "browse"]'` in
`spec.template.metadata.annotations`. This JSON array augments the detected label without using invalid
comma-separated Kubernetes label values. An invalid value appears as an actionable finding and preserves
the workload. See [journeys](journeys.md#telling-teia-which-journeys-a-service-takes-part-in) for the complete
example, limits and ownership semantics.

## Connecting Grafana

A Grafana in the same cluster reaches teia at `http://teia.teia.svc:8080`. Use that as the Infinity data source's URL,
add the token header, and import the dashboard. The steps are in the README, under
[Use it in your Grafana](../README.md#use-it-in-your-grafana).

## Operating it

- **Upgrades:** `helm upgrade`. A change to `config` restarts the pod (its checksum is on the pod); a change to flows
  does not.
- **Token rotation:** changing `auth.token` through Helm restarts the pod automatically. If you manage the Secret
  with `auth.existingSecret`, update it and run `kubectl -n teia rollout restart deployment/teia`: environment
  variables are read only when the process starts. Update Grafana's data source credential too.
- **Existing permissions:** use `rbac.create: false`, `serviceAccount.create: false` and `serviceAccount.name`
  when your platform manages the reader account and its bindings. Its token still mounts for inventory. Set
  `serviceAccount.automountServiceAccountToken: false` only when in-cluster inventory is disabled.
- **Resources:** the defaults (50m CPU, 64 Mi requested, 512 Mi limit) fit a few hundred services. Memory grows with
  the number of services and connections teia keeps. Measured sizes, for many clusters and connections: [capacity](capacity.md).
- **One replica:** a second one would only double the queries to your telemetry. If the pod moves, the map is back
  after one round.
- **Its own health:** `serviceMonitor.enabled: true` scrapes `/metrics` (with the token, when there is one). The
  Sources tab, and `/api/v1/sources`, say which source failed and why.
- **teia stays off its own map:** its pod carries `teia.nilum.io/ignore: "true"`.
