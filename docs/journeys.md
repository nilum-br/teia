# Journeys and filters

A whole map answers "who talks to whom". During an incident or a review, people ask a narrower question: *what
does checkout go through, and why is it red?* teia can show one journey, or any slice of the map, on its own:

- the services that take part in it are drawn inside a **frame**;
- whatever lies outside the frame is drawn as a **port** at its edge.

![The checkout journey in its frame: its services, with its datastores, queue and gateways in the lane on the right, and a port where it leaves to browse](assets/screenshots/teia-journey.png)

## Telling teia which journeys a service takes part in

Nothing needs adding if your services already carry one of these labels. teia uses the first one it finds:

| Label | Where it comes from |
|---|---|
| `journey` | teia's own label |
| `app.kubernetes.io/part-of` | Kubernetes' recommended label: "the name of a higher level application this one is part of" |
| `system` | Backstage's system |

To use another label, set it with the panel's **Journey label** option or `?journeyKey=` on teia's page.

A service on several journeys lists them, separated by commas:

```yaml
# Docker Compose
labels:
  journey: checkout              # orders, payments
  journey: "checkout,browse"     # web, on both
```

```yaml
# Kubernetes (pod template labels)
metadata:
  labels:
    app.kubernetes.io/part-of: checkout
```

Datastores, queues and third parties need no label: they join the journeys whose services call them.

Kubernetes label values cannot contain commas. Until teia reads a list annotation, a service on several journeys
can be left unlabelled: it still joins the journeys of the services it calls or that call it, as described below.

## How a journey is built

1. **Members** are the nodes whose journey label holds the value.
2. From them, teia follows calls **downstream**:
   - datastores, caches, queues and externals are included, but never walked past, so a shared database's other
     clients do not join the journey;
   - a service labelled with **another** journey is where this one leaves;
   - unlabelled services are followed up to two hops past the last member.
3. The **headline** is the journey's worst health and the reason that most likely causes it. It shows on the bar
   above the map, for example `orders → payments · 4.1% errors`. When a reason only passes on a dependency's
   problem, the reason it points at wins, and a line wins over a node.

## The frame and its ports

Inside the frame, the journey reads left to right:

- the services the outside calls into start it;
- its datastores, queues and third parties end it, in one lane;
- the frame's header gives its name, its health and what enters it (traffic, p95 and errors).

Everything outside the frame becomes a port, a small chip beyond its edge:

| Port | Side | What it is |
|---|---|---|
| **Caller** | left | a service outside calling into the journey |
| **Exit** | right | where the journey leaves to, one chip per journey it reaches (`↗ browse · 2`) |
| **Tenant** | right, beside the datastore | another client of a datastore, queue or third party in the journey: its traffic counts in their health |

Each port carries the traffic and the health of the calls it stands for. Its line runs straight into the frame
when nothing is in the way, and along the frame's top or bottom edge otherwise, so it never crosses a card. Past
eight ports on a side (five in a Grafana panel), the quietest fold into one "+N more" chip. Click a port to see who
is outside, open their journey, or find them on the whole map.

## Filters

Journeys are one kind of filter. The **Filter** button (key `J`) opens every way the map can be narrowed, with how
many components hold each value and the worst health among them:

- journeys;
- every label your services share: `k8s.namespace`, `team`, `tier`, `compose.project`…;
- health, kind of component and technology;
- the communication itself: declared and seen, not declared, declared but not seen;
- which signal saw it: traces, metrics, logs, inventory.

![The Filter panel: journeys, compose project, namespace, system, team and tier labels, health, kind of component and technology, each value with its count and health; the map behind keeps only the degraded components](assets/screenshots/teia-filters.png)

Values of one filter add up (namespace `shop` **or** `billing`); different filters narrow each other (namespace
`shop` **and** failing). Each filter in use shows as a chip on the bar, removed with ✕. Any filtered view gets a
frame and ports like a journey's, and keeps its own layout when you move cards.

With no filter in use, the bar lists the journeys with their health, one click away.

The search box narrows too: type and press **Isolate** (`Shift+Enter`).

| Search | What it shows |
|---|---|
| `checkout` | the journey checkout |
| `team=payments` (or `team:payments`) | the components labelled `team=payments` |
| `orders` | orders and everything it depends on |
| `from:orders` | the same, written out |

## Sharing a view

The filters go in the address bar, so a view is a link:

- `?journey=checkout,browse` shows both journeys;
- `?filter=k8s.namespace:shop,health:failing` keeps what both say;
- `?journeyKey=team` names journeys after the `team` label.

In Grafana, the panel's **Journey** option takes a value or a dashboard variable such as `${journey}`. "All", or a
variable left unresolved, shows the whole map. The lab dashboard has a Journey variable.

## Limits, and what comes next

A label journey follows **ownership**, not single requests: a service shared by two journeys brings all of its calls.
The rules above keep that in check, and the ports show where the journey touches the rest.

Next, in this order:

1. **Journeys in the engine:** computed by teia itself, with their health in the API, changes, webhooks and metrics,
   and advisor findings such as a journey with no entry point.
2. **Journeys from traces:** each entry route (`GET /checkout` on web) becomes a journey made of the calls its
   traces actually make, which separates journeys that share a frontend.
3. **Declared journeys:** a `Journey` document next to the flows files, compared with what traces show.
