# Contributing

Thanks for helping. Issues and pull requests are welcome in English or Portuguese.

## Ground rules

- **Read-only, least privilege.** A source never writes to the systems it reads. Every finding that asks
  for access names the narrowest grant that works.
- **Never call silence absence.** Only report something as missing when teia could have seen it.
- **No client data in this repository.** Examples and tests use the lab or invented names.
- Keep the core features fully open source.

## Development

Go is optional: everything runs in containers.

```sh
make test       # go vet + go test, in a golang container
make lab-up     # the lab, with teia on :8480 and Grafana on :3480
```

Without `make`:

```sh
docker run --rm -v "$PWD:/src" -w /src golang:1.27.2 sh -c 'go vet ./... && go test ./...'
docker compose -f lab/compose.yaml up -d --build
```

### Behind a TLS-inspecting proxy

Corporate proxies that re-sign TLS break `go mod download` and plugin downloads inside containers. Export
your proxy's root CA as PEM into `certs/<name>.crt` (the folder is gitignored). The Dockerfiles install it
and the lab mounts it into Grafana. If even `docker pull` fails, your Docker Engine does not trust that CA
either: fix that in Docker itself, or pull through a buildx builder configured with the CA.

## Adding a source

1. Create `internal/source/<name>` implementing `source.Source`.
2. Return what you see as `Observation`. Raise findings about your **own** access (denied, missing grant)
   with a concrete `Fix`.
3. Set `Explores` if your source can look inside a node (like the postgres explorer).
4. Wire it in `internal/config`, document it in `docs/architecture.md`, and add its findings to
   `docs/advisor.md`.
5. Test it against a fake (see `servicegraph/source_test.go`), and add it to the lab if it can be staged.

## Commits and pull requests

- Small, focused PRs with tests.
- `gofmt`, `go vet` and `go test -race ./...` must pass (CI checks them).
- Sign-off is not required for now.
