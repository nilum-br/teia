# The map's visual language

teia is a tool people read during an incident, so its look follows one rule: **every mark means something, and the
map says the same thing inside Grafana as on its own page.** Fonts and surfaces change with the host (Grafana's
font and theme inside a dashboard); the grammar below does not.

![How the map draws what teia sees, and what it does not](assets/map-grammar.svg)

## The grammar

| Mark | Means |
|---|---|
| Solid line | Declared in a flows file and seen in traffic |
| Solid line with a **+** in the middle | Seen in traffic, declared nowhere: add it to a flows file, or ask why it exists |
| Dashed line | Declared, but no traffic seen while the caller was visible |
| Solid card | Seen first-hand: its own SDK, sidecar, eBPF probe or database explorer reports it |
| Hatched card | A **blind spot**: only its callers report it, so whatever it calls is invisible |
| Dashed card | Known from an inventory or a flows file, no traffic seen |
| Icon color, triangle, cross | Health: healthy, degraded, failing. Amber and red never mean anything else |
| Tag on a card or line | Why it is amber or red, with the number that crossed the rule |

The **plan line** at the top of the page counts the same marks, like a diff: declared and seen, seen but not
declared, declared but not seen, blind spots. Each count is a button that shows only those.

## Rules for changes

- Color is for health, selection and focus. A new state gets a shape or a stroke before it gets a color, and it
  goes in the legend.
- Dashed means "only declared" everywhere; hatched means "teia cannot see inside"; a dot is a unit of work. Do not
  reuse them for anything else.
- One sans-serif family, tabular numbers, small radii (6 px on cards, 1 px inside icons).
- No decorative motion, glow, blur, gradients, dot-grid backgrounds, uppercase eyebrow labels, colored side
  stripes or icons in colored tiles. The traffic dots move only when **Flow** is on, and Flow starts off when the system asks for reduced motion.
- Text says what, from where and since when. A blind spot is written as what teia cannot see, why, and how to
  see it.
- Icons follow the [icon library](icons.md#drawing-a-new-icon) rules.
- Check a change on the standalone page and in the Grafana panel, light and dark, desktop and mobile.
