# Convenience targets. Everything runs in containers, so Go and Node are optional.
GO_IMAGE   ?= golang:1.27.2
# The image the lab builds the panel with (lab/compose.yaml, teia-panel).
NODE_IMAGE ?= node:26.12.0-trixie
# Keep in sync with the golangci-lint version in .github/workflows/ci.yml.
GOLANGCI_LINT_VERSION ?= v2.14.0

COMPOSE  := docker compose -f lab/compose.yaml
# certs/ holds extra CA certificates for TLS-inspecting proxies (see CONTRIBUTING.md).
GO       := docker run --rm -v "$(CURDIR):/src" -w /src -v teia-gomod:/go/pkg/mod -v teia-gocache:/root/.cache/go-build \
            -v "$(CURDIR)/certs:/certs:ro" -e SSL_CERT_DIR=/etc/ssl/certs:/certs $(GO_IMAGE)
NODE     := docker run --rm -v "$(CURDIR):/repo" -w /repo -v teia-panel-node-modules:/repo/plugins/grafana/node_modules $(NODE_IMAGE)
LINT     := docker run --rm -v "$(CURDIR):/src" -w /src -v teia-gomod:/go/pkg/mod -v teia-gocache:/root/.cache/go-build \
            -v teia-golangci-cache:/root/.cache/golangci-lint golangci/golangci-lint:$(GOLANGCI_LINT_VERSION)

# make check uses go and node from your PATH when they are installed, and the
# containers above otherwise.
ifneq ($(shell command -v go 2>/dev/null),)
GO_SH := sh -c
else
GO_SH := $(GO) sh -c
endif
ifneq ($(shell command -v node 2>/dev/null),)
NODE_SH := sh -c
else
NODE_SH := $(NODE) sh -c
endif

.PHONY: test check check-go check-ui lint build lab-up lab-down lab-report lab-grant lab-revoke stress stress-full

test:            ## vet and unit tests
	$(GO) sh -c 'go vet ./... && go test ./...'

# make check runs what CI runs, on Linux and macOS. On Windows, run it from WSL
# (or Git Bash with make installed), or run the same steps from PowerShell:
#   docker run --rm -v "${PWD}:/src" -w /src golang:1.27.2 sh -c 'test -z "$(gofmt -l cmd internal lab)" && go vet ./... && go mod tidy -diff && (cd lab/demo && go mod tidy -diff) && go test -race ./...'
#   docker run --rm -v "${PWD}:/src" -w /src golangci/golangci-lint:v2.14.0 golangci-lint run ./...
#   docker run --rm -v "${PWD}:/repo" -w /repo -v teia-panel-node-modules:/repo/plugins/grafana/node_modules node:26.12.0-trixie sh -c 'node --test internal/api/teia-ui.test.mjs internal/api/ui-integration-audit.test.mjs && cd plugins/grafana && npm exec --yes --package=npm@12.2.0 -- npm ci && npm run typecheck && npm run lint && npm run test:ci'
check: check-go check-ui ## everything CI checks: gofmt, vet, golangci-lint, go mod tidy, race tests, UI and panel tests

check-go:        ## gofmt, vet, go mod tidy, race tests, and golangci-lint if installed
	$(GO_SH) '\
	  set -e; \
	  unformatted=$$(find . -path "*/.*" -prune -o -path "*/node_modules" -prune -o -name "*.go" -print | xargs gofmt -l); \
	  if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi; \
	  go vet ./...; \
	  go mod tidy -diff; \
	  (cd lab/demo && go mod tidy -diff); \
	  go test -race ./...'
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; \
	else echo "golangci-lint is not installed: skipped. Run 'make lint' to use its container (CI always runs it)."; fi

check-ui:        ## map component tests, panel typecheck, lint and tests
	$(NODE_SH) '\
	  set -e; \
	  if ls certs/*.crt >/dev/null 2>&1; then cat certs/*.crt > /tmp/teia-extra-ca.pem; export NODE_EXTRA_CA_CERTS=/tmp/teia-extra-ca.pem; fi; \
	  node --test internal/api/*.test.mjs; \
	  cd plugins/grafana; \
	  [ node_modules/.package-lock.json -nt package-lock.json ] || npm exec --yes --package=npm@12.2.0 -- npm ci --no-audit --no-fund; \
	  npm run typecheck; \
	  npm run lint; \
	  npm run test:ci'

lint:            ## golangci-lint, in its container
	$(LINT) sh -c 'golangci-lint run ./... && cd lab/demo && golangci-lint run --config ../../.golangci.yml ./...'

build:           ## build the teia image
	docker build -t teia:dev .

lab-up:          ## start the lab (teia on :8480, Grafana on :3480)
	$(COMPOSE) up -d --build

lab-down:        ## stop the lab and drop its data
	$(COMPOSE) down -v

lab-report:      ## one discovery round, printed
	$(COMPOSE) exec -T teia teia discover

stress:          ## capacity check, two scenarios (~6 min; needs go, jq, bc, k6): compare with docs/capacity.md
	lab/stress/ladder.sh quick

stress-full:     ## capacity check, every scenario of docs/capacity.md (~25 min)
	lab/stress/ladder.sh full

lab-grant:       ## follow the advisor: let teia see inside orders-db
	$(COMPOSE) exec -T orders-db psql -U postgres -d orders -c 'GRANT pg_monitor TO teia;'

lab-revoke:      ## back to the blind starting point
	$(COMPOSE) exec -T orders-db psql -U postgres -d orders -c 'REVOKE pg_monitor FROM teia;'
