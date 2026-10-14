# Security policy

## Reporting a vulnerability

Please do not open a public issue, discussion or pull request for a security problem.

Report it privately through GitHub instead:
[**Report a vulnerability**](https://github.com/nilum-br/teia/security/advisories/new)
(the *Security* tab of the repository, then *Report a vulnerability*). Only the maintainers can see the
report, and we can work on the fix and the advisory with you there.

## Supported versions

teia is before 1.0 and moves fast, so fixes go to `main` and ship in the next release. Older releases do not
get backports.

| Version            | Supported |
| ------------------ | --------- |
| latest release     | yes       |
| `main`             | yes       |
| any older release  | no        |

## What to include

- The teia version (`teia version`) or commit, and how it runs: binary, container image, Kubernetes, or the
  Grafana panel.
- The part affected: HTTP API or UI, a source (Prometheus, Tempo, Loki, Kubernetes, Docker, PostgreSQL...),
  the Grafana panel, the release artifacts or the container image.
- Steps to reproduce or a proof of concept, and the impact you expect.
- The relevant part of your configuration, **with passwords, tokens, hostnames and IP addresses removed**.
- Whether and how you would like to be credited.

## What to expect

teia is maintained by a small team, so please be patient with us:

- We aim to acknowledge your report within a few working days.
- We will confirm whether we can reproduce it, and keep you posted on the fix.
- Once a fix is released we publish a GitHub security advisory (with a CVE when it applies) and credit you,
  unless you prefer otherwise.

Please keep the details private until the fix is released or we have agreed on a disclosure date together.

## Scope

In scope: the `teia` binary and container image, its HTTP API and web UI, the Grafana panel
(`nilum-teia-panel`), the release artifacts and the workflows in this repository.

Out of scope:

- **The lab** (`lab/`) is a local demo with default passwords, anonymous Grafana and access to the Docker
  socket. Do not expose it. Its defaults are not vulnerabilities, unless teia itself is affected.
- Vulnerabilities in dependencies that teia does not reach. Report those upstream; tell us if you find that
  teia is affected in a way that scanners such as `govulncheck` do not show.
