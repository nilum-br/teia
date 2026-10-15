# Contributing

Thanks for helping. Issues and pull requests are welcome in English or Portuguese.

## Ground rules

- **Read-only, least privilege.** A source never writes to the systems it reads. Every finding that asks
  for access names the narrowest grant that works.
- **Never call silence absence.** Only report something as missing when teia could have seen it.
- **No secrets and no customer data in this repository**: no tokens, passwords, certificates, real IP addresses,
  hostnames or names of companies. Examples and tests use the lab, invented names and `example.com`.
- Keep the core features fully open source.

## Prerequisites

| What | When |
|---|---|
| Docker with Compose v2 (Docker Desktop on Windows and macOS) | always: the lab, and Go or Node in containers |
| Go 1.27 (the `go` line in `go.mod`) | optional: every Go command below also runs in the `golang:1.27.2` image |
| Node 26.12.0 and npm 12.2.0 | only to work on the Grafana panel (`plugins/grafana`); the lab builds it in a container |
| `make` | optional: each target is a plain command, listed below |

Commands run from the repository root. Unless a block says otherwise, they work as-is in bash, zsh, PowerShell
and Git Bash.

## Fast loops

### 1. Unit tests (seconds)

```sh
go test ./...                                   # everything
go test ./internal/config/ -run TestParse -v    # one package, some tests
node --test internal/api/teia-ui.test.mjs       # the map component's pure logic
```

Without Go, the same in a container (the volumes cache modules and builds between runs):

```sh
# bash, zsh
docker run --rm -v "$PWD:/src" -w /src -v teia-gomod:/go/pkg/mod -v teia-gocache:/root/.cache/go-build golang:1.27.2 go test ./...
```

```powershell
# PowerShell
docker run --rm -v "${PWD}:/src" -w /src -v teia-gomod:/go/pkg/mod -v teia-gocache:/root/.cache/go-build golang:1.27.2 go test ./...
```

```sh
# Git Bash: stop it from rewriting /src and the volume paths, and pass a Windows path
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/src" -w /src -v teia-gomod:/go/pkg/mod -v teia-gocache:/root/.cache/go-build golang:1.27.2 go test ./...
```

`MSYS_NO_PATHCONV=1` matters for every `docker` command in Git Bash whose arguments or environment contain a
path such as `/src` or `/ui`: Git Bash would otherwise turn it into `C:/Program Files/Git/src`.

### 2. teia on your machine, against the lab (a restart is a few seconds)

Start the lab (below), then run teia from source with [`lab/teia.host.yaml`](lab/teia.host.yaml), which reads the
lab's published ports (Prometheus `:9480`, Tempo `:3280`, Loki `:3180`, PostgreSQL `127.0.0.1:5480`) and your own
Docker engine:

```sh
go run ./cmd/teia serve --config lab/teia.host.yaml --ui-dir internal/api/ui    # http://localhost:8080
go run ./cmd/teia discover --config lab/teia.host.yaml                           # one round, printed
```

Change Go code, stop it (Ctrl+C) and run it again. Run `orders-db`'s `GRANT pg_monitor TO teia;` (see the lab
commands) to see the postgres explorer find the hidden job.

### 3. UI edits without rebuilding anything

`--ui-dir DIR` (or `TEIA_UI_DIR`) serves the UI from disk instead of the copy built into the binary, so an edit to
`internal/api/ui/teia-ui.js` or `index.html` shows on the next browser reload. Above, that is already on.

Without Go, let the lab's teia serve the UI from your checkout (http://localhost:8480):

```sh
TEIA_LAB_UI_DIR=/ui docker compose -f lab/compose.yaml up -d teia                     # bash, zsh
```

```powershell
$env:TEIA_LAB_UI_DIR='/ui'; docker compose -f lab/compose.yaml up -d teia             # PowerShell
```

```sh
MSYS_NO_PATHCONV=1 TEIA_LAB_UI_DIR=/ui docker compose -f lab/compose.yaml up -d teia  # Git Bash
```

Run the same `up -d teia` without the variable to go back to the built-in copy (in PowerShell, first
`Remove-Item Env:TEIA_LAB_UI_DIR`). Backend changes without Go: `docker compose -f lab/compose.yaml up -d --build teia`.

The Grafana panel bundles its own copy of `teia-ui.js`. Rebuild it with
`docker compose -f lab/compose.yaml run --rm teia-panel`, then `docker compose -f lab/compose.yaml restart grafana`.
With Node 26.12.0: `cd plugins/grafana`, `npm exec --yes --package=npm@12.2.0 -- npm ci`, then
`npm run typecheck`, `npm run test:ci`, `npm run build`.

## The lab

```sh
docker compose -f lab/compose.yaml up -d --build    # first run: a few minutes (images, Go modules, the panel)
docker compose -f lab/compose.yaml exec teia teia discover
docker compose -f lab/compose.yaml exec orders-db psql -U postgres -d orders -c 'GRANT pg_monitor TO teia;'
docker compose -f lab/compose.yaml run --rm e2e     # the end-to-end check CI runs
docker compose -f lab/compose.yaml down -v          # stop and drop its data
```

| Port | What | Moved with |
|---|---|---|
| 3480 | Grafana with the teia panel | `GRAFANA_PORT` |
| 8480 | teia (API and UI) | `TEIA_PORT` |
| 9480 | Prometheus | `PROMETHEUS_PORT` |
| 3280 | Tempo | `TEMPO_PORT` |
| 3180 | Loki | `LOKI_PORT` |
| 5480 | PostgreSQL `orders-db` | `POSTGRES_PORT` |

Every port listens on this machine only (127.0.0.1): Grafana is anonymous and teia has no token in the lab.
`LAB_BIND=0.0.0.0` opens them to your network, for a demo on another screen.

```sh
GRAFANA_PORT=3481 TEIA_PORT=8481 docker compose -f lab/compose.yaml up -d                 # bash, zsh, Git Bash
```

```powershell
$env:GRAFANA_PORT=3481; $env:TEIA_PORT=8481; docker compose -f lab/compose.yaml up -d      # PowerShell
```

A second lab can run next to the first with another project name and other ports:
`docker compose -p teia-lab2 -f lab/compose.yaml up -d` (teia and Alloy only look at their own project).

In the lab, only two socket proxies mount the Docker socket; teia runs as a non-root user and reads containers
through one of them. See [Docker access](docs/sources.md#docker-access).

## Before a pull request

```sh
make check
```

runs what CI runs. Without `make`, the same checks:

```sh
gofmt -l .                       # must print nothing
go vet ./...
go test -race ./...              # -race needs cgo; in the golang container it works everywhere
go run ./cmd/teia validate examples/flows/*.yaml lab/flows/*.yaml
node --test internal/api/teia-ui.test.mjs
```

and, when you touched them, the panel (`npm run typecheck`, `npm run test:ci`, `npm run build` in
`plugins/grafana`) and the lab (`docker compose -f lab/compose.yaml run --rm e2e`, after `up -d --build`). In
PowerShell, `go run ./cmd/teia validate` needs the files listed, as it does not expand `*`:
`go run ./cmd/teia validate (Get-ChildItem examples/flows/*.yaml, lab/flows/*.yaml).FullName`.

- Small, focused PRs with tests.
- A new config key goes into `internal/config`, [`docs/configuration.md`](docs/configuration.md) and
  [`examples/teia.yaml`](examples/teia.yaml); a test fails until all three agree.
- Commits must not contain secrets, certificates or customer data (see the ground rules). Sign-off is not required
  for now.

## Adding a source

1. Create `internal/source/<name>` implementing `source.Source`.
2. Return what you see as `Observation`. Raise findings about your **own** access (denied, missing grant)
   with a concrete `Fix`.
3. Set `Explores` if your source can look inside a node (like the postgres explorer).
4. Wire it in `internal/config` (`plan()`), document it in `docs/configuration.md`, `docs/sources.md` and
   `docs/architecture.md`, and add its findings to `docs/advisor.md`.
5. Test it against a fake server, as `internal/source/prometheus/source_test.go` (`fakeProm`) and
   `internal/source/docker/source_test.go` do, and add it to the lab if it can be staged.

## Adding a Prometheus preset

A preset teaches the `prometheus` source one family of metrics. All of them live in
[`internal/source/prometheus/presets.go`](internal/source/prometheus/presets.go):

1. Define a `*Preset`:
   - `Name`, `Description`, and `Signal` (`source.SignalMesh`, `SignalNetwork`, ...);
   - `Detect`: a series selector specific enough not to match look-alikes; auto mode asks `count(<Detect>)`;
   - `Requests(window)`: one series per edge, `sum by` the labels `Map` reads; `Failures(window)`, when the metrics
     say which requests failed, with the same grouping; `NoRate: true` when the value is bytes or flows, not
     requests;
   - `Map(labels)`: returns `Result{Edge: &Edge{From, To, FromSelf, ToSelf, FromNS, ToNS, Protocol, ToKind}}`,
     `UnknownCallerOf` for a caller the metrics cannot name, or an empty `Result` to skip the series.
2. Add it to `Presets`. Order matters: earlier presets win on protocol and kind.
3. Test it in [`internal/source/prometheus/source_test.go`](internal/source/prometheus/source_test.go): add label
   sets to `TestPresetsReadTheirLabels`, and a `fakeProm` test (answers keyed by a fragment of the query) when the
   query does something special (`or` between reporters, failures, no rate).
4. Document it in the Presets table of `docs/sources.md` and the preset list in `docs/configuration.md`. The config
   accepts the new name on its own (`prometheus.Lookup`).

## Checking the Helm chart

With Helm on PATH, run the chart checks from the repository root:

```sh
helm lint charts/teia --strict
helm lint charts/teia --strict -f charts/teia/ci/full-values.yaml
go test -v ./charts/tests
```

The tests render the real chart and check external ServiceAccount credentials, token rotation, flows reload and
rejected configuration. They skip when Helm is absent; the dedicated CI job installs Helm and runs them. The
release workflow packages the chart with the release's version and image tag; a manual workflow run does not publish.

## Dependency maintenance

Target the latest stable release of every tool and direct dependency, including new majors and Node Current.
Breaking changes trigger migration work: adapt our code or replace obsolete tooling. Do not default to an
older LTS or suppress major updates. Prereleases and unreleased Git HEADs are not stable releases. Reviewed
**15 October 2026** (T16), including Go, npm, container images, Helm, observability and Actions:

| Component | Validated version |
|---|---|
| Go; Kubernetes SDK | 1.27.2; 0.37.1 (already current) |
| TypeScript CLI; compiler API compatibility package | 7.0.2; `@typescript/typescript6` 6.0.2 |
| Node; npm | 26.12.0 Current; 12.2.0 |
| Grafana SDK and lab host; React | 13.2.4; 19.3.1 |
| Jest; Playwright | 30.5.2; 1.64.1 |
| Webpack; webpack-cli; SWC | 5.111.1; 7.2.3; 1.16.18 |
| ESLint; typescript-eslint; ESLint React | 10.12.0; 8.71.2; 5.27.1 |
| Helm; setup-helm; chart test Busybox | 4.3.1; v5; 1.38.0 |
| Redis exporter | 1.94.0 |

Release checks use the [Go release feed](https://go.dev/dl/?mode=json), [Node release feed](https://nodejs.org/dist/index.json),
[npm registry](https://registry.npmjs.org/typescript/latest), [Helm releases](https://github.com/helm/helm/releases)
and [exporter releases](https://github.com/oliver006/redis_exporter/releases). Both Go modules, resolved npm locks,
Dockerfiles, Compose, Helm images and Actions are in scope. Other lab images were already current at review:
PostgreSQL 18.6, Redis 8.10.2, Prometheus 3.15.0, Tempo 3.1.0, Loki 3.7.8, Alloy 1.20.1, Collector 0.162.0 and Infinity 4.1.1.

Completed migrations, including breaking changes:

- Node 26.12.0 replaces 24.22.0 across builds, CI, release, Compose and `.nvmrc`; `@types/node` is 26.6.5.
  npm enforces the engine range. Removed the unused Grafana E2E auth fixture, whose engine capped Node at 24.
  The project-owned Playwright configuration now supports the anonymous lab or an explicit authentication
  storage state, with three real API integration tests. No existing browser tests were removed.
- ESLint 10 replaces 9. The Grafana/legacy React preset was replaced by the current
  [ESLint React TypeScript preset](https://eslint-react.xyz/docs/getting-started/typescript), official React Hooks
  rules and TypeScript rules. Project-specific Grafana checks are preserved; removed ESLint core rules use
  their maintained stylistic counterparts. Active config is `plugins/grafana/eslint.config.mjs`.
- TypeScript 7 has no compiler API. Its CLI is current; API consumers use Microsoft's latest
  [TS6 compatibility package](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/), not an old CLI.
- Kubernetes inventory uses the official client-go REST transport for read-only pod/service lists. Removing
  the full generated clientset also removes its legacy JSON patch v4 implementation from production and test
  imports, on Linux and Windows. The used module set drops from 51 to 32; tidy removes 13 indirect requirements.
  HTTP regressions cover pagination, namespaces, TLS, token-file authentication, timeout, cancellation and RBAC.
- The panel reads the public `ThemeContext` from `@grafana/data` with React 19's `use`, preserving live theme
  changes without importing the complete `@grafana/ui`/`runtime` dependency trees. The context is also exported
  by the minimum supported Grafana 13.2.0. Unused direct Emotion/i18n/schema requirements were removed; SDK-owned
  requirements remain transitive. A DOM regression checks dark/light changes without remounting the map.
- The active Webpack config is now `plugins/grafana/webpack.config.ts`, derived from the Grafana scaffold and
  reusing its bundling helpers. It replaces `eslint-webpack-plugin` (and unpatched micromatch/braces) with the
  supported ESLint API. Development lint/type checking, live reload, AMD externals, public path, metadata,
  assets and subresource integrity remain enabled. Review scaffold changes against this owned derivative.
- The Istanbul coverage-config loader uses a scoped `js-yaml` 5.4.3 override. Its `load` API remains compatible;
  this removes the old argparse/sprintf-js chain. Regressions exercise YAML inheritance, options and rejection
  of JavaScript tags; actual Jest coverage generation also passes. Remove this override once the loader updates.

Limits in current upstream packages remain explicit; direct dependencies being current does not mean every
transitive dependency uses its project's newest major:

- Kubernetes SDK 0.37.1 internally imports structured-merge-diff/v6 6.4.2 and YAML/v2 2.4.4; the newer stable API
  paths are merge-diff/v7 7.0.0 and YAML/v3 3.0.5. Our own YAML imports use v3. kube-openapi has no tagged stable
  release, so its snapshot remains the SDK's selected revision, not development HEAD. JSON patch v4 remains in
  third-party module metadata but is no longer compiled. Metadata-only dependencies are distinct from packages
  imported by our builds/tests; changing a top-level version cannot migrate API calls inside another SDK.
- OpenTelemetry 1.47 uses backoff/v5 5.0.3 internally; v6 6.0.1 changes public retry APIs. A direct module replacement was
  tested and rejected by Go because the new module also imports its own v6 path. No cosmetic compatibility
  shim or SDK fork is introduced merely to change the displayed version.
- webpack-subresource-integrity's `latest` tag is a release candidate; keep stable 5.1.0 until a stable successor.

Security remediation on 15 October: **npm audit now reports zero vulnerabilities**, down from 31, then 26
(3 high, 23 moderate). The vulnerable [braces](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm),
[sprintf-js](https://github.com/advisories/GHSA-hp3w-g68c-fv3c) and
[router compatibility](https://github.com/advisories/GHSA-wrjc-x8rr-h8h6) chains are removed from the installed tree,
without downgrades, audit exclusions or incompatible router substitution. The earlier scoped
`get-uri → basic-ftp@6.2.3` override remains, validated by a local FTP transfer. Recheck both overrides when
upstream releases change. This audit covers the plugin's complete npm tree, including development dependencies;
it does not certify the separately distributed Grafana server or its bundled libraries.

Fresh `govulncheck@latest` scans found no vulnerabilities in either Go module, including the residual SDK
dependencies above. They are upstream API-version constraints, not known vulnerabilities identified by this scan.
Removing them would require maintaining kubeconfig/exec authentication or the OTLP transport ourselves. Keep
the current official SDKs and recheck their releases instead of introducing incomplete replacements.

Dependabot runs weekly for Go, npm, Actions, Docker and Compose; major upgrades are enabled, with Grafana and
React packages grouped. `npm run deps:check` checks every locked direct dependency against the official registry,
resolves aliases, rejects prerelease tags as stable candidates and fails when an update is available. All **51**
remaining direct dependencies matched their current stable release after removing six unused/replaced requirements.
`npm run deps:audit` checks all severities and blocks CI on new advisories; `npm run test:ci` also runs the YAML
and Webpack lint integration regressions. For each update,
review prerelease/engine/peer metadata and run `make check`, `make lint`, plugin build, Helm checks and lab e2e.
Review `npm run deps:check`, `npm audit` and `go list -m -u all` in both modules. Also inspect newer Go module
paths (`/v2`, `/v3`, etc.): `-u` alone only checks the current module path. Verify the upstream limits above.
Keep Node/npm, Go and Grafana aligned across manifests, lockfiles, CI, Makefile, Docker/Compose and these instructions.
Chart image tags and compatibility exceptions need explicit review; automated update PRs do not replace it.

## Troubleshooting

**Behind a TLS-inspecting proxy.** Corporate proxies that re-sign TLS break `go mod download`, `npm ci` and Grafana's
plugin download inside containers. Export the proxy's root CA as PEM into `certs/<name>.crt` (the folder is
gitignored; each checkout or worktree has its own). The Dockerfiles install it, the panel build passes it to Node,
and the lab mounts it into Grafana. If `docker pull` itself fails, your Docker Engine does not trust that CA either:
fix that in Docker, or pull through a buildx builder that trusts it
(`docker buildx build --builder <that builder> --load -t <image> -f <file with "FROM image"> .`).

**A port is already in use** (`port is already allocated`, `address already in use`). Move it with the variables in
the table above, or find who holds it: `lsof -i :3480` (macOS, Linux), `ss -ltnp | grep 3480` (Linux),
`Get-NetTCPConnection -LocalPort 3480 | Select-Object OwningProcess` (PowerShell).

**Docker Desktop runs out of memory** (containers restarting, builds killed). Everything in the lab but Tempo fits
in 1 GB; Tempo grows with the traces it holds, to 3 to 4 GB within an hour. Give Docker 6 GB or more (Settings →
Resources; with the WSL 2 backend, `memory=` under `[wsl2]` in `%UserProfile%\.wslconfig`), or
`docker compose -f lab/compose.yaml restart tempo`.

**The panel build fails** (`teia-panel` exits, Grafana does not start): see
`docker compose -f lab/compose.yaml logs teia-panel`; it is usually TLS inspection (above).

**Root-owned files in `plugins/grafana`** left by an older lab: `sudo chown -R "$(id -u):$(id -g)" plugins/grafana/dist
plugins/grafana/src/teia/teia-ui.js`, once. The panel build now hands its output back to the checkout's owner.
