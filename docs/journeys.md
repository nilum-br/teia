# Journeys

A whole map answers "who talks to whom". A journey answers a narrower question people ask during an incident or a
review: *what does checkout go through, and why is it red?* teia shows one journey on its own: the services that
take part in it, what they call, and stubs where it is entered from or leaves to the rest of the platform.

![The checkout journey on its own: its services, datastores, queue and gateway, with catalog and recommendations as stubs where it leaves](assets/screenshots/teia-journey.png)

## Telling teia which journeys a service takes part in

Give workloads a `journey` label. A service on several journeys lists them, separated by commas:

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
    journey: checkout
```

Every value becomes a journey in the map's **Journey** selector. Datastores, queues and third parties need no
label: they join the journeys whose services call them.

Kubernetes label values cannot contain commas. Until teia reads a list annotation, a service on several journeys
can be left unlabelled: it still joins the journeys of the services it calls or that call it, as described below.

## How a journey is built

1. **Members** are the nodes whose `journey` label holds the value.
2. From them, teia follows calls **downstream**:
   - datastores, caches, queues and externals are included, but never walked past, so a shared database's other
     clients do not join the journey;
   - a service labelled with **another** journey is where this one leaves: it is shown as a stub;
   - unlabelled services are followed up to two hops past the last member.
3. **Stubs** are the nodes right outside: who calls into the journey and where it leaves. They are drawn dashed and
   faded, without traffic animation. A node with more than three outside callers keeps its two busiest and one
   "+N callers" stub. Click a stub to see it on the whole map.
4. The **headline** in the journey's chip is its worst health and the reason that most likely causes it: a reason
   that only passes on a dependency's problem gives way to the one it points at, and a line wins over a node.
   For example `checkout · orders → payments · 4.1% errors`.

Any label works too, and so does "what this service depends on":

| Journey | What it shows |
|---|---|
| `checkout` | the services labelled `journey=checkout` and what they call |
| `team=payments` (or `team:payments`) | the same, for any label |
| `from:orders` | orders and everything it depends on, with no ownership boundary |

Group by, search, filters, export and the layout all work inside a journey; each journey keeps its own layout when
you move cards.

## Where to pick one

- **teia's UI:** the Journey selector in the top bar (key `J`), or type a search and press **Isolate**
  (`Shift+Enter`): a word that names a service isolates what it depends on. The view is a link:
  `?journey=checkout`.
- **Grafana:** the panel's **Journey** option takes a value or a dashboard variable such as `${journey}`; "All" or an
  unresolved variable shows the whole map. The lab dashboard has a Journey variable.

## Limits, and what comes next

A label journey follows **ownership**, not single requests: a service shared by two journeys brings all of its calls.
The rules above keep that in check, and the stubs show where the journey touches the rest.

Next, in this order:

1. **Journeys in the engine:** computed by teia itself, with their health in the API, changes, webhooks and metrics,
   and advisor findings such as a journey with no entry point.
2. **Journeys from traces:** each entry route (`GET /checkout` on web) becomes a journey made of the calls its
   traces actually make, which separates journeys that share a frontend.
3. **Declared journeys:** a `Journey` document next to the flows files, compared with what traces show.
