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
| `auth.existingSecret` / `auth.token` | empty | Turns on the API token: a Secret with the key `token`, or a token the chart stores. Empty leaves the API open. |
| `env` / `envFrom` | empty | Extra environment, such as passwords that `config` reads with `${NAME}`. |
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

Changing `auth.token` rolls out the pod. For `auth.existingSecret`, rotate the Secret and restart the Deployment
so it reloads the token from its environment. Use top-level `auth`, not `config.auth.token` (rejected by the schema).

CI runs strict lint with defaults and all optional resources, renders the chart, and checks account credentials,
token rotation and flows reload behavior with `go test ./charts/tests` (requires Helm on PATH).

Tagged releases package the chart with matching chart/app versions, attach it to the release with checksums,
and push it to `oci://ghcr.io/nilum-br/charts/teia` after the image is published. Manual release runs only build.
