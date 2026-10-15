# Installable candidate and release handoff

The repository can build and exercise an installable candidate without publishing a tag. A candidate is
**unsigned** and must be used only in an explicitly configured test Grafana. Public signing, catalog review,
human usability sessions and download verification of published assets remain separate release gates.

**Handoff decision:** the user has deferred creating the Grafana signing organization and arranging signing
until after delivery. This is an explicit post-delivery owner task, not a technical blocker for this unsigned
test candidate. The colleague will organize the human pilot after delivery. Neither external gate is marked
executed, and the tagged public-release workflow still requires signing credentials.

## Compatibility and evidence

The plugin declares Grafana `>=13.2.0`. The tested runtime matrix is deliberately narrower than that admission
rule; it does not certify future versions or every deployment architecture.

| Candidate validation | Environment | Result on 15 October 2026 |
|---|---|---|
| Fresh Grafana installation | Grafana 13.2.0, Linux amd64, Chromium/Playwright 1.64.1 | Passed: three typed sample nodes, two connections, selection, measured versus unknown errors |
| Grafana upgrade, same database volume | 13.2.0 to 13.2.4 | Passed with the same candidate bundle and sample dashboard |
| Full telemetry inputs | Grafana 13.2.4 + Infinity 4.1.1; direct Teia API | Covered by the separate lab API/browser suite; rerun after the final build |
| Fresh Helm installation and upgrade | Helm 4.3.1, kind 0.33.0, Kubernetes 1.37.0, Linux amd64 | Passed: health hook, authenticated graph, anonymous 401, inventory and externally managed ServiceAccount/RBAC |
| Persistent history through pod replacement | Same kind cluster, chart PVC enabled, current backend image | Passed: the selected historical snapshot returned identical JSON after the upgrade replaced its pod |
| Published signed package / catalog | External Grafana approval and signing credentials | Pending; not replaced by the unsigned candidate checks |

The final repeated panel matrix used `0.1.0-candidate.20261015.4`, ZIP SHA256
`4db7786712341351debf453b580b25487cbddb693e9e1524a29529d98fa362f4`. Both fresh 13.2.0 and
the same-volume upgrade to 13.2.4 passed `candidate.spec.ts`: typed data, selection, unknown errors,
empty query, invalid fields and missing data source. Local browser artifacts are under
`plugins/grafana/artifacts/candidate4-fresh-13.2.0` and `candidate4-upgrade-13.2.4`. Helm authentication,
external ServiceAccount upgrade and persistent-history checks were repeated with backend image
`sha256:33aa1dad7008787b563e9bbab7ef6a517fd02ee77cce76e35423237299c65afa`. This hash identifies
that tested build; subsequent changes require a newly packaged version and another run.

The sample uses Grafana's bundled [TestData data source](https://grafana.com/docs/grafana/latest/datasources/testdata/configure/),
so a colleague can verify panel installation before connecting real telemetry. Its values are synthetic and
have no fabricated observation timestamps. The normal lab separately validates the Infinity and direct paths.
Grafana recommends [runtime testing across supported versions](https://grafana.com/developers/plugin-tools/e2e-test-a-plugin/ci);
repeat this matrix after SDK or runtime updates. Build success alone is insufficient.

## Package and install locally

Build the panel with the pinned Node/npm versions in [the plugin README](../plugins/grafana/README.md). From a
PowerShell session at the repository root, package a unique candidate version:

```powershell
./scripts/package-candidate.ps1 -Version 0.1.0-candidate.20261015.4
```

The script copies the existing build into `dist/candidate/<version>/nilum-teia-panel`, creates a ZIP with that
single top-level directory and writes `checksums.txt`. It never signs, uploads or changes the original build.
It rejects an already signed input because changing its version would invalidate the signature. Always build
before packaging, retain the source commit and CI results, and choose a new version for each changed bundle.

Install that directory in the isolated Compose project:

```powershell
$env:TEIA_PLUGIN_DIR=(Resolve-Path dist/candidate/0.1.0-candidate.20261015.4/nilum-teia-panel).Path.Replace('\','/')
$env:GRAFANA_VERSION='13.2.0'
docker compose -p teia-candidate -f examples/grafana/compose.yaml up -d
```

Open `http://localhost:3481/d/teia-candidate-sample`. The Compose file binds only to loopback, allows this
specific unsigned plugin and provisions the small CSV example. It does not change your normal Grafana.
`TEIA_PLUGIN_DIR` can also point to a directory extracted from the ZIP. To test an upgrade, retain the same
Compose project/data volume, change `GRAFANA_VERSION` to `13.2.4` and run `up -d` again. To upgrade the plugin,
point `TEIA_PLUGIN_DIR` to a newly packaged version, then recreate Grafana. A changed version avoids stale
browser caches. Removing the test data volume starts a clean installation; do that only for this test project.

With the repository's browser runner built, run the opt-in fixture against either installation:

```powershell
docker compose -f lab/compose.yaml -f lab/compose.visual.yaml run --rm `
  -e GRAFANA_URL=http://host.docker.internal:3481 -e TEIA_CANDIDATE=true `
  -e GRAFANA_EXPECTED_VERSION=13.2.4 `
  -e TEIA_EXPECTED_PLUGIN_VERSION=0.1.0-candidate.20261015.4 `
  visual npm run e2e -- --project=chromium --workers=1 --output artifacts/candidate-13.2.4 candidate.spec.ts
```

On Linux, use a reachable host address or attach the runner to the candidate Compose network and use
`http://grafana:3000`. The test is skipped in the ordinary lab suite. The synthetic dashboard is also portable:
provision its TestData source and import [sample.json](../examples/grafana/dashboards/sample.json).

## Map real inputs

| Input | Required columns / behavior |
|---|---|
| Nodes (`refId=nodes`) | `id`: stable nonempty string; optional `kind`, `tech`, `visibility` |
| Connections (`refId=edges`) | `source` and `target`: node IDs; optional `id`, `protocol` |
| Traffic | `requestRate`: number in requests/second |
| Errors | `errorRate`: fraction 0–1; zero is measured zero, blank/null is unknown |
| Latency | `latencyP50Ms` / `latencyP95Ms`: nonnegative milliseconds, with coverage/approximation metadata where applicable |
| Timing, evidence, links and rules | See the [full frame contract](../plugins/grafana/README.md#numeric-frame-contract); requested dashboard time is not measurement time |

Start with the example, then replace its two queries with your mapped tables. Keep identities consistent;
do not format numbers with units in the query cells. For a Teia backend, import
[`lab/grafana/dashboards/teia.json`](../lab/grafana/dashboards/teia.json) and provision an Infinity source with UID
`teia`, Teia's server-reachable URL and that host in `allowedHosts`. Store a bearer header in the data source's
secure configuration, never in dashboard JSON. The existing [authenticated setup](../docs/kubernetes.md) and
[direct-mode constraints](../plugins/grafana/README.md#feeding-it) still apply.

An arbitrary-column mapping editor is not required by this candidate: the native CSV fixture and standard
query transformations make the typed contract testable. Record onboarding cases that still require repeated
manual mapping during the human pilot before choosing an editor or another data source.

## Exercise Helm without using an existing cluster

Create a new disposable kind cluster with an explicit, separate kubeconfig. The tested node image came from
the [kind 0.33.0 release](https://github.com/kubernetes-sigs/kind/releases/tag/v0.33.0). The release provides
checksums for the CLI and a digest for the node image. Build the final Teia image, then load it and Busybox:

```powershell
kind create cluster --name teia-candidate --kubeconfig ./bin/candidate-kubeconfig `
  --image kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
kind load docker-image teia-lab/teia:dev busybox:1.38.0 --name teia-candidate
./scripts/helm-smoke.ps1 -Kubeconfig ./bin/candidate-kubeconfig
```

The smoke script generates a temporary token without printing it, uses an explicit kubeconfig/context,
installs the chart, verifies `/healthz`, requires anonymous API rejection, reads the graph with the token and
checks Kubernetes inventory. It then upgrades to an externally managed ServiceAccount and RBAC while
retaining authentication. It removes only its unique namespace and RBAC afterward. Add `-History` to test a
history-enabled image and PVC: the exact stored snapshot must survive replacement of the pod. Namespace
cleanup also removes this test's retained PVC; ordinary Helm uninstall intentionally retains production history.

If a corporate CA blocks image download, use the existing trusted builder as described in
[troubleshooting](../CONTRIBUTING.md#troubleshooting). Do not bypass certificate verification. This smoke is
a real cluster check, separate from `helm lint` and the chart-render tests; a kind pass does not certify every
managed Kubernetes distribution.

## External release gates

1. Complete the human investigation pilot and resolve blocking issues against a concrete candidate.
2. Obtain Grafana's public plugin review/signature grant and a Grafana Cloud access-policy token with
   `plugins:write`, owned by the organization matching the plugin ID. Configure it as the repository secret
   `GRAFANA_ACCESS_POLICY_TOKEN`; never paste it into a report. Official
   [signing instructions](https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin) distinguish
   public approval from private signatures scoped to exact `rootUrls`. A private signature does not authorize
   unrestricted public distribution.
3. The release workflow builds unsigned candidates on every manual dispatch, including when selecting a tag,
   with a unique `0.0.0-candidate.<commit>`
   plugin version. Tagged releases now fail before
   packaging/publishing if signing credentials are absent, and require a generated `MANIFEST.txt` after signing.
   No tag or release was published by these validation steps. Catalog availability is still an external outcome,
   not a consequence of a passing build or local signature check.
4. After release authorization, download the tagged chart, image and ZIP from their published locations; verify
   checksums/provenance, install the signed ZIP with **no unsigned-plugin exception**, repeat fresh install and
   upgrade checks, and record actual URLs/digests and signature status. Only then close the distribution gate.
