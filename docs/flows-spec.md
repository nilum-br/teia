# Flows specification (`teia.nilum.io/v1alpha1`)

A `Flows` document declares the outgoing communication of one service: who it calls, how, and why.
It lives in the service's repository, usually inside or next to `catalog-info.yaml`, so it is reviewed
in the same pull request as the code that introduces the call.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/nilum-br/teia/main/schema/flows.schema.json
apiVersion: teia.nilum.io/v1alpha1
kind: Flows
metadata:
  name: checkout
spec:
  owner: component:default/checkout
  egress:
    - to: component:default/payments-api
      protocol: grpc
      port: 443
      reason: authorize the payment
    - to: resource:default/checkout-db
      kind: database
      protocol: postgresql
      port: 5432
      reason: persist carts
    - to: external:api.stripe.com
      protocol: https
      port: 443
      reason: card processing
```

## Fields

| Field | Required | Meaning |
|---|---|---|
| `spec.owner` | yes | the caller, as a Backstage entity ref or a plain name |
| `spec.kind` | no | overrides the owner's kind (default `service`) |
| `spec.egress[].to` | yes | the callee: `component:`, `resource:`, `external:` or a plain name |
| `spec.egress[].kind` | no | `service`, `database`, `cache`, `queue`, `external`, `unknown` |
| `spec.egress[].protocol` | no | `http`, `grpc`, `postgresql`, `redis`, `kafka`... |
| `spec.egress[].port` | no | destination port; goes to the flow matrix |
| `spec.egress[].reason` | no, but please | why the call exists; reviewers and firewall teams read this |

## Refs and identity

The node ID is the last path segment of the ref, lowercased: `component:default/Orders` becomes `orders`.
`external:` refs become external nodes, which are excluded from the visibility score. When a name differs
between sources, map it with `aliases:` in the teia config.

## Only egress?

Yes, on purpose. Each service owns the calls it makes, so a change to who calls whom is reviewed by the
team that makes the call. Ingress views (who calls *me*) are derived from everyone's egress.

## Validation

```sh
teia validate catalog-info.yaml flows/*.yaml
```

Documents of other kinds in the same file are skipped. CI runs this on `examples/` and `lab/flows/`.

## Compatibility

`v1alpha1` may change. Planned additions: `expectedFrequency` (so rare flows such as monthly batches are
not reported as stale), `direction` for UDP and streaming, and CIDR destinations for flows that leave the
platform.
