# Backstage plugin (planned, v0.2)

Not implemented yet. The plan, informed by [the landscape research](../../docs/landscape.md):

| Package | What it does |
|---|---|
| `@nilum/backstage-plugin-teia-backend` | Backend module (new backend system). Reads the teia API and emits `teia-talksTo` / `teia-talkedToBy` relations, so the **existing** catalog graph shows observed communication without new UI. |
| `@nilum/backstage-plugin-teia` | Entity card: neighbours, ghost and stale edges, visibility score and the findings for that entity. Supports both the new frontend system and the legacy one. |

Identity: an entity maps to a teia node through the annotation `teia.nilum.io/node-id`, falling back to
`metadata.name`.

"Accept as declared" on a ghost edge opens a pull request that adds the generated `egress` entry to the
entity's flows file. That makes declaring cost one click, which is what keeps declared data from rotting.

The teia API is reached through Backstage's proxy (`/api/proxy/teia`).
