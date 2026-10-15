# Dependency status: 15 October 2026

The final T16 recheck found one actionable direct npm update: ESLint React 5.27.1 to **5.27.2**. It is applied
in the manifest and lockfile. All **51 direct npm dependencies** now match the latest stable release of their
actual package, including aliases. `npm run deps:check`, `npm run deps:audit` and `npm run lint` passed after
the update. The full installed npm tree has **zero reported vulnerabilities**. Fresh `govulncheck@latest`
scans also found no vulnerabilities in either the root Go module or `lab/demo`.

These are dated checks, not a guarantee against future advisories. There are still older transitive APIs in
current upstream SDKs and development tools. They are identified below rather than hidden by incompatible
overrides or incomplete forks. The maintenance policy and completed migrations are in
[CONTRIBUTING](../CONTRIBUTING.md#dependency-maintenance).

## Stable releases checked

Official release feeds reconfirmed Go **1.27.2**, Node **26.12.0 Current** and npm **12.2.0**. Node Current is
intentional; this project does not hold new work on an older LTS. The npm check also resolves the TypeScript
aliases: the CLI is TypeScript **7.0.2**, while API-consuming tools use Microsoft's current
`@typescript/typescript6` **6.0.2** compatibility package. Comparing that alias to the unrelated `typescript`
registry entry would incorrectly report it as an unfinished CLI upgrade.
Sources: [Go](https://go.dev/dl/?mode=json), [Node](https://nodejs.org/dist/index.json),
[npm](https://registry.npmjs.org/npm/latest),
[TypeScript 7](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/).

The recheck also confirmed these stable release tags against their official repositories:

| Component | Version |
|---|---|
| Grafana server and `@grafana/data` | 13.2.4 |
| Helm | 4.3.1 |
| golangci-lint | 2.14.0 |
| Prometheus | 3.15.0 |
| Tempo; Loki; Alloy | 3.1.0; 3.7.8; 1.20.1 |
| OpenTelemetry Collector | 0.162.0 |
| Redis; Redis exporter | 8.10.2; 1.94.0 |
| Grafana Infinity datasource | 4.1.1 |

Sources: [Grafana](https://github.com/grafana/grafana/releases/tag/v13.2.4),
[Helm](https://github.com/helm/helm/releases/tag/v4.3.1),
[golangci-lint](https://github.com/golangci/golangci-lint/releases/tag/v2.14.0),
[Prometheus](https://github.com/prometheus/prometheus/releases/tag/v3.15.0),
[Tempo](https://github.com/grafana/tempo/releases/tag/v3.1.0),
[Loki](https://github.com/grafana/loki/releases/tag/v3.7.8),
[Alloy](https://github.com/grafana/alloy/releases/tag/v1.20.1),
[Collector](https://github.com/open-telemetry/opentelemetry-collector-releases/releases/tag/v0.162.0),
[Redis](https://github.com/redis/redis/releases/tag/8.10.2),
[Redis exporter](https://github.com/oliver006/redis_exporter/releases/tag/v1.94.0),
[Infinity](https://github.com/grafana/grafana-infinity-datasource/releases/tag/v4.1.1).

`webpack-subresource-integrity` remains on stable **5.1.0**. Its registry `latest` tag points to **5.2.0-rc.1**,
which is a prerelease. The direct dependency checker filters prereleases rather than treating a tag name as
proof of stability. [Registry metadata](https://registry.npmjs.org/webpack-subresource-integrity/latest).

## Go: compiled dependencies versus module metadata

`go list -m -u all` describes the resolved module graph, including requirements recorded by dependencies whose
packages this application never imports. We also checked `go list -deps -test` for the root module on Linux
and Windows, and for the demo on Linux. A stale requirement in another module's metadata is not, by itself,
code included in Teia's binary. Conversely, current direct SDKs can genuinely compile older API generations.

| Compiled upstream dependency | Why it remains | Upgrade condition |
|---|---|---|
| `structured-merge-diff/v6` 6.4.2 in Kubernetes SDK 0.37.1 | The SDK imports the v6 module path; stable v7 7.0.0 is a different API/module. | Upgrade the Kubernetes SDK when it supports v7, then run inventory and authentication regressions. |
| `go.yaml.in/yaml/v2` 2.4.4 in the Kubernetes dependency chain | Current Kubernetes dependencies still use v2. Teia's own YAML imports already use v3 3.0.5. | Follow the SDK's migration; replacing an indirect module does not rewrite its callers. |
| `kube-openapi` revision `d427ff9ee9ad` | The SDK selects `v0.0.0-20260721132016-d427ff9ee9ad`; this project has no tagged stable release. `@latest` points at a newer development revision. | Adopt the revision selected by the next stable SDK, or a separately justified and tested fix. |
| `backoff/v5` 5.0.3 in the demo's OTLP HTTP exporter 1.47.0 | The exporter imports v5. Stable v6 6.0.1 changes retry APIs and imports its own v6 path. A direct replacement does not migrate the exporter. | Upgrade the official exporter when it supports v6; retain transport/retry coverage. |

The stable SDK manifests provide the dependency evidence:
[apimachinery 0.37.1](https://github.com/kubernetes/apimachinery/blob/v0.37.1/go.mod),
[client-go 0.37.1](https://github.com/kubernetes/client-go/blob/v0.37.1/go.mod),
[OTLP HTTP exporter 1.47.0](https://github.com/open-telemetry/opentelemetry-go/blob/v1.47.0/exporters/otlp/otlptrace/otlptracehttp/go.mod).

The root module's same-path update report contained 12 entries; only `kube-openapi` was imported by the
checked builds/tests. The other entries were metadata-only, including old `golang/protobuf`, `gofuzz`,
`testify`, `x/tools` and `gengo` requirements. All 20 reported same-path updates in the demo were likewise
outside its imported package set. Legacy JSON patch v4 remains in third-party module metadata but is not
imported by the checked root builds/tests. These counts describe this checkout and the checked platforms;
they are not a claim about every possible build tag or every application using those SDKs.

No new Go replacement, SDK fork or compatibility shim was added to make this report appear current.
Both actual Go module scans were clean despite the documented API-generation constraints.

## npm: development tools, SDK requirements and browser runtime

`npm outdated --all` still reports older transitive versions. It also reports absent optional peers,
TypeScript aliases and prerelease tags, so its raw count is not a count of vulnerabilities or shippable
upgrades. Even its `wanted` column can reflect a permissive peer range while the package that actually owns
the dependency requires an older major.

Examples verified against the current installed owners:

| Dependency in the installed tree | Current owner constraint | Decision |
|---|---|---|
| Babel core 7.29.7; stable 8.0.7 exists | Jest transform/config 30.5.2 require `^7.27.4`; React Hooks lint 7.1.1 requires `^7.24.4`. | Keep the supported toolchain; recheck when these owners migrate to Babel 8. |
| jsdom 26.1.0 and its types 21.1.7 | Jest environment 30.5.2 requires `jsdom ^26.1.0` and `@types/jsdom ^21.1.7`. | Do not force jsdom 30.1.3/types 30 across the environment's declared compatibility. |
| aria-query 5.3.0; stable 5.3.2 exists | Testing Library DOM 10.4.2 pins exactly `5.3.0`. | Follow its patch update; this is an exact upstream pin, not an omitted direct update. |
| marked 16.4.2; stable 18.1.1 exists | Grafana data 13.2.4 requires `^16.3.0`. | Follow the stable Grafana SDK's supported range. |

Sources: [Jest transform](https://registry.npmjs.org/@jest/transform/30.5.2),
[React Hooks lint](https://registry.npmjs.org/eslint-plugin-react-hooks/7.1.1),
[Jest jsdom environment](https://registry.npmjs.org/jest-environment-jsdom/30.5.2),
[Testing Library DOM](https://registry.npmjs.org/@testing-library/dom/10.4.2),
[Grafana data](https://registry.npmjs.org/@grafana/data/13.2.4).

The npm audit covers the **entire installed plugin tree, including development dependencies**. It does not
mean every installed package is embedded in `dist/module.js`. Webpack's
[externals configuration](../plugins/grafana/.config/bundler/externals.ts) leaves React, React DOM and
`@grafana/data` to the Grafana host. Their installed copies support compilation/tests; the host provides the
runtime implementations. npm's `dev` flag alone cannot establish what the browser bundle contains, and this
plugin audit does not certify the separate Grafana server distribution or its bundled libraries.

`@grafana/runtime` is not installed at all. The Grafana host provides it, and the panel uses two of its functions
(`locationService.partial` and `getTemplateSrv().getVariables`) to set the dashboard's slice variable.
[`src/grafana-runtime.d.ts`](../plugins/grafana/src/grafana-runtime.d.ts) declares them for TypeScript and
`jest.config.js` maps the module to a test stand-in. Installing the package would bring `@grafana/ui` and its
dependency tree back into the audited install.

Two deliberately scoped overrides remain necessary:

| Override | Current upstream requirement | Removal condition |
|---|---|---|
| `get-uri -> basic-ftp 6.2.3` | Latest get-uri 8.0.1 still requests `basic-ftp ^5.3.1`. | Remove after get-uri supports the patched major and the local FTP transfer regression still passes. |
| `@istanbuljs/load-nyc-config -> js-yaml 5.4.3` | Latest loader 1.1.0 still requests `js-yaml ^3.13.1`. | Remove after the loader updates and YAML inheritance, safety and Jest coverage regressions pass. |

Sources: [get-uri](https://registry.npmjs.org/get-uri/latest),
[Istanbul config loader](https://registry.npmjs.org/@istanbuljs/load-nyc-config/latest).
These overrides were previously exercised against their actual consumers. This final patch recheck reran
the direct-version check, full npm audit and lint; the complete release test suite remains the release gate.

## Repeatable checks and remaining maintenance

From `plugins/grafana`, with the pinned Node/npm versions:

```sh
npm ci
npm run deps:check
npm run deps:audit
npm outdated --all
npm run test:ci
npm run typecheck
npm run lint
npm run build
```

In the root Go module and again in `lab/demo`:

```sh
go list -m -u all
go list -deps -test -f '{{with .Module}}{{.Path}} {{.Version}}{{end}}' ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Check newer Go module paths separately: `-u` does not discover a `/v7` successor to `/v6`. Inspect the
owning package's declared ranges before changing an npm transitive dependency. Recheck the documented
SDK constraints and both override removal conditions when their owners release updates. Direct upgrades
continue through Dependabot and `deps:check`; the remaining constraints require supported upstream
migrations rather than a cosmetic lockfile rewrite.
