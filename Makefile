# Convenience targets. Everything runs in containers, so Go is optional.
GO_IMAGE ?= golang:1.27.2
COMPOSE  := docker compose -f lab/compose.yaml
GO       := docker run --rm -v "$(CURDIR):/src" -w /src -v teia-gomod:/go/pkg/mod -v teia-gocache:/root/.cache/go-build $(GO_IMAGE)

.PHONY: test build lab-up lab-down lab-report lab-grant lab-revoke

test:            ## vet and unit tests
	$(GO) sh -c 'go vet ./... && go test ./...'

build:           ## build the teia image
	docker build -t teia:dev .

lab-up:          ## start the lab (teia on :8480, Grafana on :3480)
	$(COMPOSE) up -d --build

lab-down:        ## stop the lab and drop its data
	$(COMPOSE) down -v

lab-report:      ## one discovery round, printed
	$(COMPOSE) exec -T teia teia discover

lab-grant:       ## follow the advisor: let teia see inside orders-db
	$(COMPOSE) exec -T orders-db psql -U postgres -d orders -c 'GRANT pg_monitor TO teia;'

lab-revoke:      ## back to the blind starting point
	$(COMPOSE) exec -T orders-db psql -U postgres -d orders -c 'REVOKE pg_monitor FROM teia;'
