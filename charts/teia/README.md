# teia Helm chart

Installs [teia](https://github.com/nilum-br/teia) in a cluster as one read-only pod. It installs no CRD, no operator
and no agent.

```sh
helm install teia ./charts/teia -n teia --create-namespace -f my-values.yaml
```

What it runs, what it reads, the permissions it needs, and how flows files get in: see
[docs/kubernetes.md](../../docs/kubernetes.md).

## Values

| Value | Default | What it does |
|---|---|---|
| `image.repository` / `image.tag` | `ghcr.io/nilum-br/teia` / the chart's appVersion | The teia image. |
| `config` | inventory only | `teia.yaml` as is ([configuration](../../docs/configuration.md)). Add your Prometheus, Tempo and Loki under `config.sources`. The chart sets `listen`, `auth.token` and `sources.declared` itself. |
| `flows` | `{}` | Flows files, one key per file, mounted at `/etc/teia/flows`. |
| `flowsConfigMap` | empty | A ConfigMap you manage instead, each key a flows file. |
| `kubeconfigSecret` | empty | A Secret with one kubeconfig per remote cluster, mounted read-only at `/etc/teia/kube`, for [several clusters](../../docs/kubernetes.md#several-clusters-from-one-teia) (`config.clusters`). With clusters, the chart drops its default top-level inventory. |
| `auth.existingSecret` / `auth.token` | empty | Turns on the API token: a Secret with the key `token`, or a token the chart stores. Empty leaves the API open. |
| `env` / `envFrom` | empty | Extra environment, such as passwords that `config` reads with `${NAME}`. |
| `persistence.enabled` | `false` | Persist observation history at `/var/lib/teia/history`; sets `config.history.directory`, uses `Recreate` and defaults the pod storage group to 65532. |
| `persistence.existingClaim` | empty | Use an existing writable claim; otherwise create a retained `<release>-history` PVC. |
| `persistence.size` / `persistence.storageClass` | `1Gi` / cluster default | New claim size and storage class (`-` selects no storage class). History bounds remain under `config.history`. |
| `rbac.create` | `true` | The read-only ClusterRole: `get`, `list`, `watch` on pods, services and namespaces. |
| `serviceAccount.*` | created | Name and annotations of teia's ServiceAccount. |
| `serviceAccount.automountServiceAccountToken` | `true` | Mount the inventory credential, including for an externally managed account. Independent of `rbac.create`. |
| `service.*` | ClusterIP, 8080 | The Service in front of the UI, API and `/metrics`. |
| `ingress.*` | off | An Ingress for the UI and API. |
| `serviceMonitor.*` | off | A Prometheus Operator ServiceMonitor for `/metrics`, with the token when there is one. |
| `resources` | 50m, 64Mi requested; 512Mi limit | |
| `podSecurityContext` / `securityContext` | non-root, read-only, no capabilities | |
| `nodeSelector`, `tolerations`, `affinity`, `priorityClassName`, `podAnnotations`, `podLabels` | empty | Scheduling and metadata. |

`values.schema.json` checks these, so a misspelt key fails the install.

`helm test teia -n teia` calls `/healthz` from inside the cluster.

The optional history claim uses `ReadWriteOnce` and is kept on Helm uninstall (`helm.sh/resource-policy: keep`).
Remove that claim separately only when its observations are no longer needed. Do not share the same archive
directory between Teia processes. The container root filesystem remains read-only; only the archive mount is
writable. A storage driver that does not honor `fsGroup` needs a pre-provisioned claim with suitable ownership.

Changing `auth.token` rolls out the pod. For `auth.existingSecret`, rotate the Secret and restart the Deployment
so it reloads the token from its environment. Use top-level `auth`, not `config.auth.token` (rejected by the schema).

CI runs strict lint with defaults and all optional resources, renders the chart, and checks account credentials,
token rotation and flows reload behavior with `go test ./charts/tests` (requires Helm on PATH).

For a real disposable-cluster check, [the candidate smoke procedure](../../docs/installation-candidate.md#exercise-helm-without-using-an-existing-cluster)
verifies fresh install, authentication, inventory and upgrade to externally managed ServiceAccount/RBAC.
Its optional history mode also checks that the same stored snapshot survives pod replacement on a PVC.

Tagged releases package the chart with matching chart/app versions, attach it to the release with checksums,
and push it to `oci://ghcr.io/nilum-br/charts/teia` after the image is published. Manual release runs only build.
