# AGENTS.md

Rules for coding agents (and humans using them) working in this repository. [CONTRIBUTING.md](CONTRIBUTING.md)
still applies; this file adds what an agent must do before it reports a change as done.

## Commits and pull requests

- Never add an agent, assistant or model as author or co-author. No `Co-Authored-By:` trailers naming an agent,
  no "Generated with …" lines, no agent signatures in commits, PR descriptions, issues or code comments.
  The human who submits the change is its author.
- Do not commit or push unless asked. Never force-push `main`; add a new commit instead of amending a published one.
- Keep each commit to one intent, with a message in the imperative mood that says what changed and why.

## Regression tests are part of every change

- A bug fix starts with a test that reproduces the bug and fails on the current code. Keep it after the fix.
- A behaviour change updates the assertions that described the old behaviour to the new acceptance criterion.
  Do not delete a check to make a suite pass; if a limitation is now fixed, assert the fix.
- Preserve existing semantics that other users rely on. A new mode (for example per-call slices next to ownership
  journeys) gets its own tests, and the existing mode keeps its tests unchanged.
- Never present unknown data as measured: missing errors stay unknown, percentiles are never summed, approximate
  values stay marked approximate. Add a regression for any path that could break this.
- Cover the layers a change crosses: source adapters, `internal/graph`/`internal/engine`, the API
  (`internal/api/*_test.go`) and the shared UI core (`internal/api/teia-ui.test.mjs`). UI changes that affect the
  Grafana panel also run the panel tests in `plugins/grafana`.

Run before reporting a change as done, and report the actual output, including failures:

```sh
gofmt -l cmd internal lab        # must print nothing
go vet ./...
go test -race ./...
node --test internal/api/teia-ui.test.mjs internal/api/ui-integration-audit.test.mjs
make check                        # everything CI runs, when Docker is available
```

## Test on a local lab, not only with unit tests

Unit tests use fixtures; behaviour that depends on real telemetry (Tempo traces, Prometheus series, Kubernetes
inventory, the browser UI) must also be checked against a running lab before it is called done.

- Compose lab (default): `make lab-up`, then `docker compose -f lab/compose.yaml up -d --build teia` after each
  backend change, or run teia from source with `go run ./cmd/teia serve --config lab/teia.host.yaml`.
  `make lab-report` prints one discovery round; `lab/e2e.sh` runs the end-to-end scenario.
- Kubernetes lab: a local [kind](https://kind.sigs.k8s.io/) cluster with the workloads, Prometheus and Tempo, and a
  teia image built from **this checkout**. Tag the image with the commit (plus `-dirty` for uncommitted changes); never
  label a modified build as a published revision. Keep cluster access on a local kubeconfig and do not change the
  global Kubernetes context.
- Compare API values with the source's own query at the evaluation time teia reports (`evidence[].evaluatedAt`),
  not with a fresh query a few seconds later.
- For UI changes, drive a real browser (Chromium/Chrome via Playwright) on the standalone page, desktop and mobile
  widths, and save screenshots as evidence. Do not claim the Grafana panel works unless it was tested too.
- Lab ports bind to `127.0.0.1` only. Lab data, kubeconfigs, screenshots and other artifacts stay out of Git.
- If the lab cannot run (no container runtime, blocked network), say so explicitly and list which checks were
  skipped; do not report them as passed.

## Capacity checks

A change to what runs every round or on every request can make teia slower or hungrier at scale, which unit
tests and the labs do not show: sources (`internal/source/*`), `internal/graph`, `internal/engine`,
`internal/slice`, `internal/advisor`, and the API's snapshot, stream, Grafana, metrics and MCP handlers.

- Run `make stress` (two scenarios, about 6 minutes; needs Go, jq, bc and [k6](https://grafana.com/docs/k6/latest/set-up/install-k6/))
  before and after such a change, on the same machine, and compare both tables with
  [docs/capacity.md](docs/capacity.md). Memory within about 10% and latencies within about 30% are noise.
- Report the before and after tables in the pull request. A regression needs a reason; an improvement updates
  docs/capacity.md (`make stress-full` for every row).
- For a memory regression, read the heap profile the run keeps
  (`go tool pprof -top -sample_index=inuse_space <scenario>/bin/teiaprof <scenario>/heap.pprof`) and fix what it
  shows, with a test that pins the fix (shared data stays shared, work done once stays once).
- Report failed requests (the `Failed` column) even when they do not repeat; do not round them away.
- Results stay out of Git (they go to `$TMPDIR` by default).

## Hygiene

- No real company names, hostnames, credentials or production data in code, tests, examples or docs.
- Match the surrounding code: naming, comment density and idiom. Document new configuration keys in
  `docs/configuration.md` and `examples/teia.yaml` (a test enforces this).
