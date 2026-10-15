# Communication contract proposals

A proposal turns one observed, undeclared communication into a review artifact for its owning repository.
It contains a validated additive Flows fragment, the owner's rationale, endpoint identities, original
source evidence and a Markdown PR body. Generating a proposal does not mark the edge accepted, alter a
Flows file, permit traffic, create a branch or publish a pull request. After an operator merges the
declaration through the repository's review process, the next discovery reconciles it with traffic.

## API contract

`POST /api/v1/review/proposals` requires `Content-Type: application/json` and the same authentication as
other Teia API routes. No repository credentials are accepted. Request bodies are limited to 16 KiB.

```json
{
  "from": "orders",
  "to": "orders-db",
  "rationale": "Persist the orders accepted by checkout",
  "generatedAt": "2026-10-15T12:00:00.123456789Z"
}
```

`generatedAt` is the exact source snapshot timestamp, including fractional precision. Preserve the original
string when submitting from a browser; formatting it with a JavaScript `Date` can discard nanoseconds.
Add `historyId` to select a stored snapshot; its timestamp must also match. Without `historyId`, the server
checks the current snapshot. A `409` means the observation changed: refresh and review it again instead of
automatically retrying acceptance against a new observation. Missing/expired history returns `404`; invalid
input returns `400`, unsupported content type `415`, oversized requests `413`, and ineligible communication
or rationale `422`. Authentication failures use the existing API behavior.

The endpoint accepts only a selection and rationale, not caller-supplied graph/evidence or repository paths.
It rejects observed edges that are already declared, unobserved/missing edges, unresolved address identities,
missing endpoint records, invalid ports, and identities that cannot round-trip through a Flows reference.
The rationale must be nonblank and at most 4,096 bytes. Named identities without catalog references retain
an explicit ownership/alias qualification. The loaded snapshot's declaration flag may not cover all
repositories: review the latest owning file for duplicates and protocol/port conflicts before merging.

The response has this shape:

```json
{
  "version": 1,
  "id": "sha256:<proposal fingerprint>",
  "title": "Declare communication: orders -> orders-db",
  "rationale": "Persist the orders accepted by checkout",
  "source": {
    "snapshotId": "sha256:<normalized snapshot fingerprint>",
    "inputKind": "current-discovery",
    "generatedAt": "2026-10-15T12:00:00.123456789Z",
    "round": 7,
    "timeMode": "current"
  },
  "connection": {
    "from": "orders",
    "to": "orders-db",
    "protocol": "postgresql",
    "port": 5432,
    "identity": "named-identity: verify aliases and ownership",
    "endpoints": [],
    "sources": ["tempo"],
    "signals": ["traces"],
    "evidence": []
  },
  "advice": ["Review endpoint identities, aliases and ownership before changing the contract."],
  "files": [
    {"name": "flows.yaml", "mediaType": "application/yaml", "content": "..."},
    {"name": "proposal.md", "mediaType": "text/markdown", "content": "..."},
    {"name": "proposal.json", "mediaType": "application/json", "content": "..."}
  ]
}
```

`connection.endpoints` has two records with `id`, proposed entity `ref`, `kind`, and known `tech`/`namespace`.
When supplied, `connection.resolution` and each endpoint's `resolution` preserve the structured identity
method, input, source, candidates, alternatives and temporal basis. They describe the available evidence,
not an inferred confidence score; absent resolution stays absent. Resolution inputs can include observed IPs.
`connection.evidence` preserves individual collection, evaluation and original-sample timestamps, sampling
counts, metrics and retained flags. Empty evidence remains explicitly unknown. Arbitrary labels, address inventories,
configured URLs and credentials are not copied into the artifact; inspect operator-written rationale and
source metadata before publishing them. Topology itself can be sensitive.

Historical requests use `inputKind=stored-history` and include `source.historyId`. The snapshot fingerprint
is SHA256 of the reserialized model snapshot, independent of input JSON formatting and unknown extra fields.
It identifies content, not a digital attestation, downloadable archive ID or retention guarantee. The proposal
ID also depends on selection, rationale and provenance. Repeating the same inputs produces the same artifact.

The `proposal.json` file contains proposal metadata without the `files` array, avoiding recursive copies.
Clients can download the returned file contents immediately after preview. The same POST with
`?format=bundle` returns a ZIP containing those three fixed filenames; it rechecks the requested snapshot,
so a later download may require refreshing the review. Responses use `Cache-Control: no-store`.

## Offline and Grafana query mode

When a Grafana panel has no browser-reachable Teia API configured, export its snapshot and request as two
files. The CLI reads them without a server, GitHub login or browser credential:

```sh
teia review propose --snapshot snapshot.json --request request.json --format bundle --output proposal.zip
```

The request file uses the API shape above. Its `generatedAt` and optional `historyId` must match the exported
snapshot. It replaces `--from`, `--to` and `--rationale`, avoiding interpolation of identities or free text
into a shell command. The result is labelled `inputKind=provided-snapshot`: the CLI validates structure and
eligibility but cannot authenticate a local file's origin or confirm current repository state.

Named flags remain available for deliberate CLI use:

```sh
teia review propose --snapshot snapshot.json --from orders --to orders-db --rationale 'Persist checkout orders' --format markdown --output proposal.md
```

Formats are `json` (default), `markdown`, `flows`, and `bundle`. `--output` creates a new file with restrictive
permissions where supported and refuses to overwrite an existing path. Prefer it to shell redirection for
ZIP output, especially in Windows PowerShell. No `gh`, `git`, shell subprocess or network client runs during
proposal generation. Ordinary frame data without declared/observed intent should not be offered as an
architecture-acceptance action; export the discovery snapshot that actually carries that contract context.

## Operator-owned Git workflow

Extract the bundle outside the owning repository. Review its rationale, endpoint references, capture time,
retained status and each source period. Missing protocol/port stays missing; a source sample is not continuous
coverage. Compare the repository's current declarations, then create a branch and merge the single
`spec.egress` entry into the existing owner document. Preserve other flows and the repository's formatting.

```sh
git switch -c teia/accept-communication
# Merge the reviewed entry into the owning file.
teia validate path/to/flows.yaml
git diff --check
git diff -- path/to/flows.yaml
```

The fragment is not an applicable patch against an unknown repository revision. After the operator reviews
the actual diff and follows the repository's approval process, the optional publication commands are:

```sh
git add -- path/to/flows.yaml
git commit -m "Declare reviewed communication"
git push -u origin teia/accept-communication
gh pr create --draft --repo OWNER/REPOSITORY --base main --head teia/accept-communication --title "Declare reviewed communication" --body-file /path/to/proposal.md
```

Replace paths, repository and branch names deliberately. Authenticate GitHub CLI locally using the
organization's normal process. The [official CLI manual](https://cli.github.com/manual/gh_pr_create) documents
`--draft`, `--body-file`, `--base` and `--head`; a draft PR still publishes content. Its `--dry-run` option may
push changes, so it is not used for Teia's local preview. After pushing a reviewed branch, GitHub's
[compare page](https://docs.github.com/en/pull-requests/how-tos/commit-changes/comparing-commits) is an alternative:
open the owning repository's `/compare` and select its base and head branches. Teia does not invent a compare
URL before a repository and pushed branch exist.

## Validation

Go tests cover deterministic source/proposal identities, valid YAML and ZIP parity, unchanged input state,
unknown/retained timestamps, rejection of unsafe additions, literal rationale containing shell/Markdown
syntax, auth, CORS JSON preflight, exact timestamp conflicts, historical reads and bounded input. CLI tests
exercise the two-file handoff and refusal to overwrite an output. No test publishes a branch or PR.
