/* teia UI: the communication map as one dependency-free component.
 *
 * The same file runs the page teia serves and the Grafana panel
 * (plugins/grafana). It renders into a shadow root, so its styles never leak
 * into the host page and the host's never leak in.
 *
 *   const ui = TeiaUI.mount(element, { mode: 'page', api: 'api/v1/' });   // fetch + live stream
 *   const ui = TeiaUI.mount(element, { mode: 'panel' }); ui.update(snap); // data pushed in
 *
 * Two layers: TeiaUI.core is pure data logic (sanitising, grouping, layout,
 * search) with no DOM, tested under node; mount() draws it and owns every
 * listener, timer and request, all released by destroy().
 */
(function (global) {
  'use strict';

  const STYLE = `:host {
  --bg: #0d1120; --bg-2: #111627; --panel: #141a2a; --panel-2: #1b2234; --line: rgba(214, 222, 240, .09); --line-2: rgba(214, 222, 240, .17);
  --ink: #e7e9ed; --ink-2: #b6bcc6; --muted: #8d95a1; --accent: #6f9fff; --accent-2: #6f9fff; --accent-ink: #0b1220;
  --healthy: #4aae80; --degraded: #dfa23a; --failing: #ea5c5c; --unknown: #868e9b;
  --ghost: #dfa23a; --stale: #868e9b; --edge: #7a8494; --particle: #c3c9d3;
  --card: #161c2c; --card-hi: #1d2437; --field: #0d1120; --grid: transparent;
  --shadow: 0 1px 2px rgba(0, 0, 0, .3), 0 8px 24px rgba(0, 0, 0, .35);
  --critical: #ea5c5c; --warning: #dfa23a; --info: #6f9fff;
  --ok-text: #5cc595; --warn-text: #e5ad45; --err-text: #f27878;
  --radius: 8px;
  color-scheme: dark;
}
@media (prefers-color-scheme: light) {
  :host(:not([data-theme="dark"])) {
    --bg: #f3f4f6; --bg-2: #eceef1; --panel: #ffffff; --panel-2: #eef0f3; --line: #e1e4e9; --line-2: #cdd2da;
    --ink: #14171c; --ink-2: #383e49; --muted: #5a616d; --accent: #0052e0; --accent-2: #0052e0; --accent-ink: #ffffff;
    --healthy: #2c8f62; --degraded: #a87406; --failing: #d23b3b; --unknown: #7a818d;
    --ghost: #a87406; --stale: #7a818d; --edge: #78808d; --particle: #4b5361;
    --card: #ffffff; --card-hi: #f2f5fb; --field: #ffffff; --grid: transparent;
    --shadow: 0 1px 2px rgba(20, 23, 28, .06), 0 6px 18px rgba(20, 23, 28, .10);
    --critical: #d23b3b; --warning: #a87406; --info: #0052e0;
    --ok-text: #16764b; --warn-text: #875800; --err-text: #bd2f2d;
    color-scheme: light;
  }
}
:host([data-theme="light"]) {
  --bg: #f3f4f6; --bg-2: #eceef1; --panel: #ffffff; --panel-2: #eef0f3; --line: #e1e4e9; --line-2: #cdd2da;
  --ink: #14171c; --ink-2: #383e49; --muted: #5a616d; --accent: #0052e0; --accent-2: #0052e0; --accent-ink: #ffffff;
  --healthy: #2c8f62; --degraded: #a87406; --failing: #d23b3b; --unknown: #7a818d;
  --ghost: #a87406; --stale: #7a818d; --edge: #78808d; --particle: #4b5361;
  --card: #ffffff; --card-hi: #f2f5fb; --field: #ffffff; --grid: transparent;
  --shadow: 0 1px 2px rgba(20, 23, 28, .06), 0 6px 18px rgba(20, 23, 28, .10);
  --critical: #d23b3b; --warning: #a87406; --info: #0052e0;
  --ok-text: #16764b; --warn-text: #875800; --err-text: #bd2f2d;
  color-scheme: light;
}

* { box-sizing: border-box; }
:host { display: block; height: 100%; container: teia / inline-size; }
[hidden] { display: none !important; }
.teia {
  height: 100%; position: relative;
  background: var(--bg); color: var(--ink);
  font: 13px/1.45 "teia-plex", "IBM Plex Sans", system-ui, -apple-system, "Segoe UI", sans-serif;
  font-variant-numeric: tabular-nums; caret-color: var(--accent); scrollbar-color: var(--line-2) transparent;
  display: grid; grid-template-columns: minmax(0, 1fr); grid-template-rows: auto auto minmax(0, 1fr); overflow: hidden;
}
.teia ::selection { background: color-mix(in srgb, var(--accent) 32%, transparent); color: var(--ink); }
:is(.pane, .filter-panel, .legend, .entity-table, .share-dialog, .menu-list) { scrollbar-width: thin; }
button, input, select { font: inherit; color: inherit; }
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.search input:focus-visible, #map :focus-visible { outline: none; }
.search:focus-within { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 6px; }
.num { font-variant-numeric: tabular-nums; }

/* ---- top bar ---------------------------------------------------------- */
header {
  display: flex; align-items: center; gap: 8px 16px; padding: 8px 14px;
  background: var(--bg-2); border-bottom: 1px solid var(--line);
  min-height: 50px; flex-wrap: wrap;
}
/* teia's own page wears the Nilum navy on its top bar, in both themes: the
   brand's one surface. Lime marks what is on or focused there, nothing else. */
.teia.page header {
  --bg-2: #020f59; --panel: #0a155f; --panel-2: #0d1a70; --field: #020a33;
  --line: rgba(247, 248, 255, .10); --line-2: rgba(247, 248, 255, .22);
  --ink: #f7f8ff; --ink-2: #c9d0ee; --muted: #9aa6d6; --accent: #a9f549; --accent-ink: #020f59;
  --healthy: #46d18a; --degraded: #ffce4a; --failing: #ff6b6b; --unknown: #9aa6d6;
  --ok-text: #46d18a; --warn-text: #ffce4a; --err-text: #ff8f8f;
  color: var(--ink); color-scheme: dark; border-bottom-color: #020a33;
}
.brand { display: flex; align-items: center; gap: 8px; margin-right: 6px; }
.brand svg { width: 22px; height: 22px; border-radius: 5px; }
.brand b { font-size: 15px; font-weight: 600; }
.cluster { display: flex; align-items: center; gap: 8px; border: 1px solid var(--line-2); border-radius: 6px; padding: 2px 4px 2px 10px; min-height: 30px; max-width: 100%; }
.cluster[hidden] { display: none; }
.cluster-label { color: var(--muted); font-size: 12px; }
.cluster-name { font-size: 13px; font-weight: 600; padding-right: 6px; overflow-wrap: anywhere; }
.cluster select { font-weight: 600; max-width: 220px; }
/* The plan line: the map's grammar with its counts, read like a diff. */
.rings { display: flex; gap: 2px 4px; flex-wrap: wrap; }
.ring {
  display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; height: 26px; padding: 0 7px;
  border: 1px solid transparent; border-radius: 6px; background: none; cursor: pointer; color: var(--ink-2); font-size: 12px;
}
.ring:hover { background: var(--panel-2); }
.ring[aria-pressed="true"] { background: color-mix(in srgb, var(--accent) 13%, transparent); border-color: color-mix(in srgb, var(--accent) 55%, transparent); }
.ring b { font-size: 13px; font-weight: 600; color: var(--ink); }
.ring svg { flex: none; color: var(--ink-2); }
.gram .hatch { stroke-width: 1; stroke-opacity: .65; }
.spacer { flex: 1; }
.live { display: flex; align-items: center; gap: 7px; color: var(--muted); font-size: 12px; white-space: nowrap; }
.live i { width: 7px; height: 7px; border-radius: 50%; background: var(--unknown); }
.live.on i { background: var(--healthy); }
.live.paused i { background: var(--degraded); }
.live.error i { background: var(--failing); }
.live.error span { color: var(--err-text); }
.search { position: relative; }
.search input {
  width: 240px; height: 30px; background: var(--field); border: 1px solid var(--line-2); border-radius: 6px;
  padding: 0 10px 0 30px; outline: none;
}
.search input::placeholder { color: var(--muted); }
.search input:focus { border-color: var(--accent); }
.search svg { position: absolute; left: 10px; top: 8px; width: 14px; height: 14px; color: var(--muted); }
.ctl { display: flex; flex-wrap: wrap; gap: 6px; align-items: center; min-width: 0; max-width: 100%; }
.btn {
  display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 10px; border-radius: 6px;
  background: transparent; border: 1px solid var(--line-2); cursor: pointer; white-space: nowrap; text-decoration: none; color: var(--ink);
  font-weight: 500;
}
.btn:hover { background: var(--panel-2); }
.btn:disabled { opacity: .5; cursor: default; }
.btn:disabled:hover { background: transparent; }
.btn svg { width: 15px; height: 15px; color: var(--ink-2); flex: none; }
/* Icons are drawn at 20px; smaller ones get a heavier stroke so it stays about 1.3px. */
:is(.btn, .chip, .fchip, .fv, .legend, .feed, .search, .scope, .why) svg:not(.gram) { stroke-width: 1.75; }
.btn.on { background: color-mix(in srgb, var(--accent) 13%, transparent); border-color: color-mix(in srgb, var(--accent) 55%, transparent); }
.btn.on svg { color: var(--accent); }
.btn.icon { width: 30px; padding: 0; justify-content: center; }
#exportBtn svg { width: 13px; height: 13px; margin-right: -2px; }
select.btn { padding-right: 8px; min-width: 0; max-width: 100%; background: var(--field); }
.menu { position: relative; }
.menu-list {
  position: absolute; right: 0; top: 36px; z-index: 20; background: var(--panel); border: 1px solid var(--line-2);
  border-radius: 8px; box-shadow: var(--shadow); padding: 4px; min-width: 210px; display: none;
}
.menu.open .menu-list { display: block; }
.menu-list button, .menu-list a {
  display: block; width: 100%; text-align: left; padding: 7px 10px; border: 0; background: none; border-radius: 4px;
  cursor: pointer; color: var(--ink); text-decoration: none;
}
.menu-list button:hover, .menu-list a:hover { background: var(--panel-2); }

/* ---- layout ----------------------------------------------------------- */
main { display: grid; grid-template-columns: minmax(0, 1fr) 410px; min-height: 0; position: relative; }
.page.drawer-closed main { grid-template-columns: minmax(0, 1fr); }
.page.drawer-closed aside { display: none; }
#stage { position: relative; overflow: hidden; background: var(--bg); touch-action: none; }
#map { position: absolute; inset: 0; width: 100%; height: 100%; cursor: grab; user-select: none; }
#map.panning { cursor: grabbing; }

.floating {
  position: absolute; z-index: 5; background: var(--panel);
  border: 1px solid var(--line-2); border-radius: var(--radius); box-shadow: var(--shadow);
}
.chip {
  display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 9px; border-radius: 6px;
  border: 1px solid var(--line-2); background: transparent; cursor: pointer; font-size: 12px; color: var(--ink-2);
}
.chip .dot { width: 8px; height: 8px; border-radius: 50%; }
.chip .ln { width: 16px; height: 0; border-top: 2px solid; }
.chip b { color: var(--ink); font-weight: 600; }
.chip.off { opacity: .45; }
.chip svg { width: 12px; height: 12px; }
.sep { width: 1px; background: var(--line-2); margin: 3px 2px; }
.legend { left: 12px; bottom: 12px; padding: 10px 12px; font-size: 12px; color: var(--ink-2); display: grid; grid-template-columns: repeat(3, auto); gap: 5px 18px; max-width: calc(100% - 80px); max-height: calc(100% - 24px); overflow: auto; }
.legend .lg { display: flex; align-items: center; gap: 7px; white-space: nowrap; }
.legend .lg svg.gram { flex: none; color: var(--ink-2); }
.legend .lg.healthy svg { color: var(--healthy); } .legend .lg.degraded svg { color: var(--degraded); } .legend .lg.failing svg { color: var(--failing); }
.legend h4 { margin: 0 0 2px; font-size: 12px; font-weight: 600; color: var(--ink); grid-column: span 1; }
.legend h4.span3 { grid-column: 1 / -1; margin-top: 8px; }
.legend .lg.icon svg { width: 16px; height: 16px; color: var(--ink-2); flex: none; }
.teia .floating.legend:not(.closed) { padding-right: 36px; } /* room for its close button */
.legend.closed { display: block; padding: 0; }
.legend .open-legend { display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 12px; border: 0; background: none; color: var(--ink-2); cursor: pointer; font-size: 12px; font-weight: 500; }
.legend .open-legend svg { width: 14px; height: 14px; }
.legend .close-legend { position: absolute; top: 6px; right: 6px; width: 24px; height: 24px; border: 0; border-radius: 4px; background: none; color: var(--muted); cursor: pointer; display: grid; place-items: center; padding: 0; }
.legend .close-legend svg { width: 14px; height: 14px; }
.legend .close-legend:hover { background: var(--panel-2); color: var(--ink); }
.legend-toggle { grid-column: 1 / -1; justify-self: start; background: none; border: 0; color: var(--accent); cursor: pointer; padding: 2px 0 0; font-size: 12px; }
.zoom { right: 12px; bottom: 12px; display: flex; flex-direction: column; padding: 3px; gap: 2px; }
.zoom .btn { width: 30px; height: 30px; padding: 0; justify-content: center; border-color: transparent; }
.zoom .btn:hover { background: var(--panel-2); }
.zoom .btn svg { width: 16px; height: 16px; }
.toasts { position: absolute; right: 12px; top: 12px; z-index: 6; display: flex; flex-direction: column; gap: 8px; width: 330px; pointer-events: none; }
.toast {
  position: relative; background: var(--panel); border: 1px solid var(--line-2); border-radius: 8px;
  padding: 9px 12px 9px 28px; box-shadow: var(--shadow); font-size: 12.5px; animation: toast-in .15s ease-out; pointer-events: auto;
  text-align: left; color: var(--ink); cursor: pointer;
}
.toast::before { content: ""; position: absolute; left: 12px; top: 14px; width: 7px; height: 7px; border-radius: 2px; background: var(--info); }
.toast.warn::before { background: var(--degraded); } .toast.bad::before { background: var(--failing); } .toast.good::before { background: var(--healthy); }
.toast small { color: var(--muted); display: block; font-size: 11.5px; }
@keyframes toast-in { from { transform: translateY(-4px); opacity: 0; } to { transform: none; opacity: 1; } }
.empty-state { position: absolute; inset: 0; display: grid; place-items: center; padding: 24px; text-align: center; color: var(--muted); pointer-events: none; }
.empty-state button { pointer-events: auto; }

/* ---- side panel ------------------------------------------------------- */
aside { background: var(--panel); border-left: 1px solid var(--line); display: grid; grid-template-rows: auto 1fr; min-height: 0; }
.tabs { display: flex; flex-wrap: wrap; border-bottom: 1px solid var(--line); padding: 0 8px; }
.tab {
  padding: 11px 10px; border: 0; background: none; cursor: pointer; color: var(--muted); border-bottom: 2px solid transparent;
  font-weight: 500; font-size: 13px; display: flex; gap: 6px; align-items: center; margin-bottom: -1px;
}
.tab:hover { color: var(--ink); }
.tab.on { color: var(--ink); border-bottom-color: var(--accent); }
.tab .count { color: var(--muted); font-size: 12px; font-weight: 500; }
.pane { overflow-y: auto; padding: 14px 16px 30px; display: none; }
.pane.on { display: block; }
.pane h3 { font-size: 12.5px; font-weight: 600; color: var(--ink-2); margin: 22px 0 8px; }
.pane h3:first-child { margin-top: 2px; }
.card-f { border-top: 1px solid var(--line); padding: 12px 0 10px; }
.card-f:first-of-type { border-top: 0; padding-top: 4px; }
.card-head { display: block; width: 100%; padding: 0; border: 0; background: none; text-align: left; cursor: pointer; }
.card-head:hover .h4 { color: var(--accent); }
.card-f .meta { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--muted); }
.card-f .meta::before { content: ""; width: 7px; height: 7px; border-radius: 2px; background: var(--info); flex: none; }
.card-f.warning .meta::before { background: var(--warning); } .card-f.critical .meta::before { background: var(--critical); }
.card-f .h4 { display: block; margin: 3px 0 4px; font-size: 13.5px; font-weight: 600; color: var(--ink); }
.card-f p { margin: 0 0 6px; color: var(--ink-2); }
.card-f .next { color: var(--ink); }
.snippet { position: relative; }
.snippet pre {
  background: var(--field); border: 1px solid var(--line); border-radius: 6px; padding: 9px 10px; overflow-x: auto;
  font: 11.5px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace; margin: 6px 0 0; color: var(--ink);
}
.snippet .copy { position: absolute; right: 6px; top: 6px; height: 24px; font-size: 11.5px; padding: 0 8px; background: var(--panel); }
details summary { cursor: pointer; color: var(--accent); font-size: 12px; margin-top: 4px; }
.hero { display: flex; gap: 12px; align-items: flex-start; margin-bottom: 12px; }
.hero .tile { width: 28px; height: 28px; display: grid; place-items: center; flex: none; margin-top: 1px; color: var(--ink-2); }
.hero .tile.healthy { color: var(--ok-text); } .hero .tile.degraded { color: var(--warn-text); } .hero .tile.failing { color: var(--err-text); }
.hero .tile svg { width: 20px; height: 20px; }
.hero h2 { margin: 0 0 4px; font-size: 17px; font-weight: 600; line-height: 1.3; word-break: break-all; }
.why { display: grid; grid-template-columns: 22px 1fr auto; gap: 8px; align-items: center; padding: 8px 10px; margin-bottom: 6px;
  border-radius: 6px; border: 1px solid var(--line-2); background: var(--panel-2); }
.why svg { width: 18px; height: 18px; }
.why.degraded svg { color: var(--warn-text); } .why.failing svg { color: var(--err-text); }
.why .btn { height: 26px; font-size: 12px; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
.search .isolate { position: absolute; right: 3px; top: 3px; height: 24px; padding: 0 8px; font-size: 12px; background: var(--panel-2); }
.scope { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 6px; padding: 6px 12px; border-bottom: 1px solid var(--line);
  background: var(--bg-2); }
.scope .caption { color: var(--muted); font-size: 12px; margin: 0 2px 0 8px; white-space: nowrap; }
.filter-btn .count { color: var(--accent); font-weight: 600; font-size: 12px; }
.fchip { display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 3px 0 9px; border-radius: 6px; white-space: nowrap;
  background: color-mix(in srgb, var(--accent) 10%, transparent); border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent); color: var(--ink); font-size: 12px; font-weight: 500; }
.fchip button { height: 20px; min-width: 20px; border: 0; border-radius: 4px; background: none; color: var(--ink-2); cursor: pointer; padding: 0 5px; }
.fchip button:hover { background: var(--panel-2); color: var(--ink); }
.fchip button svg { width: 12px; height: 12px; display: block; }
.fchip.slice { border-style: dashed; }
.scope .slice-note { text-transform: none; letter-spacing: 0; }
.scope .clear { border: 0; background: none; color: var(--accent); cursor: pointer; font-size: 12px; white-space: nowrap; }
.filter-panel { position: absolute; z-index: 30; left: 12px; top: 104px; width: min(760px, calc(100% - 24px)); max-height: min(70vh, calc(100% - 130px)); overflow: auto;
  background: var(--panel); border: 1px solid var(--line-2); border-radius: var(--radius); box-shadow: var(--shadow); padding: 14px 16px 16px; }
.time-context { flex-basis: 100%; color: var(--muted); font-size: 12px; line-height: 1.4; overflow-wrap: anywhere; }
.filter-panel .top { display: flex; gap: 10px; align-items: center; margin-bottom: 10px; }
.filter-panel .top input { flex: 1; height: 30px; background: var(--field); border: 1px solid var(--line-2); border-radius: 6px; padding: 0 10px; outline: none; }
.filter-panel .top input:focus { border-color: var(--accent); }
.filter-panel .facets { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 14px 18px; }
.filter-panel h5 { margin: 0 0 6px; font-size: 12px; font-weight: 600; color: var(--ink-2); }
.filter-panel .values { display: flex; flex-direction: column; gap: 1px; }
.fv { display: flex; align-items: center; gap: 8px; width: 100%; padding: 4px 6px; border: 1px solid transparent; border-radius: 4px; background: none;
  color: var(--ink-2); cursor: pointer; text-align: left; font-size: 12.5px; }
.fv:hover { background: var(--panel-2); color: var(--ink); }
.fv.on { background: color-mix(in srgb, var(--accent) 10%, transparent); color: var(--ink); }
.fv .dot { width: 7px; height: 7px; border-radius: 50%; flex: none; }
.fv .check { width: 14px; height: 14px; border: 1.5px solid var(--line-2); border-radius: 3px; flex: none; display: grid; place-items: center; font-size: 10px; color: transparent; }
.fv.on .check { background: var(--accent); border-color: var(--accent); color: var(--accent-ink); }
.fv .check svg { width: 11px; height: 11px; stroke-width: 3; }
.fv .n { margin-left: auto; color: var(--muted); font-size: 12px; }
.fv svg { width: 13px; height: 13px; }
.jtab { display: inline-flex; align-items: center; gap: 7px; height: 26px; padding: 0 9px; border-radius: 6px; white-space: nowrap;
  border: 1px solid transparent; background: none; color: var(--ink-2); cursor: pointer; font-size: 12.5px; font-weight: 500; }
.jtab:hover { background: var(--panel-2); color: var(--ink); }
.jtab.on { background: var(--panel-2); border-color: var(--line-2); color: var(--ink); }
.jtab .dot { width: 7px; height: 7px; border-radius: 50%; flex: none; }
.jtab .x { margin-left: 2px; color: var(--muted); }
.scope select.btn { height: 26px; }
.scope .headline { margin-left: auto; display: inline-flex; flex-wrap: wrap; align-items: center; gap: 4px 8px; min-width: 0; font-size: 12.5px; color: var(--ink-2); }
.scope .headline b { overflow-wrap: anywhere; }
.scope .headline svg { width: 15px; height: 15px; }
.scope .headline.failing svg { color: var(--err-text); } .scope .headline.degraded svg { color: var(--warn-text); }
.scope .headline b { color: var(--ink); }
.scope .headline .btn { height: 24px; padding: 0 8px; font-size: 12px; }
.teia.panel .scope { padding: 4px 8px; }
.legend .reasons { grid-column: 1 / -1; display: flex; flex-wrap: wrap; gap: 4px 12px; }
.legend .reasons svg { width: 14px; height: 14px; color: var(--ink-2); flex: none; }
.pill { display: inline-flex; align-items: center; gap: 5px; border-radius: 4px; padding: 0 6px; font-size: 12px; border: 1px solid var(--line-2); color: var(--ink-2); }
.pill .dot { width: 7px; height: 7px; border-radius: 50%; }
.kv { display: grid; grid-template-columns: 130px 1fr; gap: 6px 10px; }
.kv dt { color: var(--muted); }
.kv dd { margin: 0; word-break: break-word; }
.labels { display: flex; flex-wrap: wrap; gap: 5px; }
.label { font: 11.5px/1.6 ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace; border: 1px solid var(--line-2); border-radius: 4px; padding: 0 6px; background: transparent; color: var(--ink); }
button.label { cursor: pointer; }
button.label:hover { border-color: var(--accent); }
.label b { color: var(--muted); font-weight: 400; }
.rel { display: flex; align-items: center; gap: 8px; width: 100%; padding: 6px 8px; border: 0; border-radius: 4px; background: none; text-align: left; cursor: pointer; }
.rel:hover { background: var(--panel-2); }
.rel .dot { width: 7px; height: 7px; border-radius: 50%; flex: none; }
.rel span { color: var(--muted); margin-left: auto; font-size: 12px; }
.explain { color: var(--ink-2); margin: 4px 0 10px; }
.feed .ev {
  display: grid; grid-template-columns: 58px 18px 1fr; gap: 6px; padding: 7px 4px; align-items: start; width: 100%;
  border: 0; border-bottom: 1px solid var(--line); background: none; text-align: left; cursor: pointer;
}
.feed .ev:hover { background: var(--panel-2); }
.feed .ev time { color: var(--muted); font-size: 12px; }
.feed .ev svg { width: 15px; height: 15px; margin-top: 2px; }
.feed .ev.new { animation: toast-in .2s ease-out; }
.src { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; padding: 8px 0; border-bottom: 1px solid var(--line); }
.src small { display: block; color: var(--muted); }
.state { font-size: 12px; border-radius: 4px; padding: 0 6px; border: 1px solid var(--line-2); white-space: nowrap; }
.state.ok { color: var(--ok-text); } .state.degraded { color: var(--warn-text); } .state.error { color: var(--err-text); }
.muted { color: var(--muted); }
.map-help { position: absolute; left: 12px; top: 8px; z-index: 4; max-width: calc(100% - 24px); display: flex; flex-wrap: wrap; gap: 4px 8px; align-items: center; background: var(--panel); border: 1px solid var(--line-2); border-radius: 6px; padding: 4px 8px; color: var(--ink-2); font-size: 12px; }
.map-help button { border: 0; background: none; color: var(--accent); cursor: pointer; min-height: 24px; }
.evidence-card { border-top: 1px solid var(--line); padding: 10px 0 6px; margin: 0; }
.evidence-card:first-of-type { border-top: 0; padding-top: 2px; }
.evidence-card p { margin: 4px 0; overflow-wrap: anywhere; }
.evidence-card .kv { grid-template-columns: 95px 1fr; margin: 8px 0; font-size: 12px; }
.detail-actions { display: flex; flex-wrap: wrap; gap: 6px; margin: 8px 0 12px; }
.pane :is(.label, summary, .rel, .clear) { min-height: 24px; }
.filter-panel summary { min-height: 28px; }
.share-dialog { background: var(--panel); color: var(--ink); border: 1px solid var(--line-2); border-radius: 10px; padding: 18px; width: min(560px, 90vw); box-shadow: var(--shadow); }
.share-dialog h2 { margin: 0 0 8px; font-size: 16px; font-weight: 600; }
.share-dialog::backdrop { background: rgba(8, 10, 14, .55); }
.share-dialog input { display: block; width: 100%; height: 30px; padding: 0 8px; margin: 8px 0; background: var(--field); border: 1px solid var(--line-2); border-radius: 6px; }
.share-dialog { max-height: 90vh; overflow: auto; }
.detail-actions small { display: block; }
.scope :is(button, select), .filter-panel button { min-height: 24px; }

@container teia (max-width: 1200px) {
  .page main { grid-template-columns: minmax(0, 1fr); }
  .page aside { position: absolute; right: 0; top: 0; bottom: 0; width: min(410px, 90%); z-index: 10; box-shadow: var(--shadow); }
}
@container teia (max-width: 960px) {
  .search input { width: 170px; }
}
/* Slice choices sit in the bar when it has room; narrow maps keep them in Filter. */
.slice-pick { display: contents; }
@container teia (max-width: 760px) {
  .slice-pick { display: none; }
}

@media (prefers-reduced-motion: reduce) {
  .toast, .feed .ev.new { animation: none; }
  .teia.panel aside { transition: none; }
}

/* ---- panel mode (inside Grafana): Grafana's own surface, compact bar, drawer -- */
/* The panel sits on Grafana's panel background (its default themes): cards and
   the knockouts behind labels take that surface, so nothing reads as a box in a box. */
.teia.panel { --bg: #181b1f; --card: #1e2128; --card-hi: #252932; --panel: #1e2128; --panel-2: #262a33; --field: #111217; }
:host([data-theme="light"]) .teia.panel { --bg: #ffffff; --card: #ffffff; --card-hi: #f4f6fa; --panel: #ffffff; --panel-2: #f0f2f5; --field: #ffffff; }
.teia.panel { grid-template-rows: auto auto 1fr; font-family: inherit; background: transparent; }
.teia.panel header { min-height: 42px; padding: 4px 8px; gap: 8px; background: transparent; border-bottom: 1px solid var(--line); }
.teia.panel .scope, .teia.panel #stage { background: transparent; }
.teia.panel .brand { display: none; }
.teia.panel .ring b { font-size: 13px; }
.teia.panel .search input { width: 180px; }
.teia.panel .btn { height: 28px; }
.teia.panel main { grid-template-columns: minmax(0, 1fr); position: relative; }
.teia.panel aside {
  position: absolute; right: 0; top: 0; bottom: 0; width: min(400px, 90%); z-index: 10;
  transform: translateX(102%); transition: transform .2s ease-out; box-shadow: var(--shadow);
}
.teia.panel aside.open { transform: none; }
.teia.panel .drawer-close { display: inline-flex; }
.drawer-close { display: inline-flex; margin-left: auto; align-self: center; border-color: transparent; }
.drawer-close:hover { background: var(--panel-2); }
.teia.panel .legend { font-size: 12px; padding: 8px 10px; }
.stepsBtn { display: inline-flex; }
.teia.panel .stepsBtn { display: inline-flex; }
.stepsBtn .count { color: var(--warn-text); font-weight: 600; font-size: 12px; }
@container teia (max-width: 640px) {
  .panel .rings { display: none; }
  .teia.panel .search input { width: 120px; }
  .ctl { gap: 4px; }
  .ctl .btn { padding: 0 8px; font-size: 12px; }
  .ctl select { width: 150px; }
}
@container teia (max-width: 480px) {
  .teia header { gap: 6px; padding: 6px 8px; min-height: 0; }
  .teia .brand, .teia .spacer { display: none; }
  .teia .rings { gap: 4px 12px; }
  .teia .ring b { font-size: 13px; }
  .teia .search { flex: 1; min-width: 100px; }
  .teia .search input { width: 100%; }
  .teia .btn { height: 28px; }
  .teia .live, .teia .time-context { font-size: 11px; }
  .teia .time-context { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .teia .scope { padding: 4px 8px; }
}
`;

  // Icons drawn for the controls, on the icon library's grid and stroke.
  const UI_ICON = {
    x: '<path d="M5 5L15 15M15 5L5 15"/>',
    chevron: '<path d="M5.5 8L10 12.5L14.5 8"/>',
    plus: '<path d="M10 4.25V15.75M4.25 10H15.75"/>',
    minus: '<path d="M4.25 10H15.75"/>',
    fit: '<path d="M3.25 7.25V3.25H7.25M12.75 3.25H16.75V7.25M16.75 12.75V16.75H12.75M7.25 16.75H3.25V12.75"/>',
    reset: '<path d="M4 10A6 6 0 1 0 5.75 5.75L3.75 7.75"/><path d="M3.75 3.75V7.75H7.75"/>',
    check: '<path d="M4.5 10.25L8.25 14L15.5 6.5"/>',
    search: '<circle cx="8.75" cy="8.75" r="5.5"/><path d="M12.75 12.75L16.75 16.75"/>',
    info: '<circle cx="10" cy="10" r="7.25"/><path d="M10 9V13.75"/><circle cx="10" cy="6.5" r="1" fill="currentColor" stroke="none"/>',
    filter: '<path d="M2.75 3.75H17.25L11.75 10.5V16.25L8.25 17.25V10.5Z"/>',
    live: '<circle cx="10" cy="10" r="2" fill="currentColor" stroke="none"/><path d="M5.75 5.75A6 6 0 0 0 5.75 14.25M14.25 5.75A6 6 0 0 1 14.25 14.25"/>',
    values: '<path d="M2.75 14.25L7.5 9.5L10.75 12.75L16.75 6.75M12.5 6.75H16.75V11"/>',
    arrow: '<path d="M3.25 10H16.25M12 5.75L16.25 10L12 14.25"/>',
    play: '<path d="M6 4V16L16 10Z"/>',
    pause: '<path d="M7 4.75V15.25M13 4.75V15.25"/>',
  };
  const ICON_ATTRS = 'viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="square" stroke-linejoin="miter"';
  const uiIcon = paths => `<svg ${ICON_ATTRS} aria-hidden="true">${paths}</svg>`;

  const MARKUP = `<header>
  <div class="brand">
    <svg viewBox="0 0 128 128" aria-hidden="true">
  <rect width="128" height="128" rx="26" fill="#020f59"/>
  <g fill="none" stroke-linecap="round" stroke-linejoin="round">
  <path d="M64 64 L64.0 11.0 M64 64 L109.9 37.5 M64 64 L109.9 90.5 M64 64 L64.0 117.0 M64 64 L18.1 90.5 M64 64 L18.1 37.5" stroke="#5b94ff" stroke-opacity=".5" stroke-width="2.2"/>
  <path d="M64.0 49.0 Q69.8 53.9 77.0 56.5 M77.0 56.5 Q75.7 64.0 77.0 71.5 M77.0 71.5 Q69.8 74.1 64.0 79.0 M64.0 79.0 Q58.2 74.1 51.0 71.5 M51.0 71.5 Q52.3 64.0 51.0 56.5 M51.0 56.5 Q58.1 53.9 64.0 49.0 M64.0 33.0 Q76.1 43.1 90.8 48.5 M90.8 48.5 Q88.2 64.0 90.8 79.5 M90.8 79.5 Q76.1 84.9 64.0 95.0 M64.0 95.0 Q51.9 84.9 37.2 79.5 M37.2 79.5 Q39.8 64.0 37.2 48.5 M37.2 48.5 Q51.9 43.1 64.0 33.0 M64.0 17.0 Q82.3 32.3 104.7 40.5 M104.7 40.5 Q100.7 64.0 104.7 87.5 M104.7 87.5 Q82.3 95.7 64.0 111.0 M64.0 111.0 Q45.7 95.7 23.3 87.5 M23.3 87.5 Q27.3 64.0 23.3 40.5 M23.3 40.5 Q45.7 32.3 64.0 17.0" stroke="#5b94ff" stroke-opacity=".75" stroke-width="2.2"/>
  <path d="M23.3 87.5 L37.2 79.5 Q51.9 84.9 64.0 95.0 L64.0 79.0 L64 64 L77.0 56.5 L90.8 48.5 L104.7 40.5" stroke="#a9f549" stroke-width="4.8"/>
  </g>
  <circle cx="64.0" cy="17.0" r="3.3" fill="#f7f8ff" fill-opacity=".8"/>
  <circle cx="64.0" cy="111.0" r="3.3" fill="#f7f8ff" fill-opacity=".8"/>
  <circle cx="23.3" cy="40.5" r="3.3" fill="#f7f8ff" fill-opacity=".8"/>
  <circle cx="37.2" cy="48.5" r="3.3" fill="#f7f8ff" fill-opacity=".8"/>
  <circle cx="90.8" cy="79.5" r="3.3" fill="#f7f8ff" fill-opacity=".8"/>
  <circle cx="64.0" cy="49.0" r="3.3" fill="#f7f8ff" fill-opacity=".8"/>
  <circle cx="51.0" cy="71.5" r="3.3" fill="#f7f8ff" fill-opacity=".8"/>
  <circle cx="23.3" cy="87.5" r="5.2" fill="#020f59" stroke="#a9f549" stroke-width="2.8"/>
  <circle cx="37.2" cy="79.5" r="5.2" fill="#020f59" stroke="#a9f549" stroke-width="2.8"/>
  <circle cx="64.0" cy="95.0" r="5.2" fill="#020f59" stroke="#a9f549" stroke-width="2.8"/>
  <circle cx="64.0" cy="79.0" r="5.2" fill="#020f59" stroke="#a9f549" stroke-width="2.8"/>
  <circle cx="77.0" cy="56.5" r="5.2" fill="#020f59" stroke="#a9f549" stroke-width="2.8"/>
  <circle cx="90.8" cy="48.5" r="5.2" fill="#020f59" stroke="#a9f549" stroke-width="2.8"/>
  <circle cx="104.7" cy="40.5" r="5.2" fill="#020f59" stroke="#a9f549" stroke-width="2.8"/>
  <circle cx="64" cy="64" r="8" fill="#a9f549" stroke="#020f59" stroke-width="2.5"/></svg>
    <b>teia</b>
  </div>
  <div class="cluster" id="clusterBox" role="group" aria-label="Cluster" hidden>
    <span class="cluster-label">Cluster</span>
    <b class="cluster-name" id="clusterName"></b>
    <select class="btn" id="clusterPick" aria-label="Cluster shown on the map" hidden></select>
  </div>
  <div class="rings" id="rings"></div>
  <div class="spacer"></div>
  <div class="live" id="live" title="Live updates from the discovery engine"><i></i><span>connecting…</span></div>
  <label class="search">
    ${uiIcon(UI_ICON.search)}
    <input id="search" type="search" placeholder="Search…  e.g. team:checkout" aria-label="Search the map" autocomplete="off" spellcheck="false">
    <button type="button" class="btn isolate" id="isolateBtn" title="Show only what the search names, and what it calls (Shift+Enter)" hidden>Isolate</button>
  </label>
  <div class="ctl">
    <select class="btn" id="groupBy" title="Group nodes by a label" aria-label="Group nodes by a label"></select>
    <button type="button" class="btn" id="valuesBtn" title="Traffic and latency on every line (V)"></button>
    <button type="button" class="btn" id="flowBtn" title="Animate traffic (space)"></button>
    <button type="button" class="btn" id="liveBtn" title="Follow live updates"></button>
    <button type="button" class="btn" id="historyBtn" title="Replay a stored discovery snapshot">History</button>
    <button type="button" class="btn" id="exploreBtn" aria-haspopup="dialog">Investigate</button>
    <button type="button" class="btn stepsBtn" id="stepsBtn" title="Next steps">Next steps <span class="count" id="stepsBadge">0</span></button>
    <div class="menu" id="exportMenu">
      <button type="button" class="btn" id="exportBtn" title="Export" aria-haspopup="true" aria-expanded="false">Export ${uiIcon(UI_ICON.chevron)}</button>
      <div class="menu-list">
        <button type="button" id="shareBtn">Share this view</button>
        <button type="button" data-export="png">Map as PNG</button>
        <button type="button" data-export="svg">Map as SVG</button>
        <a data-api="export/matrix.csv" download>Flow matrix (CSV)</a>
        <a data-api="export/matrix.csv?lang=pt-BR" download>Matriz de fluxos (CSV, pt-BR)</a>
        <a data-api="snapshot" target="_blank" rel="noopener">Snapshot (JSON)</a>
      </div>
    </div>
  </div>
  <div class="time-context" id="timeContext"></div>
</header>
<nav class="scope" id="scope" aria-label="Filters"></nav>
<div class="filter-panel" id="filterPanel" role="dialog" aria-label="Filter the map" hidden></div>
<dialog class="share-dialog" id="shareDialog" aria-label="Share this view"><h2>Share this view</h2><p id="shareContext"></p><label>View link<input id="shareURL" readonly></label><p id="shareStatus" role="status"></p><div class="detail-actions"><button type="button" class="btn" id="copyViewBtn">Copy link</button><button type="button" class="btn" id="closeShareBtn">Close sharing</button></div></dialog>
<dialog class="share-dialog workspace-dialog" id="investigateDialog" aria-label="Investigation workspace">
  <h2>Investigation workspace</h2><p>Focus the topology, compare observations or connect your data.</p>
  <nav class="detail-actions" aria-label="Investigation tools"><button class="btn" type="button" data-workspace="scope">Focus and list</button><button class="btn" type="button" data-workspace="compare">Compare snapshots</button><button class="btn" type="button" data-workspace="setup">Connect data</button><button class="btn" type="button" data-workspace="operations">Operations and traces</button></nav>
  <section id="workspaceContent"></section><div class="detail-actions workspace-footer"><button class="btn" type="button" id="closeInvestigateBtn">Close investigation</button></div>
</dialog>
<dialog class="share-dialog" id="historyDialog" aria-label="Observation history"><h2>Observation history</h2><p>Replay stored discovery snapshots. Gaps between snapshots are unmeasured; unavailable history does not mean no traffic.</p><form id="historyForm"><label>From (UTC)<input id="historyFrom" type="datetime-local" required></label><label>To (UTC)<input id="historyTo" type="datetime-local" required></label><button class="btn" type="submit">Find snapshots</button></form><p id="historyStatus" role="status"></p><div id="historyList"></div><div class="detail-actions"><button type="button" class="btn" id="currentBtn">Return to current state</button><button type="button" class="btn" id="closeHistoryBtn">Close history</button></div></dialog>
<main>
  <div id="stage">
    <div id="investigationSummary" class="investigation-summary" hidden></div>
    <div id="entityTable" class="entity-table" hidden></div>
    <svg id="map" role="group" aria-label="Communication map. Drag to pan, F to fit, slash to search."></svg>
    <div class="map-help" id="mapHelp" hidden><span>Overview · select a component for its evidence.</span><button type="button" id="readableBtn">Read names</button></div>
    <div class="empty-state" id="empty"><p>Waiting for the first discovery round…</p></div>
    <div class="floating legend" id="legend"></div>
    <div class="floating zoom">
      <button type="button" class="btn" id="zoomIn" title="Zoom in" aria-label="Zoom in">${uiIcon(UI_ICON.plus)}</button>
      <button type="button" class="btn" id="zoomOut" title="Zoom out" aria-label="Zoom out">${uiIcon(UI_ICON.minus)}</button>
      <button type="button" class="btn" id="fitBtn" title="Fit (F)" aria-label="Fit the map">${uiIcon(UI_ICON.fit)}</button>
      <button type="button" class="btn" id="resetBtn" title="Reset layout" aria-label="Reset layout">${uiIcon(UI_ICON.reset)}</button>
    </div>
    <div class="toasts" id="toasts" role="status" aria-live="polite"></div>
    <div class="sr-only" id="announce" role="status" aria-live="polite"></div>
  </div>
  <aside>
    <nav class="tabs" role="tablist">
      <button type="button" role="tab" class="tab on" data-tab="steps" aria-selected="true">Next steps <span class="count" id="stepsCount">0</span></button>
      <button type="button" role="tab" class="tab" data-tab="details" aria-selected="false">Details</button>
      <button type="button" role="tab" class="tab" data-tab="activity" aria-selected="false">Live <span class="count" id="activityCount">0</span></button>
      <button type="button" role="tab" class="tab" data-tab="sources" aria-selected="false">Sources</button>
      <button type="button" class="btn icon drawer-close" id="drawerClose" title="Close" aria-label="Close">${uiIcon(UI_ICON.x)}</button>
    </nav>
    <section class="pane on" role="tabpanel" id="pane-steps"></section>
    <section class="pane" role="tabpanel" id="pane-details"><p class="muted">Click a service, a datastore or a communication on the map.</p></section>
    <section class="pane feed" role="tabpanel" id="pane-activity"><p class="muted">Changes between discovery rounds show up here as they happen.</p></section>
    <section class="pane" role="tabpanel" id="pane-sources"></section>
  </aside>
</main>`;

  /*! IBM Plex Sans variable font, latin subset. Copyright 2019 IBM Corp.
   *  (https://github.com/IBM/plex). SIL Open Font License 1.1. */
  const PLEX = 'data:font/woff2;base64,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';
  let fontLoaded = false;
  function loadFont() {
    if (fontLoaded || typeof FontFace === 'undefined' || !document.fonts) return;
    fontLoaded = true;
    const face = new FontFace('teia-plex', `url(${PLEX})`, { weight: '100 700', display: 'swap' });
    face.load().then(f => document.fonts.add(f)).catch(() => { /* system font fallback */ });
  }

  // ============================================================== core
  // Pure, DOM-free logic: the data model the map draws. It is unit-tested
  // under node (internal/api/ui/teia-ui.test.mjs) through TeiaUI.core.

  const has = (obj, key) => obj != null && Object.prototype.hasOwnProperty.call(obj, key);

  // The teia icon library, drawn for the map rather than taken from a kit: a
  // 20px grid with 1px of padding, a 1.5 stroke with square ends and mitred
  // corners, and a filled dot for a unit of work (a request, a message, a
  // record), like the dots that travel the map's lines. Datastores share the
  // cylinder; what is unusual about one is drawn inside it.
  const DOT = (x, y, r = 1.5) => `<circle cx="${x}" cy="${y}" r="${r}" fill="currentColor" stroke="none"/>`;
  const HEX = '<path d="M10 1.75L17.15 5.88V14.12L10 18.25L2.85 14.12V5.88Z"/>';
  const CYL = '<ellipse cx="10" cy="4.75" rx="7.25" ry="2.5"/><path d="M2.75 4.75V15.25C2.75 16.63 6 17.75 10 17.75S17.25 16.63 17.25 15.25V4.75"/>';
  const ICON = {
    service: HEX + DOT(10, 10, 2),
    database: CYL + '<path d="M2.75 10C2.75 11.38 6 12.5 10 12.5S17.25 11.38 17.25 10"/>',
    external: '<path d="M5.5 15.75H14.75A3.5 3.5 0 0 0 15.25 8.8A5.25 5.25 0 0 0 5.15 7.6A4.1 4.1 0 0 0 5.5 15.75Z"/>',
    unknown: HEX.replace('/>', ' stroke-dasharray="2.5 2"/>') + '<path d="M8.25 8.25C8.25 6 11.75 6 11.75 8.25C11.75 9.75 10 10 10 11.5"/>' + DOT(10, 14, 1),
    group: '<path d="M10 3.7L13.12 5.5V9.1L10 10.9L6.88 9.1V5.5ZM6.88 9.1L10 10.9V14.5L6.88 16.3L3.76 14.5V10.9ZM13.12 9.1L16.24 10.9V14.5L13.12 16.3L10 14.5V10.9Z"/>',
  };
  const CATEGORY = {
    service: { label: 'Microservice', icon: ICON.service },
    frontend: { label: 'Frontend', icon: '<rect x="2.75" y="3.75" width="14.5" height="12.5" rx="1"/><path d="M2.75 7.25H17.25"/>' + DOT(10, 11.75) },
    gateway: { label: 'Gateway / proxy', icon: DOT(3.25, 10) + '<path d="M4.75 10H8.5L17.25 3.75M8.5 10H17.25M8.5 10L17.25 16.25"/>' },
    job: { label: 'Job / worker', icon: '<circle cx="10" cy="11" r="6.75"/><path d="M10 11V7.75M7.75 1.75H12.25M10 1.75V4.25M15.25 4.5L16.5 5.75"/>' },
    function: { label: 'Function', icon: '<path d="M5.75 2.75C3.75 5 3.75 15 5.75 17.25M14.25 2.75C16.25 5 16.25 15 14.25 17.25"/><path d="M11 4.5L7.75 10.5H12.25L9 15.5"/>' },
    relational: { label: 'Relational database', icon: ICON.database },
    document: { label: 'Document database (NoSQL)', icon: '<path d="M4.25 2.75H11.75L15.75 6.75V17.25H4.25Z"/><path d="M8.75 9C7.75 9 7.75 9.5 7.75 10.5S7.25 11.75 6.75 11.75C7.25 11.75 7.75 12 7.75 13S7.75 14.5 8.75 14.5M11.25 9C12.25 9 12.25 9.5 12.25 10.5S12.75 11.75 13.25 11.75C12.75 11.75 12.25 12 12.25 13S12.25 14.5 11.25 14.5" stroke-width="1.25"/>' },
    widecolumn: { label: 'Wide-column database', icon: [2.75, 8, 13.25].map(x => `<ellipse cx="${x + 2}" cy="4.5" rx="2" ry="1"/><path d="M${x} 4.5V15.5C${x} 16.05 ${x + 0.9} 16.5 ${x + 2} 16.5S${x + 4} 16.05 ${x + 4} 15.5V4.5"/>`).join('') },
    search: { label: 'Search engine', icon: '<ellipse cx="8.5" cy="4.5" rx="5.75" ry="2"/><path d="M2.75 4.5V13.5C2.75 14.6 5.3 15.5 8.5 15.5M14.25 4.5V8"/><circle cx="14" cy="13" r="3"/><path d="M16.25 15.25L18 17"/>' },
    timeseries: { label: 'Time-series / analytics', icon: '<path d="M2.75 17.25H17.25M4 13L8 9L12 11.25L16 5.5"/>' + DOT(4, 13) + DOT(8, 9) + DOT(12, 11.25) + DOT(16, 5.5) },
    graphdb: { label: 'Graph database', icon: '<circle cx="10" cy="4" r="2.25"/><circle cx="4" cy="15.5" r="2.25"/><circle cx="16" cy="15.5" r="2.25"/><path d="M9 6L5 13.5M11 6L15 13.5M6.25 15.5H13.75"/>' },
    cache: { label: 'Cache / key-value', icon: '<rect x="2.75" y="4.75" width="14.5" height="8.5" rx="1"/><path d="M6 13.25V16.25M10 13.25V16.25M14 13.25V16.25"/>' + DOT(6.5, 9, 1.25) + DOT(10, 9, 1.25) + DOT(13.5, 9, 1.25) },
    stream: { label: 'Event stream', icon: '<path d="M2.75 10H16.25M13.75 7L17 10L13.75 13"/>' + DOT(4.5, 10) + DOT(8.25, 10) + DOT(12, 10) },
    queue: { label: 'Message queue', icon: '<path d="M13.25 5.75H2.75V14.25H13.25"/>' + DOT(5.5, 10) + DOT(9, 10) + DOT(15.75, 10) },
    storage: { label: 'Object storage', icon: '<path d="M3 7.25H17L15.25 17.25H4.75Z"/><path d="M4.25 7.25C4.25 3.5 15.75 3.5 15.75 7.25"/>' },
    identity: { label: 'Identity', icon: '<circle cx="6.25" cy="13.75" r="3.5"/><path d="M8.75 11.25L17 3M14 6L16.25 8.25M11.75 8.25L13.5 10"/>' },
    external: { label: 'External / SaaS', icon: ICON.external },
    unknown: { label: 'Unknown', icon: ICON.unknown },
  };
  const KIND_CATEGORY = { service: 'service', database: 'relational', cache: 'cache', queue: 'queue', external: 'external', unknown: 'unknown' };
  // Technologies teia recognises (internal/graph/tech.go): category and name.
  const TECH = {};
  const techs = (cat, list) => list.split(',').forEach(pair => { const [id, label] = pair.split('='); TECH[id] = { cat, label }; });
  techs('relational', 'postgresql=PostgreSQL,mysql=MySQL,mariadb=MariaDB,mssql=SQL Server,oracle=Oracle,db2=Db2,sqlite=SQLite');
  techs('document', 'mongodb=MongoDB,couchdb=CouchDB,dynamodb=DynamoDB,cosmosdb=Cosmos DB,firestore=Firestore');
  techs('widecolumn', 'cassandra=Cassandra,scylladb=ScyllaDB,hbase=HBase,bigtable=Bigtable');
  techs('search', 'elasticsearch=Elasticsearch,opensearch=OpenSearch,solr=Solr,meilisearch=Meilisearch');
  techs('timeseries', 'clickhouse=ClickHouse,influxdb=InfluxDB,druid=Druid');
  techs('graphdb', 'neo4j=Neo4j,neptune=Neptune');
  techs('cache', 'redis=Redis,valkey=Valkey,memcached=Memcached,keydb=KeyDB,dragonfly=Dragonfly,hazelcast=Hazelcast');
  techs('stream', 'kafka=Kafka,redpanda=Redpanda,kinesis=Kinesis,pulsar=Pulsar,eventhubs=Event Hubs');
  techs('queue', 'rabbitmq=RabbitMQ,sqs=SQS,sns=SNS,activemq=ActiveMQ,nats=NATS,pubsub=Pub/Sub,servicebus=Service Bus');
  techs('storage', 's3=S3,minio=MinIO,gcs=Cloud Storage,azureblob=Blob Storage');
  techs('gateway', 'nginx=NGINX,envoy=Envoy,haproxy=HAProxy,traefik=Traefik,kong=Kong,apisix=APISIX');
  techs('identity', 'keycloak=Keycloak,auth0=Auth0,okta=Okta,dex=Dex');
  techs('frontend', 'frontend=Frontend');
  techs('job', 'job=Job');
  techs('function', 'function=Function');
  techs('service', 'service=Microservice');
  techs('external', 'external=External');

  const SIGNAL = {
    traces: { label: 'Traces', icon: '<rect x="2.75" y="3.75" width="8.5" height="2.5"/><rect x="6.25" y="8.75" width="8" height="2.5"/><rect x="9.75" y="13.75" width="7.5" height="2.5"/>' },
    mesh: { label: 'Mesh', icon: '<path d="M8 2.75L13.25 5.75V12L8 15L2.75 12V5.75Z"/><rect x="13.25" y="10.75" width="4" height="6.5" rx="1"/>' },
    network: { label: 'Network', icon: '<path d="M2.75 6.5H16M13 3.5L16.25 6.5L13 9.5M17.25 13.5H4M7 10.5L3.75 13.5L7 16.5"/>' },
    logs: { label: 'Logs', icon: DOT(3.5, 5, 1.25) + DOT(3.5, 10, 1.25) + DOT(3.5, 15, 1.25) + '<path d="M6.75 5H17.25M6.75 10H17.25M6.75 15H13.25"/>' },
    database: { label: 'Database', icon: ICON.database },
    metrics: { label: 'Metrics', icon: '<path d="M2.75 17.25H17.25M5.25 14.25V9.75M10 14.25V3.75M14.75 14.25V7.75"/>' },
    inventory: { label: 'Inventory', icon: '<rect x="2.75" y="2.75" width="5.5" height="5.5"/><rect x="11.75" y="2.75" width="5.5" height="5.5"/><rect x="2.75" y="11.75" width="5.5" height="5.5"/><rect x="11.75" y="11.75" width="5.5" height="5.5"/>' },
    // A flows file: dashed, like everything only declared.
    declared: { label: 'Declared', icon: '<rect x="2.75" y="3.75" width="14.5" height="12.5" rx="1" stroke-dasharray="2.5 2"/><path d="M6.75 10.25L9 12.5L13.5 8"/>' },
  };
  // Why a card or line is amber or red: one pictogram per golden signal.
  const REASON = {
    errors: { label: 'Errors', icon: '<circle cx="10" cy="10" r="7.25"/><path d="M7.5 7.5L12.5 12.5M12.5 7.5L7.5 12.5"/>' },
    latency: { label: 'Latency', icon: '<path d="M2.75 4.75V15.25M17.25 4.75V15.25M5.75 10H14.25M8 7.75L5.75 10L8 12.25M12 7.75L14.25 10L12 12.25"/>' },
    saturation: { label: 'Saturation', icon: '<path d="M2.75 15.25A7.25 7.25 0 0 1 17.25 15.25"/><path d="M10 15.25L13.5 10.25M2.75 17.25H17.25"/>' },
    backlog: { label: 'Queue backlog', icon: '<path d="M2.75 5.75H17.25M2.75 10H17.25M2.75 14.25H11.25"/>' + DOT(15, 14.25, 1.25) },
    lag: { label: 'Delay / age', icon: '<path d="M4.75 2.75H15.25M4.75 17.25H15.25M6 2.75H14L10 10L14 17.25H6L10 10Z"/>' },
    locks: { label: 'Locks', icon: '<rect x="3.75" y="8.75" width="12.5" height="8.5" rx="1"/><path d="M6.75 8.75V6.25A3.25 3.25 0 0 1 13.25 6.25V8.75M10 12V14"/>' },
    restarts: { label: 'Restarts', icon: '<path d="M16.25 8.25A6.5 6.5 0 0 0 4.5 6.5M3.75 11.75A6.5 6.5 0 0 0 15.5 13.5M4.25 3V6.75H8M15.75 17V13.25H12"/>' },
    evictions: { label: 'Cache evictions', icon: '<rect x="2.75" y="5.75" width="9.5" height="8.5" rx="1"/><path d="M8 10H17.25M14.5 7.25L17.25 10L14.5 12.75"/>' },
    rejections: { label: 'Rejected clients', icon: '<circle cx="10" cy="10" r="7.25"/><path d="M6.25 10H13.75"/>' },
  };
  // healthText says a node's or line's health in words.
  const healthText = x => x.health === 'unknown' && x.errorsUnknown && x.requestRate > 0 ? 'failures not measured' : HEALTH_LABEL[x.health];
  const HEALTH_LABEL = { healthy: 'healthy', degraded: 'degraded', failing: 'failing', unknown: 'health unknown' };
  const HEALTH_RANK = { failing: 3, degraded: 2, healthy: 1, unknown: 0 };
  // Comparison marks are text as well as colour, like health.
  const DIFF_MARK = { added: '+ ', removed: '− ', changed: '~ ' };
  const DIFF_TEXT = { added: 'only in the after capture', removed: 'only in the before capture', changed: 'changed between the captures' };
  const STATE_LABEL = { ok: 'declared and seen', ghost: 'seen, not declared', stale: 'declared, not seen' };
  const VIS_LABEL = { full: 'seen first-hand', partial: 'blind spot', none: 'no traffic seen' };
  const VIS_EXPLAIN = {
    full: 'teia sees it first-hand: its own SDK, sidecar, eBPF probe or database explorer reports it.',
    partial: 'Blind spot: teia sees it only through what its callers report, so whatever it calls stays invisible. To see it first-hand, give it an SDK, a sidecar or an eBPF probe.',
    none: 'teia knows it exists (inventory or a flows file) but has seen no traffic to or from it.',
  };

  const categoryOf = n => n.group ? 'group' : (has(TECH, n.tech) ? TECH[n.tech].cat : has(KIND_CATEGORY, n.kind) ? KIND_CATEGORY[n.kind] : 'unknown');
  const iconOf = n => n.group ? ICON.group : (has(CATEGORY, categoryOf(n)) ? CATEGORY[categoryOf(n)].icon : ICON.unknown);
  const techLabel = n => has(TECH, n.tech) ? TECH[n.tech].label : n.kind;
  const userLabel = k => !k.startsWith('teia.') && k !== 'docker.container' && k !== 'backstage.ref' && k !== 'image';

  const oneOf = (v, allowed, fallback) => allowed.includes(v) ? v : fallback;
  const finite = v => (typeof v === 'number' && Number.isFinite(v)) ? v : 0;
  const strings = v => Array.isArray(v) ? v.filter(s => typeof s === 'string') : [];
  const text = v => typeof v === 'string' ? v : '';
  const timestamp = v => typeof v === 'string' && !v.startsWith('0001') && Number.isFinite(Date.parse(v)) ? new Date(v).toISOString() : '';
  const IDENTITY_METHODS = ['explicit', 'name', 'alias', 'ip-temporal', 'inferred', 'ambiguous', 'unresolved'];
  const IDENTITY_TIME = ['not-applicable', 'unknown', 'evaluation-window', 'collection-window', 'sample-envelope', 'sample-point'];
  // Identity metadata explains source decisions; it never assigns a numeric
  // confidence or substitutes collection time for original observation time.
  const identity = value => (Array.isArray(value) ? value : [])
    .filter(r => r && !Array.isArray(r) && text(r.input) && text(r.resolvedId) && text(r.source) && IDENTITY_METHODS.includes(r.method))
    .map(r => ({
      input: r.input, resolvedId: r.resolvedId, method: r.method, source: r.source, sourceType: text(r.sourceType),
      basis: text(r.basis), detail: text(r.detail), timeBasis: oneOf(r.timeBasis, IDENTITY_TIME, 'unknown'),
      collectedAt: timestamp(r.collectedAt), evaluatedAt: timestamp(r.evaluatedAt), observedFrom: timestamp(r.observedFrom), observedAt: timestamp(r.observedAt),
      windowSeconds: Math.max(0, finite(r.windowSeconds)), retained: r.retained === true, alternatives: strings(r.alternatives),
      candidates: (Array.isArray(r.candidates) ? r.candidates : []).filter(c => c && !Array.isArray(c) && text(c.id) && text(c.source)).map(c => ({
        id: c.id, source: c.source, sourceType: text(c.sourceType), basis: text(c.basis),
        since: timestamp(c.since), collectedAt: timestamp(c.collectedAt), retained: c.retained === true,
      })),
    }));
  const connectionIdentity = value => value && typeof value === 'object' && !Array.isArray(value)
    ? { from: identity(value.from), to: identity(value.to) } : undefined;
  const evidence = v => (Array.isArray(v) ? v : []).filter(e => e && typeof e.source === 'string').map(e => {
    const sampleCount = Number.isSafeInteger(e.sampleCount) && e.sampleCount > 0 ? e.sampleCount : 0;
    let observedAt = timestamp(e.observedAt), observedFrom = timestamp(e.observedFrom);
    if (sampleCount && (!observedFrom || !observedAt || Date.parse(observedFrom) > Date.parse(observedAt))) observedAt = observedFrom = '';
    return {
      source: e.source, signal: text(e.signal), origin: text(e.origin), collectedAt: timestamp(e.collectedAt), observedAt, observedFrom, sampleCount,
      evaluatedAt: timestamp(e.evaluatedAt), oldestSampleAt: timestamp(e.oldestSampleAt),
      windowSeconds: Math.max(0, finite(e.windowSeconds)), retained: e.retained === true, metrics: strings(e.metrics),
    };
  });
  const ageText = (at, reference) => {
    const seconds = (reference - Date.parse(at)) / 1000;
    return seconds < 0 ? `${fmtSec(-seconds)} in the future; check source clock` : `${fmtSec(seconds)} ago`;
  };
  function evidenceText(e) {
    const subject = e.subject ? `${e.subject.node || `${e.subject.from} → ${e.subject.to}`} · ` : '';
    const age = at => at && e.collectedAt ? ` (age at collection: ${ageText(at, Date.parse(e.collectedAt))})` : '';
    const observation = e.sampleCount
      ? `Sampled calls: ${e.sampleCount}. ` + (e.observedFrom && e.observedAt
        ? `Earliest start in sample: ${e.observedFrom}. Latest end in sample: ${e.observedAt}${age(e.observedAt)}. `
        : 'Sample time range unknown: missing or invalid span times. ') +
        'Sample bounds do not imply continuous coverage or the service\'s last call. '
      : e.oldestSampleAt ? `Oldest input sample: ${e.oldestSampleAt}${age(e.oldestSampleAt)}. `
        : `Original observation: ${e.observedAt || 'unknown'}${age(e.observedAt)}. `;
    return `${subject}${e.source}${e.origin ? ` (${e.origin})` : ''} · ${e.signal || 'source'} · ${e.metrics.join(', ') || 'inventory'}${e.retained ? ' · retained topology' : ''}. ` +
      `Collected: ${e.collectedAt || 'unknown'}. ` + (e.evaluatedAt ? `Query evaluated: ${e.evaluatedAt}. ` : '') + observation +
      (e.windowSeconds > 0 ? `Query lookback: ${fmtSec(e.windowSeconds)}.` : 'Query lookback not supplied.');
  }
  function temporalText(snap, opts = {}, now = Date.now()) {
    if (!snap) return 'Waiting for time information';
    if (snap.operationContext) return `Sampled operation · ${snap.operationContext.operation} · ${snap.operationContext.traceId || 'all returned traces'} · total traffic and sampling coverage unknown · global health excluded`;
    if (opts.comparison) return `Frozen capture · ${snap.generatedAt || 'time unknown'} · ${snap.history?.id || snap.timeMode || 'query'} · discrete observation, not continuous coverage`;
    const mode = snap.timeMode || (opts.api ? 'current' : 'query');
    const age = at => ageText(at, now);
    const collected = timestamp(snap.generatedAt);
    const observed = timestamp(snap.observedAt);
    const windows = [...new Set([...snap.nodes, ...snap.edges].flatMap(x => (x.evidence || []).map(e => e.windowSeconds)).filter(w => w > 0))].sort((a, b) => a - b);
    const parts = [mode === 'current' ? 'Current state' : mode === 'mixed' ? 'Mixed time modes' : 'Query data'];
    if (mode === 'historical') return `Historical snapshot · ${collected || 'time unknown'} · recorded ${snap.history?.recordedAt || 'unknown'} · discrete observation, not continuous interval coverage · live updates suspended`;
    if (mode !== 'current') parts.push(snap.period ? `${snap.period.from} – ${snap.period.to}` : 'effective period unknown');
    parts.push(collected ? `snapshot updated ${age(collected)}` : 'collection time unknown');
    if (windows.length) parts.push(`source lookbacks ${windows.map(fmtSec).join(', ')}`);
    parts.push(observed ? `original observation ${age(observed)}` : 'original observation time unknown');
    if (mode === 'current' && (opts.mode === 'panel' || opts.requestedRange)) parts.push('dashboard range does not change this snapshot');
    return parts.join(' · ');
  }
  // measures are readings beyond traffic: connections in use, sessions
  // waiting on locks, consumer lag.
  const measures = v => (Array.isArray(v) ? v : [])
    .filter(m => m && has(REASON, m.cause) && typeof m.what === 'string' && m.what)
    .map(m => ({ cause: m.cause, what: m.what, value: finite(m.value), max: finite(m.max), unit: oneOf(m.unit, ['bytes', 'seconds', 'share'], ''), via: text(m.via) }));
  // reasons keeps the ones the map knows how to draw, worst first.
  const reasons = v => (Array.isArray(v) ? v : [])
    .filter(r => r && has(REASON, r.cause) && (r.health === 'degraded' || r.health === 'failing'))
    .map(r => ({ cause: r.cause, health: r.health, value: finite(r.value), limit: finite(r.limit), summary: text(r.summary), what: text(r.what), via: text(r.via), coverage: finite(r.coverage), approximate: r.approximate === true, rule: text(r.rule), unit: text(r.unit), evidence: evidence(r.evidence) }))
    .sort((a, b) => HEALTH_RANK[b.health] - HEALTH_RANK[a.health]);

  // Keep a measured cause at its origin; details retain every propagated reason.
  function mapReasons(item, nodes) {
    return (item.reasons || []).filter(r => {
      const origin = r.via || item.to;
      if (!origin || origin === item.id) return true;
      return !(nodes.get(origin)?.reasons || []).some(other => !other.via &&
        other.cause === r.cause && other.what === r.what && other.value === r.value && other.health === r.health);
    });
  }

  const healthRules = v => (Array.isArray(v) ? v : []).filter(r => r && has(REASON, r.cause)).map(r => ({
    cause: r.cause, name: text(r.name), match: oneOf(r.match, ['global', 'identity', 'selector'], 'global'),
    priority: finite(r.priority), degraded: finite(r.degraded), failing: finite(r.failing), unit: text(r.unit),
  }));
  function safeLinks(v) {
    return (Array.isArray(v) ? v : []).flatMap(l => {
      if (!l || !text(l.label)) return [];
      try {
        const url = new URL(l.url);
        if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) return [];
        return [{ label: text(l.label), url: url.href, kind: text(l.kind), description: text(l.description), periodFrom: timestamp(l.periodFrom), periodTo: timestamp(l.periodTo) }];
      } catch { return []; }
    });
  }

  function journeyContractOf(value, nodes, edges) {
    if (!value || value.version !== 1 || value.mode !== 'ownership' || typeof value.key !== 'string' || !Array.isArray(value.scopes) ||
      !Number.isInteger(value.maxDepth) || value.maxDepth < 0 || !Number.isInteger(value.maxNodes) || value.maxNodes < 1) return undefined;
    const ids = new Set(nodes.map(n => n.id)), edgeIDs = new Set(edges.map(e => e.from + '->' + e.to));
    if (!value.scopes.every(s => s && typeof s.name === 'string' && s.key === value.key && s.roles && typeof s.roles === 'object' &&
      Object.entries(s.roles).every(([id, role]) => ids.has(id) && ['member', 'dependency'].includes(role)) &&
      Array.isArray(s.entryPoints) && s.entryPoints.every(id => ids.has(id)) && Array.isArray(s.entryEdges) && s.entryEdges.every(id => edgeIDs.has(id)) &&
      Array.isArray(s.boundaryPorts) && s.boundaryPorts.every(b => b && ['caller', 'tenant', 'exit'].includes(b.kind) && edgeIDs.has(b.edgeId) && b.edgeId === b.from + '->' + b.to) &&
      typeof s.truncated === 'boolean')) return undefined;
    return value;
  }

  // sanitize turns whatever arrived (teia's API, a Grafana data frame, a
  // proxy's error page) into a snapshot the map can always draw.
  function sanitize(raw) {
    const s = raw && typeof raw === 'object' ? raw : {};
    const nodes = [], seen = new Set();
    for (const n of Array.isArray(s.nodes) ? s.nodes : []) {
      if (!n || typeof n.id !== 'string' || !n.id || seen.has(n.id)) continue;
      seen.add(n.id);
      nodes.push({
        ...n,
        identity: identity(n.identity),
        journeys: strings(n.journeys),
        kind: typeof n.kind === 'string' && n.kind ? n.kind : 'unknown',
        health: oneOf(n.health, ['healthy', 'degraded', 'failing', 'unknown'], 'unknown'),
        visibility: oneOf(n.visibility, ['full', 'partial', 'none'], 'partial'),
        requestRate: finite(n.requestRate), errorRate: finite(n.errorRate), errorsUnknown: n.errorsUnknown === true,
        latencyP50Ms: finite(n.latencyP50Ms), latencyP95Ms: finite(n.latencyP95Ms),
        reasons: reasons(n.reasons), measures: measures(n.measures), evidence: evidence(n.evidence), healthRules: healthRules(n.healthRules), links: safeLinks(n.links),
        sources: strings(n.sources), addresses: strings(n.addresses),
        labels: n.labels && typeof n.labels === 'object' ? n.labels : {},
      });
    }
    const edges = [];
    for (const e of Array.isArray(s.edges) ? s.edges : []) {
      if (!e || typeof e.from !== 'string' || typeof e.to !== 'string' || !e.from || !e.to || e.from === e.to) continue;
      for (const id of [e.from, e.to]) {
        if (!seen.has(id)) { seen.add(id); nodes.push({ id, kind: 'unknown', health: 'unknown', visibility: 'partial', requestRate: 0, errorRate: 0, latencyP50Ms: 0, latencyP95Ms: 0, reasons: [], measures: [], sources: [], addresses: [], labels: {} }); }
      }
      edges.push({
        ...e,
        identity: connectionIdentity(e.identity),
        state: oneOf(String(e.state || '').toLowerCase(), ['ok', 'ghost', 'stale'], e.declared === false ? 'ghost' : 'ok'),
        health: oneOf(e.health, ['healthy', 'degraded', 'failing', 'unknown'], 'unknown'),
        requestRate: finite(e.requestRate), errorRate: finite(e.errorRate), errorsUnknown: e.errorsUnknown === true,
        latencyP50Ms: finite(e.latencyP50Ms), latencyP95Ms: finite(e.latencyP95Ms),
        reasons: reasons(e.reasons), measures: measures(e.measures), evidence: evidence(e.evidence), healthRules: healthRules(e.healthRules), links: safeLinks(e.links),
        declared: !!e.declared, observed: e.observed !== false,
        signals: strings(e.signals), sources: strings(e.sources),
      });
    }
    const cov = s.coverage && typeof s.coverage === 'object' ? s.coverage : {};
    return {
      round: Number.isFinite(s.round) ? s.round : 0,
      generatedAt: timestamp(s.generatedAt), observedAt: timestamp(s.observedAt),
      timeMode: oneOf(s.timeMode, ['current', 'query', 'mixed', 'historical'], ''),
      history: s.history?.availability === 'stored-snapshot' && typeof s.history.id === 'string' ? s.history : undefined,
      period: timestamp(s.period?.from) && timestamp(s.period?.to) && s.period.from <= s.period.to ? { from: timestamp(s.period.from), to: timestamp(s.period.to) } : undefined,
      nodes, edges, journeys: journeyContractOf(s.journeys, nodes, edges), operations: operationsOf(s.operations),
      findings: (Array.isArray(s.findings) ? s.findings : []).filter(f => f && typeof f.title === 'string').map(f => ({
        ...f, severity: oneOf(f.severity, ['critical', 'warning', 'info'], 'info'), subject: String(f.subject || ''),
      })).sort((a, b) => ['critical', 'warning', 'info'].indexOf(a.severity) - ['critical', 'warning', 'info'].indexOf(b.severity) || a.subject.localeCompare(b.subject)),
      sources: (Array.isArray(s.sources) ? s.sources : []).filter(x => x && typeof x.name === 'string'),
      coverage: { visibility: finite(cov.visibility), declared: finite(cov.declared), observed: finite(cov.observed) },
      // Data without intent (plain Node Graph frames) has no drift to show.
      drift: s.drift !== false,
      changes: Array.isArray(s.changes) ? s.changes : [],
      dimensions: dimensionsOf(s.dimensions),
      slice: sliceOf(s.slice),
      cluster: typeof s.cluster === 'string' ? s.cluster.slice(0, 128) : '',
    };
  }

  // dimensionsOf keeps {dimension: [values]} with string values only.
  function dimensionsOf(raw) {
    const out = {};
    if (!raw || typeof raw !== 'object') return out;
    for (const [k, v] of Object.entries(raw)) { const vs = strings(v); if (k && vs.length) out[k] = vs; }
    return out;
  }
  // sliceOf describes a snapshot narrowed to calls carrying some dimension values.
  function sliceOf(raw) {
    if (!raw || typeof raw !== 'object' || !raw.selector || typeof raw.selector !== 'object') return undefined;
    const selector = Object.fromEntries(Object.entries(raw.selector).filter(([k, v]) => k && typeof v === 'string'));
    return { selector, matched: raw.matched === true, metrics: text(raw.metrics), sources: strings(raw.sources), excluded: strings(raw.excluded) };
  }
  // sliceText writes a selector the way the API reads it: journey:x,tenant:y.
  const sliceText = selector => Object.keys(selector || {}).sort().map(k => k + ':' + selector[k]).join(',');
  // sliceLabel reads that form to people: journey = x · tenant = y.
  const sliceLabel = text => String(text || '').split(',').map(p => p.replace(':', ' = ')).join(' · ');

  // signature identifies what is drawn, so refreshes with the same data are no-ops.
  function signature(snap) {
    return JSON.stringify([snap.nodes, snap.edges, snap.findings, snap.sources, snap.coverage, snap.journeys, snap.operations, snap.operationContext, snap.timeMode, snap.history, snap.drift, snap.slice, snap.dimensions, snap.cluster]);
  }

  function indexSnapshot(snap) {
    const nodes = new Map(snap.nodes.map(n => [n.id, n]));
    const sourceType = new Map(snap.sources.map(s => [s.name, s.type]));
    const touching = new Map();
    for (const e of snap.edges) {
      if (!e.observed) continue;
      for (const id of [e.from, e.to]) {
        if (!touching.has(id)) touching.set(id, new Set());
        for (const sig of e.signals) touching.get(id).add(sig);
      }
    }
    return { nodes, sourceType, touching };
  }

  // nodeSignals: which kinds of telemetry see this node first-hand.
  function nodeSignals(n, index) {
    const out = new Set();
    for (const [k, v] of Object.entries(n.labels || {})) if (k.startsWith('teia.signal.') && v === 'true') out.add(k.slice(12));
    for (const s of n.sources || []) {
      const t = index.sourceType.get(s);
      if (t === 'docker' || t === 'kubernetes') out.add('inventory');
      else if (t === 'declared') out.add('declared');
      else if (t === 'postgres' && n.kind === 'database') out.add('database');
    }
    for (const sig of index.touching.get(n.id) || []) if (sig === 'logs' || sig === 'database') out.add(sig);
    return [...out].filter(s => has(SIGNAL, s));
  }

  function labelKeys(snap, exclude = '') {
    const keys = new Map();
    for (const n of snap.nodes) for (const k of Object.keys(n.labels || {})) if (userLabel(k) && k !== exclude) keys.set(k, (keys.get(k) || 0) + 1);
    return [...keys.entries()].filter(([, c]) => c > 1).map(([k]) => k).sort();
  }

  // Percentile summaries are approximations, never combined distributions
  // or end-to-end journey latency. Weight only measured, positive traffic.
  function latencySum() {
    let total = 0, weight = 0, weight50 = 0, p50 = 0, p95 = 0, count = 0, approximate = false;
    return {
      add(x) {
        const rate = Math.max(0, finite(x.requestRate));
        total += rate;
        const coverage = typeof x.latencyCoverage === 'number' ? Math.max(0, Math.min(1, finite(x.latencyCoverage))) : 1;
        const w = rate * coverage;
        if (!(x.latencyP95Ms > 0 && w > 0)) return;
        weight += w; p95 += w * x.latencyP95Ms; count++;
        if (x.latencyP50Ms > 0) { weight50 += w; p50 += w * x.latencyP50Ms; }
        approximate ||= !!x.latencyApproximate;
      },
      result: () => ({ latencyP50Ms: weight50 > 0 ? p50 / weight50 : 0, latencyP95Ms: weight > 0 ? p95 / weight : 0,
        latencyApproximate: count > 1 || approximate, latencyCoverage: total > 0 ? weight / total : 0 }),
    };
  }

  // fmtSec writes seconds: "45 s", "12 min", "2.5 h".
  function fmtSec(s) {
    if (s >= 5400) return (s / 3600).toFixed(1) + ' h';
    if (s >= 90) return Math.round(s / 60) + ' min';
    return Math.round(s) + ' s';
  }

  // fmtBytes writes a size in binary units: "512 B", "48.0 MiB".
  function fmtBytes(b) {
    if (b < 1024) return Math.round(b) + ' B';
    let n = b / 1024, i = 0;
    while (n >= 1024 && i < 4) { n /= 1024; i++; }
    return n.toFixed(1) + ' ' + 'KMGTP'[i] + 'iB';
  }

  // fmtValue writes a measure's value in its unit.
  const fmtValue = (v, unit) => unit === 'milliseconds' ? fmtMs(v) : unit === 'bytes' ? fmtBytes(v) : unit === 'seconds' ? fmtSec(v) : unit === 'share' ? pct(v) : compact(v);

  // fmtMs writes milliseconds the way people read them, like teia's CLI.
  function fmtMs(ms) {
    if (ms >= 999.5) return (ms / 1000).toFixed(1) + ' s';
    if (ms >= 9.95) return ms.toFixed(0) + ' ms';
    return ms.toFixed(1) + ' ms';
  }
  const fmtRate = r => r >= 100 ? r.toFixed(0) : r >= 10 ? r.toFixed(1) : r.toFixed(2);
  const pct = v => (v * 100).toFixed(v > 0 && v < 0.1 ? 1 : 0) + '%';

  // Error rates describe the measured subset. Missing traffic must not
  // dilute that rate, and nested groups must preserve the denominator.
  const errorCoverage = x => x.errorsUnknown ? 0 : typeof x.errorCoverage === 'number'
    ? Math.max(0, Math.min(1, finite(x.errorCoverage))) : 1;
  function errorStats(items) {
    let requestRate = 0, measured = 0, failed = 0;
    for (const x of items) {
      const rate = Math.max(0, finite(x.requestRate)), known = rate * errorCoverage(x);
      requestRate += rate; measured += known; failed += known * finite(x.errorRate);
    }
    return { requestRate, errorRate: measured > 0 ? failed / measured : 0, errorsUnknown: measured === 0,
      errorCoverage: requestRate > 0 ? measured / requestRate : 0 };
  }
  function errorText(x, short = false) {
    const coverage = errorCoverage(x);
    if (!coverage || !(x.requestRate > 0)) return short ? 'err ?' : 'failures not measured';
    const value = pct(x.errorRate) + (short ? ' err' : ' errors');
    return coverage < 1 ? value + (short ? ` (${pct(coverage)} covered)` : ` · measured on ${pct(coverage)} of traffic`) : value;
  }
  function latencyText(x, short = false) {
    if (!(x.latencyP95Ms > 0)) return 'latency not measured';
    const label = x.latencyApproximate ? (short ? '≈ p95 ' : 'p95 approximation ') : 'p95 ';
    const value = label + fmtMs(x.latencyP95Ms);
    if (short) return value;
    const p50 = x.latencyP50Ms > 0 ? ` · p50 ${x.latencyApproximate ? 'approx. ' : ''}${fmtMs(x.latencyP50Ms)}` : '';
    const coverage = typeof x.latencyCoverage === 'number' && x.latencyCoverage < 1
      ? x.latencyCoverage > 0 ? ` · measured on ${pct(x.latencyCoverage)} of traffic` : ' · traffic coverage unknown' : '';
    return value + p50 + coverage + (x.latencyApproximate ? ' · summary of percentiles, not a combined distribution' : '');
  }

  // compact writes 1200 as 1.2k and 45000 as 45k.
  function compact(n) {
    if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 0 : 1) + 'M';
    if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e4 ? 0 : 1) + 'k';
    return String(Math.round(n));
  }

  // reasonTag is the few words a card or line shows for its worst reason:
  // what crossed its limit, and where it most likely comes from.
  // causeWords words a reason's cause. A switch, not a lookup: a cause from
  // the data never names a method to call (constructor, toString...).
  function causeWords(r) {
    switch (r.cause) {
      case 'errors': return `${pct(r.value)} errors${r.coverage > 0 && r.coverage < 1 ? ` (${pct(r.coverage)} covered)` : ''}`;
      case 'latency': return `${r.approximate ? '≈ ' : ''}p95 ${fmtMs(r.value)}`;
      case 'saturation': return `${pct(r.value)} ${r.what || 'used'}`;
      case 'backlog': return `${compact(r.value)} ${r.what || 'waiting'}`;
      case 'lag': return `${fmtSec(r.value)} ${r.what || 'behind'}`;
      case 'locks': return `${compact(r.value)} on locks`;
      case 'restarts': return `${compact(r.value)} restarts`;
      case 'evictions': return `${compact(r.value)} evictions`;
      case 'rejections': return `${compact(r.value)} rejected clients`;
      // An unknown cause (unsanitized data, a newer server) says what it can.
      default: return String(r.summary || r.cause || 'unhealthy');
    }
  }
  function reasonTag(r, more = 0) {
    const what = causeWords(r);
    const via = r.via ? (r.cause === 'locks' ? ` held by ${r.via}` : ` from ${r.via}`) : r.subject ? ` · ${r.subject}` : '';
    return what + via + (more > 0 ? ` +${more}` : '');
  }

  // placeTag picks where a w × h tag goes: the first spot (its centre) that
  // overlaps none of the obstacles ({x0, y0, x1, y1}); when every spot does,
  // the one that covers the least. Spots run from the middle of a line out.
  function placeTag(spots, w, h, obstacles, { strict = false } = {}) {
    const box = p => ({ x0: p.x - w / 2, y0: p.y - h / 2, x1: p.x + w / 2, y1: p.y + h / 2 });
    const overlap = (a, b) => Math.max(0, Math.min(a.x1, b.x1) - Math.max(a.x0, b.x0)) * Math.max(0, Math.min(a.y1, b.y1) - Math.max(a.y0, b.y0));
    let best = null;
    for (const p of spots) {
      const b = box(p);
      const covered = obstacles.reduce((sum, o) => sum + overlap(b, o), 0);
      if (covered === 0) return { ...p, box: b };
      if (!best || covered < best.covered) best = { ...p, box: b, covered };
    }
    if (strict || !best) return null;
    return { x: best.x, y: best.y, box: best.box };
  }

  // cardSubtitle is the second line of a card: what it is, then the golden
  // signals it has (traffic, latency, errors). When that does not fit, what
  // it is goes first: the icon and the details already say it.
  function cardSubtitle(n, max = 30) {
    const what = n.group ? `${n.members.length} workload${n.members.length === 1 ? '' : 's'}` : techLabel(n);
    const stats = [];
    if (n.requestRate > 0) stats.push(`${fmtRate(n.requestRate)}/s`);
    if (n.latencyP95Ms > 0) stats.push((n.latencyApproximate ? '≈ ' : '') + fmtMs(n.latencyP95Ms));
    if (n.requestRate > 0 && !n.group) {
      if (errorCoverage(n) < 1) stats.unshift(errorText(n, true));
      else if (n.errorRate > 0.0005) stats.push(errorText(n, true));
    }
    let text = [what, ...stats].join(' · ');
    if (text.length > max && stats.length) text = stats.join(' · ');
    return text.length > max ? text.slice(0, max - 1) + '…' : text;
  }

  // ---------------------------------------------------------- journeys
  // A journey is a path through the map: the services that take part in it
  // (by a label, "journey" unless said otherwise), what they call
  // downstream, and stubs where it is entered from or leaves to the rest.

  // The labels that can say which journeys a service takes part in, in the
  // order teia looks for them: its own, Kubernetes' recommended
  // app.kubernetes.io/part-of ("the higher-level application this is part
  // of"), and Backstage's system. Nothing needs adding where one exists.
  const JOURNEY_KEYS = ['journey', 'app.kubernetes.io/part-of', 'system'];
  const JOURNEY_KEY = JOURNEY_KEYS[0];

  // journeyKeyOf picks the journey label: the one asked for, else the first
  // of JOURNEY_KEYS some node carries.
  function journeyKeyOf(snap, preferred) {
    if (preferred) return preferred;
    if (snap.journeys) return snap.journeys.key;
    return JOURNEY_KEYS.find(k => snap.nodes.some(n => labelValues(n, k).length)) || JOURNEY_KEY;
  }

  // parseJourney reads "checkout" (a value of the journey label),
  // "team=checkout" or "team:checkout" (any label), or "from:orders" (what
  // a node depends on).
  function parseJourney(spec, key = JOURNEY_KEY) {
    const text = String(spec || '').trim();
    if (!text) return null;
    const m = /^([^=:\s]+)\s*[=:]\s*(.+)$/.exec(text);
    if (!m) return { text, key, value: text };
    if (m[1].toLowerCase() === 'from') return { text, entry: m[2].trim() };
    return { text, key: m[1], value: m[2].trim() };
  }

  // labelValues splits a label's value on commas: a service can take part in
  // several journeys ("checkout,browse").
  const labelValues = (n, key) => String(n?.labels?.[key] ?? '').split(',').map(v => v.trim().toLowerCase()).filter(Boolean);
  const memberships = (snap, n, key) => [...new Set([...labelValues(n, key), ...(key === journeyKeyOf(snap) ? strings(n?.journeys).map(v => v.toLowerCase()) : [])])];
  const isService = n => !!n && n.kind === 'service';

  // journeyOf expands a journey over a snapshot. Members are the nodes the
  // spec names; from them it follows calls downstream with plain rules:
  // datastores, caches, queues and externals are included but never walked
  // past (their other clients are not part of this journey); a service that
  // belongs to another value of the same label is where the journey leaves;
  // unlabelled services are followed maxDepth hops past the last member.
  function journeyOf(snap, spec, { maxDepth = 2, maxNodes = 200, key = JOURNEY_KEY } = {}) {
    const j = typeof spec === 'string' ? parseJourney(spec, key) : spec;
    const roles = new Map(); // id -> 'member' | 'dependency'
    if (!j) return { spec: j, roles, truncated: false, empty: true };
    const contract = snap.journeys;
    const supplied = !j.entry && contract?.key === j.key && contract.maxDepth === maxDepth && contract.maxNodes === maxNodes
      ? contract.scopes.find(s => s.name.toLowerCase() === j.value.toLowerCase()) : null;
    if (supplied) return { ...supplied, spec: j, roles: new Map(Object.entries(supplied.roles)), empty: !Object.keys(supplied.roles).length, backend: true };
    const nodes = new Map(snap.nodes.map(n => [n.id, n]));
    const out = new Map();
    for (const e of snap.edges) {
      if (!out.has(e.from)) out.set(e.from, []);
      out.get(e.from).push(e.to);
    }
    const want = j.entry ? null : j.value.toLowerCase();
    const member = n => !!n && (j.entry ? n.id.toLowerCase() === j.entry.toLowerCase() : memberships(snap, n, j.key).includes(want));
    const elsewhere = n => !j.entry && isService(n) && memberships(snap, n, j.key).length > 0 && !member(n);
    const queue = [];
    let truncated = false;
    for (const n of [...snap.nodes].sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0)) if (member(n)) {
      if (roles.size >= maxNodes) { truncated = true; continue; }
      roles.set(n.id, 'member'); queue.push([n.id, 0]);
    }
    for (let q = 0; q < queue.length; q++) {
      const [id, depth] = queue[q];
      if (!isService(nodes.get(id))) continue; // included, not walked past
      for (const to of [...new Set(out.get(id) || [])].sort()) {
        if (roles.has(to)) continue;
        const n = nodes.get(to);
        if (elsewhere(n)) continue; // owned by another journey: a stub
        const next = member(n) ? 0 : depth + 1;
        if (isService(n) && next > maxDepth) { truncated = true; continue; }
        if (roles.size >= maxNodes) { truncated = true; break; }
        roles.set(to, member(n) ? 'member' : 'dependency');
        queue.push([to, next]);
      }
    }
    return { spec: j, roles, truncated, empty: roles.size === 0 };
  }

  // scopeSnapshot keeps what a scope draws inside its frame: its nodes and
  // the communication between them. What crosses the frame becomes ports.
  function scopeSnapshot(snap, scope) {
    const inside = id => scope.roles.has(id);
    return { ...snap, nodes: snap.nodes.filter(n => inside(n.id)), edges: snap.edges.filter(e => inside(e.from) && inside(e.to)) };
  }

  // scopePins fixes the columns of a scope: the services the outside calls
  // into (and nothing inside calls) start it; datastores, queues and third
  // parties that call nothing end it, in one lane.
  function scopePins(view, ports) {
    const called = new Set(), calls = new Set();
    for (const e of view.edges.values()) { called.add(e.to); calls.add(e.from); }
    const entered = new Set(ports.filter(p => p.side === 'in').flatMap(p => p.anchors));
    const pin = new Map();
    for (const n of view.nodes.values()) {
      const resource = n.group ? n.members.every(isResource) : isResource(n);
      if (resource && !calls.has(n.id)) pin.set(n.id, 'last');
      else if (entered.has(n.id) && !called.has(n.id)) pin.set(n.id, 'first');
    }
    return pin;
  }

  // journeyWhy is a journey's headline: its worst health and the reason that
  // most likely causes it. Reasons that only pass on a slow or failing
  // dependency inside the journey give way to the one they point at.
  function journeyWhy(snap, journey) {
    const inside = id => journey.roles.has(id);
    const items = [
      ...snap.nodes.filter(n => inside(n.id)).map(n => ({ subject: n.id, edge: false, health: n.health, reasons: n.reasons || [] })),
      ...snap.edges.filter(e => inside(e.from) && inside(e.to)).map(e => ({ subject: e.from + '->' + e.to, edge: true, health: e.health, reasons: e.reasons || [] })),
    ];
    let health = 'unknown';
    for (const it of items) if (HEALTH_RANK[it.health] > HEALTH_RANK[health]) health = it.health;
    const passes = (r, cause) => r.via && inside(r.via) && snap.nodes.some(n => n.id === r.via && (n.reasons || []).some(x => x.cause === cause));
    const candidates = [];
    for (const it of items) for (const r of it.reasons) {
      if (r.health === health && !passes(r, r.cause)) candidates.push({ subject: it.subject, edge: it.edge, reason: r });
    }
    const weight = c => (c.reason.limit > 0 ? c.reason.value / c.reason.limit : 0);
    candidates.sort((a, b) => (b.edge - a.edge) || (weight(b) - weight(a)) || (a.subject < b.subject ? -1 : 1));
    return candidates.length ? { health, subject: candidates[0].subject, reason: candidates[0].reason, more: candidates.length - 1 } : { health, subject: '', reason: null, more: 0 };
  }

  // --------------------------------------------------------- filters
  // One filter model for the whole map. Journeys keep a journey and what it
  // calls; every other facet (any label, health, kind of component,
  // technology) keeps the nodes with one of its chosen values. Values of one
  // facet add up; facets narrow each other.
  //
  //   { journeys: ['checkout'], facets: { 'k8s.namespace': ['shop'], health: ['failing'] } }

  const BUILTIN_FACETS = ['health', 'category', 'tech'];

  // facetValues is what a node holds for a facet: a label's values (a list
  // for the journey label), its health, its kind of component, its tech.
  function facetValues(n, key) {
    if (key === 'health') return [n.health];
    if (key === 'category') return [categoryOf(n)];
    if (key === 'tech') return n.tech ? [n.tech] : [];
    // Third parties are only ever seen through their callers: not a blind spot to fix.
    if (key === 'visibility') return n.kind === 'external' || n.group ? [] : [n.visibility];
    return labelValues(n, key);
  }

  const emptyFilters = () => ({ journeys: [], facets: {} });
  const activeFacets = f => Object.entries(f.facets || {}).filter(([, v]) => v && v.length);
  const filtersActive = f => !!f && ((f.journeys || []).length > 0 || activeFacets(f).length > 0);

  // scopeOf keeps the nodes the filters name, as a journey does (a role per
  // node, so the same stubs and headline apply), or null for the whole map.
  function scopeOf(snap, filters) {
    if (!filtersActive(filters)) return null;
    if (filters.journeys.length === 1 && !activeFacets(filters).length) return { ...journeyOf(snap, filters.journeys[0], { key: filters.journeyKey || JOURNEY_KEY }), filters };
    const roles = new Map();
    let truncated = false;
    if (filters.journeys.length) {
      for (const spec of filters.journeys) {
        const j = journeyOf(snap, spec, { key: filters.journeyKey || JOURNEY_KEY });
        truncated ||= j.truncated;
        for (const [id, role] of j.roles) if (!roles.has(id) || role === 'member') roles.set(id, role);
      }
    } else {
      for (const n of snap.nodes) roles.set(n.id, 'member');
    }
    const nodes = new Map(snap.nodes.map(n => [n.id, n]));
    for (const [key, values] of activeFacets(filters)) {
      const want = new Set(values.map(v => String(v).toLowerCase()));
      for (const id of [...roles.keys()]) {
        if (!facetValues(nodes.get(id), key).some(v => want.has(String(v).toLowerCase()))) roles.delete(id);
      }
    }
    return { roles, truncated, empty: roles.size === 0, filters };
  }

  // facetsOf lists what the map can be filtered by, with how many nodes hold
  // each value and the worst health among them: journeys first, then every
  // label several nodes share, then health, kind of component, technology.
  function facetsOf(snap, key = JOURNEY_KEY) {
    const sections = [];
    const tally = (key, label) => {
      const values = new Map();
      for (const n of snap.nodes) {
        for (const v of facetValues(n, key)) {
          const t = values.get(v) || { value: v, count: 0, health: 'unknown' };
          t.count++;
          if (HEALTH_RANK[n.health] > HEALTH_RANK[t.health]) t.health = n.health;
          values.set(v, t);
        }
      }
      return [...values.values()].map(t => ({ ...t, label: label ? label(t.value) : t.value }));
    };
    const journeys = journeyNames(snap, key).map(name => {
      const j = journeyOf(snap, name, { key });
      return { value: name, label: name, count: j.roles.size, health: journeyWhy(snap, j).health };
    });
    if (journeys.length) sections.push({ key: 'journey', title: 'Journeys', values: journeys });
    for (const k of labelKeys(snap, key)) sections.push({ key: k, title: k, values: tally(k).sort((a, b) => a.label < b.label ? -1 : 1) });
    const order = ['failing', 'degraded', 'healthy', 'unknown'];
    sections.push({ key: 'health', title: 'Health', values: tally('health', v => HEALTH_LABEL[v]).sort((a, b) => order.indexOf(a.value) - order.indexOf(b.value)) });
    const sight = ['partial', 'none', 'full'];
    const visibility = tally('visibility', v => VIS_LABEL[v]).sort((a, b) => sight.indexOf(a.value) - sight.indexOf(b.value));
    if (visibility.length) sections.push({ key: 'visibility', title: 'Visibility', values: visibility });
    sections.push({ key: 'category', title: 'Component', values: tally('category', v => CATEGORY[v]?.label || v).sort((a, b) => b.count - a.count || (a.label < b.label ? -1 : 1)) });
    const techs = tally('tech', v => has(TECH, v) ? TECH[v].label : v).sort((a, b) => a.label < b.label ? -1 : 1);
    if (techs.length) sections.push({ key: 'tech', title: 'Technology', values: techs });
    return sections;
  }

  // filtersFromParams reads a shareable view: ?journey=checkout,browse and
  // ?filter=k8s.namespace:shop,health:failing (a key may repeat).
  function filtersFromParams(params) {
    const f = emptyFilters();
    const list = v => String(v || '').split(',').map(x => x.trim()).filter(Boolean);
    f.journeys = list(params.get('journey'));
    for (const item of list(params.get('filter'))) {
      const i = item.indexOf(':');
      if (i <= 0) continue;
      const key = item.slice(0, i), value = item.slice(i + 1);
      if (key === 'journey') { if (!f.journeys.includes(value)) f.journeys.push(value); continue; }
      (f.facets[key] ||= []).includes(value) || f.facets[key].push(value);
    }
    return f;
  }

  // filtersToParams writes them back the same way (empty strings: none).
  function filtersToParams(f) {
    const items = activeFacets(f).flatMap(([k, vs]) => vs.map(v => `${k}:${v}`));
    return { journey: (f.journeys || []).join(','), filter: items.join(',') };
  }

  // filterLabel names a filter for its chip and the screen reader.
  function filterLabel(key, value) {
    if (key === 'journey') return value.startsWith('from:') ? `depends on ${value.slice(5)}` : `journey ${value}`;
    if (key === 'health') return HEALTH_LABEL[value] || value;
    if (key === 'category') return CATEGORY[value]?.label || value;
    if (key === 'tech') return has(TECH, value) ? TECH[value].label : value;
    if (key === 'visibility') return VIS_LABEL[value] || value;
    return `${key} ${value}`;
  }

  // ------------------------------------------------------ scoped view
  // A scope (a journey or any filter) is drawn inside a frame. Whatever is
  // outside it becomes a port: a compact chip beyond the frame's edge,
  // callers on the left, where it leaves to and who shares its datastores on
  // the right, so outsiders never mix with the scope's own cards.

  // isResource: datastores, caches, queues, third parties: they end a flow.
  const isResource = n => !!n && !isService(n) && n.kind !== 'group';

  // journeyPorts splits a snapshot into what the scope draws inside its
  // frame and the ports around it. An outsider calling a service is a
  // caller (left); one calling a datastore, queue or third party is a tenant
  // (right, beside it); where the scope calls out is an exit (right), folded
  // by the outsider's journey ("↗ browse · 2"). Each side keeps its busiest
  // maxPerSide - 1 ports and one "+N more".
  function journeyPorts(snap, scope, { maxPerSide = 8, journeyKey = JOURNEY_KEY } = {}) {
    const inside = id => scope.roles.has(id);
    const nodes = new Map(snap.nodes.map(n => [n.id, n]));
    const ports = new Map();
    const port = (side, kind, key, label, foldJourney) => {
      const id = `port:${side}:${key}`;
      if (!ports.has(id)) ports.set(id, { id, side, kind, label, journey: foldJourney || '', of: [], anchors: [], requestRate: 0, health: 'unknown', reasons: [], edges: [] });
      return ports.get(id);
    };
    const add = (p, outsider, anchor, e) => {
      if (!p.of.includes(outsider)) p.of.push(outsider);
      if (!p.anchors.includes(anchor)) p.anchors.push(anchor);
      p.requestRate += e.requestRate || 0;
      if (HEALTH_RANK[e.health] > HEALTH_RANK[p.health]) p.health = e.health;
      p.reasons.push(...(e.reasons || []));
      p.edges.push(e);
    };
    const boundaries = scope.backend ? new Map(scope.boundaryPorts.map(p => [p.edgeId, p])) : null;
    for (const e of snap.edges) {
      const boundary = boundaries?.get(e.from + '->' + e.to);
      if (boundaries && !boundary) continue;
      const fromIn = inside(e.from), toIn = inside(e.to);
      if (fromIn === toIn) continue;
      if (toIn) {
        const target = nodes.get(e.to);
        const kind = boundary?.kind || (isResource(target) ? 'tenant' : 'caller');
        add(port(kind === 'tenant' ? 'out' : 'in', kind, e.from, e.from), e.from, e.to, e);
      } else {
        const j = memberships(snap, nodes.get(e.to), journeyKey)[0];
        add(port('out', 'exit', j ? 'journey:' + j : e.to, j ? `↗ ${j}` : `→ ${e.to}`, j), e.to, e.from, e);
      }
    }
    const out = [];
    for (const side of ['in', 'out']) {
      const list = [...ports.values()].filter(p => p.side === side).sort((a, b) => b.requestRate - a.requestRate || (a.id < b.id ? -1 : 1));
      if (list.length <= maxPerSide) { out.push(...list); continue; }
      const keep = list.slice(0, maxPerSide - 1), rest = list.slice(maxPerSide - 1);
      out.push(...keep, {
        id: `port:${side}:more`, side, kind: 'more', label: `+${rest.reduce((a, p) => a + p.of.length, 0)} more`, journey: '',
        of: rest.flatMap(p => p.of), anchors: [], requestRate: rest.reduce((a, p) => a + p.requestRate, 0),
        health: rest.reduce((h, p) => HEALTH_RANK[p.health] > HEALTH_RANK[h] ? p.health : h, 'unknown'), reasons: [], edges: [],
      });
    }
    for (const p of out) {
      if (p.kind === 'exit' && p.journey) p.label = `↗ ${p.journey}${p.of.length > 1 ? ' · ' + p.of.length : ''}`;
      p.reasons.sort((a, b) => HEALTH_RANK[b.health] - HEALTH_RANK[a.health]);
    }
    return out;
  }

  // packColumn stacks items of the given sizes in their order, each as close
  // to where it wants to be (desired centre) as it can without overlapping:
  // overlapping runs move together to the mean of what they want.
  function packColumn(items, gap) {
    const clusters = [];
    items.forEach((it, i) => {
      clusters.push({ first: i, last: i, size: it.size, want: it.desired - it.size / 2, n: 1 });
      while (clusters.length > 1) {
        const b = clusters[clusters.length - 1], a = clusters[clusters.length - 2];
        const aTop = a.want / a.n, bTop = b.want / b.n;
        if (aTop + a.size + gap <= bTop) break;
        // b's items sit after a's: their wanted tops move up by a's size and the gap
        const shift = a.size + gap;
        clusters.splice(-2, 2, { first: a.first, last: b.last, size: a.size + gap + b.size, want: a.want + b.want - shift * b.n, n: a.n + b.n });
      }
    });
    const ys = [];
    for (const c of clusters) {
      let top = c.want / c.n;
      for (let i = c.first; i <= c.last; i++) { ys[i] = top + items[i].size / 2; top += items[i].size + gap; }
    }
    return ys;
  }

  // chipText is what a port's chip says: who, and how much traffic.
  const chipText = p => (p.label.length > 18 ? p.label.slice(0, 17) + '…' : p.label) + (p.requestRate > 0 ? `  ${fmtRate(p.requestRate)}/s` : '');

  // frameLayout places the frame around a scope's cards, the lane of its
  // datastores, the port chips beyond the frame and the route of each port
  // line. A line that would cross a card runs along the top or bottom
  // corridor of the frame instead.
  function frameLayout(pos, ports, { nodeW = 196, nodeH = 62, lane = [], pad = { x: 36, top: 108, bottom: 36 }, portGap = 70, pitch = 34 } = {}) {
    const ids = Object.keys(pos);
    if (!ids.length) return null;
    const xs = ids.map(id => pos[id].x), ys = ids.map(id => pos[id].y);
    const frame = {
      x0: Math.min(...xs) - nodeW / 2 - pad.x, x1: Math.max(...xs) + nodeW / 2 + pad.x,
      y0: Math.min(...ys) - nodeH / 2 - pad.top, y1: Math.max(...ys) + nodeH / 2 + pad.bottom,
    };
    const corridor = { top: frame.y0 + 64, bottom: frame.y1 - 16 }; // under the header, above the lane's caption
    const laneXs = lane.filter(id => pos[id]).map(id => pos[id].x);
    const band = laneXs.length ? { x0: Math.min(...laneXs) - nodeW / 2 - 18, x1: Math.max(...laneXs) + nodeW / 2 + 18 } : null;
    const cards = ids.map(id => ({ id, x0: pos[id].x - nodeW / 2 - 6, x1: pos[id].x + nodeW / 2 + 6, y0: pos[id].y - nodeH / 2 - 10, y1: pos[id].y + nodeH / 2 + 10 }));
    // blocked: a straight run at y from x0 to x1 would cross a card other than the anchor's
    const blocked = (y, xa, xb, except) => cards.some(c => c.id !== except && c.y0 < y && y < c.y1 && Math.min(xa, xb) < c.x1 && c.x0 < Math.max(xa, xb));
    const chipW = p => Math.round(Math.max(92, Math.min(196, 40 + chipText(p).length * 6.6)));
    const routeFor = (p, anchor) => {
      const a = pos[anchor];
      const edgeX = p.side === 'in' ? frame.x0 : frame.x1;
      const sideX = p.side === 'in' ? a.x - nodeW / 2 : a.x + nodeW / 2;
      if (!blocked(a.y, sideX, edgeX, anchor)) return { anchor, mode: 'direct', y: a.y, from: { x: sideX, y: a.y } };
      const viaTop = Math.abs(a.y - corridor.top) <= Math.abs(a.y - corridor.bottom);
      const y = viaTop ? corridor.top : corridor.bottom;
      return { anchor, mode: viaTop ? 'top' : 'bottom', y, from: { x: a.x, y: a.y + (viaTop ? -nodeH / 2 : nodeH / 2) } };
    };
    const chips = [];
    for (const side of ['in', 'out']) {
      const list = ports.filter(p => p.side === side).map(p => {
        const routes = p.anchors.filter(id => pos[id]).map(id => routeFor(p, id));
        const desired = routes.length ? routes.reduce((sum, r) => sum + r.y, 0) / routes.length : frame.y1 - 13;
        return { port: p, routes, desired, w: chipW(p) };
      }).sort((a, b) => a.desired - b.desired || (a.port.id < b.port.id ? -1 : 1));
      const yAt = packColumn(list.map(c => ({ desired: c.desired, size: pitch - 8 })), 8);
      list.forEach((c, i) => {
        c.y = yAt[i];
        c.x = side === 'in' ? frame.x0 - portGap - c.w / 2 : frame.x1 + portGap + c.w / 2;
        chips.push(c);
      });
    }
    return { frame, band, corridor, chips };
  }

  // scopeStats are the numbers on the frame's header: what enters the scope,
  // with p95 and errors weighted the same way. Traffic enters from callers
  // outside, and where it starts inside: the calls of a part nothing calls
  // (a frontend fed by users teia does not see). A tenant's calls into a
  // shared datastore are not the scope's traffic.
  function scopeStats(snap, scope) {
    const inside = id => scope.roles.has(id);
    const nodes = new Map(snap.nodes.map(n => [n.id, n]));
    const called = new Set(snap.edges.map(e => e.to));
    const latency = latencySum();
    const into = scope.backend ? snap.edges.filter(e => scope.entryEdges.includes(e.from + '->' + e.to))
      : snap.edges.filter(e => inside(e.to) && (inside(e.from) ? !called.has(e.from) : !isResource(nodes.get(e.to))));
    for (const e of into) {
      latency.add(e);
    }
    const errors = errorStats(into);
    const timing = latency.result();
    return { parts: scope.roles.size, ...errors, ...timing, rate: errors.requestRate, p95: timing.latencyP95Ms };
  }

  // journeyNames lists the journeys the label names, for the selector.
  function journeyNames(snap, key = JOURNEY_KEY) {
    if (snap.journeys?.key === key) return snap.journeys.scopes.map(s => s.name).sort();
    const names = new Set();
    for (const n of snap.nodes) for (const v of memberships(snap, n, key)) if (v.trim()) names.add(v.trim());
    return [...names].sort();
  }

  // Ordered paths are JSON, so keys/values containing punctuation cannot collide.
  function groupingPath(node, groupBy) {
    if (!groupBy.startsWith('hierarchy:')) {
      const value = node?.labels?.[groupBy];
      return value ? [{ id: 'group:' + value, title: value, labelKey: groupBy, expansionKey: value }] : [];
    }
    let keys;
    try { keys = JSON.parse(groupBy.slice(10)); } catch { return []; }
    if (!Array.isArray(keys) || !keys.length || keys.length > 6 || keys.some(k => typeof k !== 'string')) return [];
    const path = [];
    return keys.flatMap(key => {
      // Externals without hierarchy labels remain distinct and reachable.
      const value = node?.labels?.[key];
      if (!value) return [];
      path.push([key, value]);
      const id = 'group:path:' + JSON.stringify(path);
      return [{ id, title: String(value), labelKey: key, expansionKey: id }];
    });
  }

  function neighbourOrder(edges, by = 'traffic') {
    const value = e => by === 'errors' ? (errorStats(e.members || [e]).errorCoverage > 0 ? e.errorRate : -1)
      : by === 'latency' ? (e.latencyP95Ms > 0 ? e.latencyP95Ms : -1) : e.requestRate;
    return [...edges].sort((a, b) => (value(b) ?? -1) - (value(a) ?? -1) || a.id.localeCompare(b.id));
  }

  // buildView: what is drawn. Grouping by a label folds nodes into one card
  // and sums the traffic between groups.
  function buildView(snap, index, { groupBy = '', expanded = new Set() } = {}) {
    const groupInfo = new Map(snap.nodes.map(n => [n.id, groupingPath(n, groupBy).find(g => !expanded.has(g.expansionKey))]));
    const groupOf = id => groupInfo.get(id)?.id || null;
    const nodes = new Map();
    for (const n of snap.nodes) {
      const g = groupOf(n.id);
      if (!g) { nodes.set(n.id, { ...n, title: n.id, signals: nodeSignals(n, index) }); continue; }
      let gn = nodes.get(g);
      if (!gn) {
        const info = groupInfo.get(n.id);
        gn = { ...info, group: true, kind: 'group', members: [], health: 'unknown', visibility: 'none', requestRate: 0, latency: latencySum(), reasons: [], measures: [], signals: [], labels: { [info.labelKey]: info.title } };
        nodes.set(g, gn);
      }
      gn.members.push(n);
      gn.links = [...(gn.links || []), ...(n.links || []).map(l => ({ ...l, label: n.id + ': ' + l.label }))];
      gn.healthRules = [...(gn.healthRules || []), ...(n.healthRules || []).map(r => ({ ...r, subject: n.id }))];
      gn.evidence = [...(gn.evidence || []), ...(n.evidence || []).map(ev => ({ ...ev, subject: { node: n.id } }))];
      if (HEALTH_RANK[n.health] > HEALTH_RANK[gn.health]) gn.health = n.health;
      if (n.visibility === 'full' || (n.visibility === 'partial' && gn.visibility === 'none')) gn.visibility = n.visibility;
      gn.requestRate += n.requestRate || 0;
      gn.latency.add(n);
      gn.reasons.push(...n.reasons.map(r => ({ ...r, subject: n.id })));
      gn.signals = [...new Set([...gn.signals, ...nodeSignals(n, index)])];
    }
    const edges = new Map(), internal = new Map();
    for (const e of snap.edges) {
      const from = groupOf(e.from) || e.from, to = groupOf(e.to) || e.to;
      if (from === to) { internal.set(from, (internal.get(from) || 0) + 1); continue; }
      const id = from + '->' + to;
      let ve = edges.get(id);
      if (!ve) {
        ve = { id, from, to, members: [], requestRate: 0, latency: latencySum(), reasons: [], measures: [], signals: new Set(), states: new Set(), declared: false, observed: false, health: 'unknown', protocol: e.protocol, reason: e.reason, port: e.port, sources: new Set() };
        edges.set(id, ve);
      }
      ve.members.push(e);
      ve.links = [...(ve.links || []), ...(e.links || []).map(l => from === e.from && to === e.to ? l : ({ ...l, label: `${e.from} → ${e.to}: ${l.label}` }))];
      ve.healthRules = [...(ve.healthRules || []), ...(e.healthRules || []).map(r => ({ ...r, subject: `${e.from} → ${e.to}` }))];
      // Equal source/timing/count metadata on two different relationships
      // is still two samples. Keep their identities when details deduplicate.
      ve.evidence = [...(ve.evidence || []), ...(e.evidence || []).map(ev =>
        from === e.from && to === e.to ? ev : { ...ev, subject: { from: e.from, to: e.to } })];
      ve.requestRate += e.requestRate;
      ve.latency.add(e);
      ve.measures.push(...e.measures);
      if (e.reasons.length) ve.reasons.push(...e.reasons.map(r => (from === e.from && to === e.to ? r : { ...r, subject: `${e.from}→${e.to}` })));
      e.signals.forEach(s => ve.signals.add(s));
      e.sources.forEach(s => ve.sources.add(s));
      ve.states.add(e.state);
      ve.declared ||= e.declared; ve.observed ||= e.observed;
      if (HEALTH_RANK[e.health] > HEALTH_RANK[ve.health]) ve.health = e.health;
    }
    for (const ve of edges.values()) {
      Object.assign(ve, errorStats(ve.members));
      Object.assign(ve, ve.latency.result());
      if (ve.members.length === 1) {
        const member = ve.members[0];
        ve.expectedFrequencySeconds = member.expectedFrequencySeconds;
        ve.frequency = member.frequency;
        // A single sampled edge can have latency without a request rate.
        ve.latencyP50Ms = member.latencyP50Ms; ve.latencyP95Ms = member.latencyP95Ms;
        ve.latencyApproximate = !!member.latencyApproximate;
      }
      delete ve.latency;
      ve.reasons.sort((a, b) => HEALTH_RANK[b.health] - HEALTH_RANK[a.health]);
      ve.state = ve.states.has('ghost') ? 'ghost' : ve.states.has('ok') ? 'ok' : 'stale';
      ve.signals = [...ve.signals]; ve.sources = [...ve.sources];
      delete ve.states;
    }
    for (const n of nodes.values()) {
      if (!n.group) continue;
      Object.assign(n, n.latency.result());
      delete n.latency;
      n.reasons.sort((a, b) => HEALTH_RANK[b.health] - HEALTH_RANK[a.health]);
    }
    for (const [g, c] of internal) if (nodes.has(g)) nodes.get(g).internal = c;
    return { nodes, edges, groupOf };
  }

  // matcher: free text matches names; key:value matches fields and labels.
  function matcher(q) {
    const terms = String(q || '').trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (!terms.length) return null;
    return n => terms.every(t => {
      const eq = t.indexOf('=');
      if (eq > 0) { // key=value: an exact label value (one of a comma list)
        const k = t.slice(0, eq), v = t.slice(eq + 1);
        const labels = n.group ? n.members.map(m => m.labels || {}) : [n.labels || {}];
        return labels.some(l => Object.entries(l).some(([lk, lv]) => lk.toLowerCase() === k && String(lv).toLowerCase().split(',').map(x => x.trim()).includes(v)));
      }
      const i = t.indexOf(':');
      if (i > 0) {
        const k = t.slice(0, i), v = t.slice(i + 1);
        const fields = { kind: n.kind, tech: n.tech || '', category: categoryOf(n), health: n.health, visibility: n.visibility, id: n.id, why: (n.reasons || []).map(r => r.cause).join(' ') };
        if (has(fields, k)) return String(fields[k]).toLowerCase().includes(v);
        const labels = n.group ? n.members.map(m => m.labels || {}) : [n.labels || {}];
        return labels.some(l => Object.entries(l).some(([lk, lv]) => lk.toLowerCase() === k && String(lv).toLowerCase().includes(v)));
      }
      return n.title.toLowerCase().includes(t) || (!!n.group && n.members.some(m => m.id.toLowerCase().includes(t)));
    });
  }

  // layered: columns so that edges point right, rows ordered to keep lines
  // short. Back edges (cycles) are set aside first, columns are renumbered
  // without gaps, and neighbours are indexed once, so 500 nodes and 1000
  // edges lay out in a few milliseconds.
  // pin (id -> 'first' | 'last') fixes a column, as ELK's layer constraints
  // do: entries start the flow, datastores and third parties end it.
  function layered(view, { colGap = 300, rowGap = 112, pin = new Map() } = {}) {
    const ids = [...view.nodes.keys()].sort();
    const out = new Map(ids.map(id => [id, []])), inc = new Map(ids.map(id => [id, []]));
    for (const e of view.edges.values()) {
      if (out.has(e.from) && inc.has(e.to)) { out.get(e.from).push(e.to); inc.get(e.to).push(e.from); }
    }
    // 1. drop back edges with an iterative DFS
    const forward = new Map(ids.map(id => [id, []]));
    const mark = new Map(); // 1 = on the stack, 2 = done
    for (const start of ids) {
      if (mark.has(start)) continue;
      const stack = [[start, 0]];
      mark.set(start, 1);
      while (stack.length) {
        const top = stack[stack.length - 1];
        const next = out.get(top[0])[top[1]++];
        if (next === undefined) { mark.set(top[0], 2); stack.pop(); continue; }
        const m = mark.get(next);
        if (m === 1) continue; // back edge: ignored for layering
        forward.get(top[0]).push(next);
        if (!m) { mark.set(next, 1); stack.push([next, 0]); }
      }
    }
    // 2. longest path over the DAG, in topological order
    const indeg = new Map(ids.map(id => [id, 0]));
    for (const id of ids) for (const t of forward.get(id)) indeg.set(t, indeg.get(t) + 1);
    const queue = ids.filter(id => indeg.get(id) === 0), depth = new Map(ids.map(id => [id, 0]));
    for (let q = 0; q < queue.length; q++) {
      const id = queue[q];
      for (const t of forward.get(id)) {
        depth.set(t, Math.max(depth.get(t), depth.get(id) + 1));
        indeg.set(t, indeg.get(t) - 1);
        if (indeg.get(t) === 0) queue.push(t);
      }
    }
    // 3. nodes nobody calls sit right before their nearest callee
    for (const id of ids) {
      if (inc.get(id).length || !forward.get(id).length || pin.get(id) === 'first') continue;
      depth.set(id, Math.max(0, Math.min(...forward.get(id).map(t => depth.get(t))) - 1));
    }
    // pinned columns: the first, and one past every other node
    const free = ids.filter(id => pin.get(id) !== 'last');
    const lastCol = (free.length ? Math.max(...free.map(id => depth.get(id))) : -1) + 1;
    for (const [id, where] of pin) if (depth.has(id)) depth.set(id, where === 'first' ? 0 : lastCol);
    // 4. renumber columns without gaps
    const used = [...new Set(depth.values())].sort((a, b) => a - b);
    const colOf = new Map(used.map((d, i) => [d, i]));
    const cols = used.map(() => []);
    for (const id of ids) cols[colOf.get(depth.get(id))].push(id);
    // 5. barycentre sweeps with precomputed neighbour lists
    const row = new Map();
    cols.forEach(c => c.forEach((id, i) => row.set(id, i)));
    const avg = list => list.length ? list.reduce((a, id) => a + row.get(id), 0) / list.length : null;
    for (let sweep = 0; sweep < 4; sweep++) {
      const down = sweep % 2 === 0;
      for (const c of down ? cols : [...cols].reverse()) {
        const key = new Map(c.map(id => { const b = avg(down ? inc.get(id) : out.get(id)); return [id, b === null ? row.get(id) : b]; }));
        c.sort((a, b) => key.get(a) - key.get(b) || (a < b ? -1 : 1));
        c.forEach((id, i) => row.set(id, i));
      }
    }
    const pos = {};
    cols.forEach((c, x) => c.forEach((id, y) => { pos[id] = { x: x * colGap, y: (y - (c.length - 1) / 2) * rowGap }; }));
    return pos;
  }

  // AUTO_GROUP_NODES is the most components a map draws one by one when
  // nobody chose a grouping: past it a browser spends seconds on every round
  // (2 000 components and 8 000 connections took 16 s a round in Chromium),
  // and people read tens of nodes, not thousands. Such a map opens grouped by
  // the first of AUTO_GROUP_KEYS that fits; No grouping still shows them all.
  const AUTO_GROUP_NODES = 1000;
  const AUTO_GROUP_KEYS = ['k8s.namespace', 'namespace', 'team', 'system', 'app.kubernetes.io/part-of', 'service.namespace'];
  // autoGroupKey is the label a large map opens grouped by: one most of its
  // components carry, giving at least two groups of three or more on average;
  // empty for a map small enough to draw whole, or when no label fits.
  function autoGroupKey(snap, keys) {
    const nodes = snap?.nodes || [];
    if (nodes.length <= AUTO_GROUP_NODES) return '';
    for (const key of AUTO_GROUP_KEYS) {
      if (!keys.includes(key)) continue;
      const values = new Set();
      let carried = 0;
      for (const n of nodes) {
        const v = n.labels?.[key];
        if (v) { values.add(v); carried++; }
      }
      if (carried * 2 >= nodes.length && values.size >= 2 && values.size * 3 <= nodes.length) return key;
    }
    return '';
  }

  // particleBudget shares a fixed number of animated dots among edges by
  // traffic, so a thousand edges cost the same per frame as fifty.
  function particleBudget(edges, total = 360) {
    const live = edges.filter(e => e.observed);
    const weight = e => e.requestRate > 0 ? 1 + Math.log2(1 + e.requestRate) : 0.5;
    const sum = live.reduce((a, e) => a + weight(e), 0) || 1;
    const counts = new Map();
    for (const e of live) counts.set(e.id, Math.max(live.length > total ? 0 : 1, Math.min(6, Math.round(total * weight(e) / sum))));
    return counts;
  }

  // safeApiBase only lets http(s) URLs through: the base ends up in links.
  function safeApiBase(api, base) {
    if (!api) return null;
    try {
      const u = new URL(api, base);
      if (u.protocol !== 'http:' && u.protocol !== 'https:') return null;
      return u.href.endsWith('/') ? u.href : u.href + '/';
    } catch { return null; }
  }

  const core = { sanitize, signature, indexSnapshot, nodeSignals, labelKeys, buildView, matcher, layered, autoGroupKey, AUTO_GROUP_NODES, particleBudget, safeApiBase, categoryOf, cardSubtitle, fmtMs, fmtSec, fmtBytes, reasonTag, compact, placeTag, parseJourney, journeyOf, scopeSnapshot, scopePins, journeyWhy, journeyNames, scopeOf, facetsOf, facetValues, journeyKeyOf, JOURNEY_KEYS, filtersFromParams, filtersToParams, filterLabel, filtersActive, emptyFilters, journeyPorts, packColumn, frameLayout, scopeStats, TECH, CATEGORY, SIGNAL, REASON };
  function operationsOf(value) {
    if (!value || value.version !== 1 || value.mode !== 'operation' || !Array.isArray(value.scopes) || !Array.isArray(value.sources)) return undefined;
    const scopes = value.scopes.filter(s => s && typeof s.id === 'string' && typeof s.operation === 'string' && typeof s.entryService === 'string' &&
      Array.isArray(s.nodes) && s.nodes.every(n => n && typeof n.id === 'string') &&
      Array.isArray(s.edges) && s.edges.every(e => e && typeof e.from === 'string' && typeof e.to === 'string' && Array.isArray(e.calls) && e.calls.every(c => c && typeof c.traceId === 'string')) &&
      Array.isArray(s.traces) && s.traces.every(t => t && typeof t.traceId === 'string' && Array.isArray(t.spans) && t.spans.every(p => p && typeof p.spanId === 'string' && typeof p.nodeId === 'string')));
    return { ...value, scopes, sources: value.sources.filter(s => s && typeof s.source === 'string'), unattributed: Array.isArray(value.unattributed) ? value.unattributed.filter(Boolean) : [] };
  }
  function operationSnapshot(base, id, traceId = '') {
    const scope = base.operations?.scopes.find(s => s.id === id);
    const traces = traceId ? (scope?.traces || []).filter(t => t.traceId === traceId) : [];
    // A vanished operation must not silently fall back to the platform graph.
    const edges = (scope?.edges || []).flatMap(e => {
      const calls = traceId ? e.calls.filter(c => c.traceId === traceId) : e.calls;
      if (!calls.length || (traceId && !traces.length)) return [];
      const starts = calls.map(c => timestamp(c.startedAt)).filter(Boolean).sort();
      const ends = calls.map(c => timestamp(c.endedAt)).filter(Boolean).sort();
      return [{ from: e.from, to: e.to, declared: false, observed: true, state: 'ok', health: 'unknown', errorsUnknown: true,
        signals: ['traces'], sources: [scope.source], evidence: [{ source: scope.source, signal: 'traces', sampleCount: calls.length,
          observedFrom: starts.length === calls.length ? starts[0] : '', observedAt: ends.length === calls.length ? ends.at(-1) : '', windowSeconds: 0, metrics: [] }] }];
    });
    const ids = new Set(traceId ? traces.flatMap(t => t.spans.map(p => p.nodeId)) : (scope?.nodes || []).map(n => n.id));
    edges.forEach(e => { ids.add(e.from); ids.add(e.to); });
    const original = new Map(base.nodes.map(n => [n.id, n]));
    const nodes = [...ids].filter(Boolean).map(nodeId => {
      const n = original.get(nodeId);
      return { id: nodeId, kind: n?.kind || scope?.nodes.find(n => n.id === nodeId)?.kind || 'unknown', tech: n?.tech,
        labels: { ...Object.fromEntries(Object.entries(n?.labels || {}).filter(([key]) => !key.startsWith('teia.signal.'))), 'teia.signal.traces': 'true' }, identity: n?.identity || [], sources: scope ? [scope.source] : [], health: 'unknown', visibility: 'partial', errorsUnknown: true };
    });
    const projected = sanitize({ ...base, nodes, edges, findings: [], changes: [], journeys: undefined, drift: false,
      coverage: { visibility: 0, declared: 0, observed: 0 } });
    projected.operationContext = { id, traceId, operation: scope?.operation || (base.slice ? 'not sampled in this slice' : 'unavailable in this capture'), unavailable: !scope || (!!traceId && !traces.length) };
    return projected;
  }
  Object.assign(core, { groupingPath, neighbourOrder, mapReasons, safeLinks, investigateSnapshot, compareSnapshots, operationsOf, operationSnapshot });

  // A bounded view of observed topology, never a claim that a path is one request.
  function investigateSnapshot(snap, options = {}) {
    const mode = oneOf(options.mode, ['all', 'neighbors', 'path'], 'all');
    const limit = Math.max(1, Math.min(1000, options.limit || 200));
    const depth = Math.max(1, Math.min(3, options.depth || 1));
    const direction = oneOf(options.direction, ['both', 'in', 'out'], 'both');
    const ids = new Set(snap.nodes.map(n => n.id)), selected = new Set(), pathEdges = new Set();
    let limited = false, found = mode !== 'path';
    if (mode === 'all') return { snapshot: snap, mode, hiddenNodes: 0, boundaryEdges: 0, limited: false, found: true };
    const adjacent = new Map(snap.nodes.map(n => [n.id, []]));
    for (const e of snap.edges) {
      if (!ids.has(e.from) || !ids.has(e.to)) continue;
      if (mode === 'path' || direction !== 'in') adjacent.get(e.from).push({ id: e.to, edge: e.from + '->' + e.to });
      if (mode !== 'path' && direction !== 'out') adjacent.get(e.to).push({ id: e.from, edge: e.from + '->' + e.to });
    }
    for (const neighbors of adjacent.values()) neighbors.sort((a, b) => a.id.localeCompare(b.id));
    if (ids.has(options.focus)) {
      if (mode === 'neighbors') {
        selected.add(options.focus);
        const queue = [[options.focus, 0]];
        for (let i = 0; i < queue.length; i++) {
          const [id, distance] = queue[i];
          if (distance === depth) continue;
          for (const next of adjacent.get(id)) if (!selected.has(next.id)) {
            if (selected.size === limit) { limited = true; continue; }
            selected.add(next.id); queue.push([next.id, distance + 1]);
          }
        }
      } else if (ids.has(options.target)) {
        const queue = [options.focus], previous = new Map([[options.focus, null]]);
        for (let i = 0; i < queue.length && !previous.has(options.target); i++) {
          for (const next of adjacent.get(queue[i])) if (!previous.has(next.id)) {
            previous.set(next.id, { id: queue[i], edge: next.edge }); queue.push(next.id);
          }
        }
        found = previous.has(options.target);
        if (found) {
          let id = options.target;
          while (id != null) { selected.add(id); const p = previous.get(id); if (p) pathEdges.add(p.edge); id = p?.id; }
          // A path is never silently shortened: a long route must be narrowed by the operator.
          if (selected.size > limit) { limited = true; selected.clear(); pathEdges.clear(); }
        }
      }
    }
    const nodes = snap.nodes.filter(n => selected.has(n.id));
    const edges = snap.edges.filter(e => selected.has(e.from) && selected.has(e.to) && (mode !== 'path' || pathEdges.has(e.from + '->' + e.to)));
    return { snapshot: { ...snap, nodes, edges }, mode, found, limited, hiddenNodes: snap.nodes.length - nodes.length,
      boundaryEdges: snap.edges.filter(e => selected.has(e.from) !== selected.has(e.to)).length };
  }

  // Stable structural/state differences. Collection timestamps and rate jitter
  // are not changes in declared architecture; both original records remain available.
  function compareSnapshots(before, after) {
    const canonical = value => JSON.stringify(value && typeof value === 'object' && !Array.isArray(value)
      ? Object.fromEntries(Object.keys(value).sort().map(key => [key, value[key]])) : value ?? null);
    // Identity records carry collection times that move every round; only the
    // decision itself (input, result, method) is a change between captures.
    // A capture without identity metadata (older stores, plain frames) is not comparable on it.
    const decisions = (records, role = '') => [...new Set((records || []).map(r => `${role}${r.input} → ${r.resolvedId} (${r.method})`))].sort();
    const differs = (field, a, b) => field === 'identity' && (!a.identity.length || !b.identity.length) ? false : canonical(a[field]) !== canonical(b[field]);
    const compare = (type, oldItems, newItems, fields, keyOf, project) => {
      const old = new Map(oldItems.map(n => [keyOf(n), n])), next = new Map(newItems.map(n => [keyOf(n), n]));
      return [...new Set([...old.keys(), ...next.keys()])].sort().map(id => {
        const a = old.get(id), b = next.get(id), pa = a && project(a), pb = b && project(b);
        const changes = a && b ? fields.filter(field => differs(field, pa, pb)).map(field => ({ field, before: pa[field] ?? null, after: pb[field] ?? null })) : [];
        return { type, id, status: !a ? 'added' : !b ? 'removed' : changes.length ? 'changed' : 'unchanged', before: a, after: b, changes };
      });
    };
    const count = rows => Object.fromEntries(['added', 'removed', 'changed', 'unchanged'].map(status => [status, rows.filter(row => row.status === status).length]));
    // A slice holds only the calls that carried its dimensions, and a cluster
    // only its own components: against another scope a capture differs in
    // scope, not by change.
    const scopeOf = snap => ({ cluster: snap.cluster || '', slice: snap.slice ? sliceText(snap.slice.selector) : '' });
    const scope = { before: scopeOf(before), after: scopeOf(after) };
    if (scope.before.cluster !== scope.after.cluster || scope.before.slice !== scope.after.slice) return { comparable: false, scope, nodes: [], edges: [], counts: count([]) };
    const nodes = compare('node', before.nodes, after.nodes, ['kind', 'tech', 'health', 'visibility', 'labels', 'addresses', 'errorsUnknown', 'identity'], n => n.id,
      n => ({ ...n, identity: decisions(n.identity) }));
    const edges = compare('edge', before.edges, after.edges, ['state', 'health', 'declared', 'observed', 'protocol', 'port', 'errorsUnknown', 'identity'], e => e.from + '->' + e.to,
      e => ({ ...e, identity: [...decisions(e.identity?.from, 'caller '), ...decisions(e.identity?.to, 'callee ')] }));
    return { comparable: true, scope, nodes, edges, counts: count([...nodes, ...edges]) };
  }

  // ============================================================== mount
  function mount(host, opts = {}) {
    loadFont();
    opts = { mode: 'page', ...opts };
    const page = opts.mode === 'page';
    if (opts.api) {
      opts.api = safeApiBase(opts.api, typeof location !== 'undefined' ? location.href : undefined);
      if (!opts.api) console.warn('teia: ignoring the API URL: only http and https are allowed');
    }

    // A fresh wrapper per mount: an old instance still finishing async work
    // can only touch its own, detached elements.
    const root = host.shadowRoot || host.attachShadow({ mode: 'open' });
    const app = document.createElement('div');
    app.className = `teia ${opts.mode}${opts.comparison ? ' comparison' : ''}`;
    app.innerHTML = MARKUP;
    const style = document.createElement('style');
    style.textContent = STYLE + `
      .workspace-dialog { width:min(1180px,94vw); max-height:90vh; overflow:auto; color:var(--ink); }
      .comparison #liveBtn,.comparison #flowBtn,.comparison #stepsBtn,.comparison #exportMenu { display:none; }
      .workspace-dialog p { color:var(--ink-2); line-height:1.5; }
      .workspace-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(260px,100%),1fr)); gap:14px; margin:12px 0; }
      .workspace-dialog select,.workspace-dialog textarea { box-sizing:border-box; width:100%; border:1px solid var(--line-2); border-radius:6px; padding:6px 8px; background:var(--field); color:var(--ink); font:inherit; }
      .workspace-dialog input[type=file] { max-width:100%; }
      .workspace-dialog textarea { min-height:90px; }
      .workspace-dialog label { display:block; margin:10px 0; }
      .workspace-dialog pre { white-space:pre-wrap; overflow-wrap:anywhere; max-height:300px; overflow:auto; }
      .workspace-card { min-width:0; }
      .workspace-footer { position:sticky; bottom:-20px; background:var(--panel); padding:12px 0; border-top:1px solid var(--line); z-index:6; }
      .workspace-status { padding:10px; border:1px solid var(--line-2); border-radius:6px; overflow-wrap:anywhere; }
      .workspace-status:empty { display:none; }
      .investigation-summary { position:absolute; top:8px; left:12px; right:60px; z-index:4; background:var(--panel); padding:6px 8px; border:1px solid var(--line-2); border-radius:6px; font-size:12px;
        display:flex; flex-wrap:wrap; align-items:center; gap:6px 10px; }
      .investigation-summary[hidden] { display:none; }
      .entity-table select { margin-left:6px; border:1px solid var(--line-2); border-radius:6px; padding:4px 8px; background:var(--field); color:var(--ink); font:inherit; }
      #stage.has-investigation #mapHelp { top:64px; }
      #stage.list-view :is(#map, .zoom, #legend, #mapHelp) { visibility:hidden; }
      .entity-table { position:absolute; inset:64px 8px 8px; z-index:3; background:var(--panel); border:1px solid var(--line); border-radius:8px; overflow:auto; padding:8px; }
      .entity-table table,.workspace-table { border-collapse:collapse; width:100%; text-align:left; font-size:12.5px; }
      .entity-table th,.entity-table td,.workspace-table th,.workspace-table td { padding:7px 8px; border-bottom:1px solid var(--line); vertical-align:top; overflow-wrap:anywhere; }
      .entity-table th,.workspace-table th { color:var(--ink-2); font-weight:600; }
      .entity-table th { position:sticky; top:0; background:var(--panel); }
      .comparison-map { height:clamp(330px, 55vh, 520px); min-width:0; border:1px solid var(--line-2); border-radius:8px; overflow:hidden; }
      .workspace-card { border:1px solid var(--line-2); padding:12px; border-radius:8px; min-width:0; }
      .workspace-card pre { white-space:pre-wrap; overflow-wrap:anywhere; max-height:300px; overflow:auto; }
      .change-added td:first-child { color:var(--ok-text); font-weight:600; }
      .change-removed td:first-child { color:var(--err-text); font-weight:600; }
      .change-changed td:first-child { color:var(--warn-text); font-weight:600; }
      .workspace-dialog button:focus-visible,.entity-table button:focus-visible { outline:2px solid var(--accent); outline-offset:2px; }
      @media(max-width:700px) { .workspace-grid { grid-template-columns:1fr; } .comparison-map { height:330px; } }
    `;
    root.replaceChildren(style, app);
    const $ = s => app.querySelector(s);

    const aborter = new AbortController();
    // Every pending timer, so destroy() can clear them; each forgets itself when it fires.
    const timers = new Set();
    const later = (fn, ms) => { const t = setTimeout(() => { timers.delete(t); fn(); }, ms); timers.add(t); };
    let raf = 0, destroyed = false, es = null, reconnectDelay = 1000;

    const NODE_W = 196, NODE_H = 62, SVGNS = 'http://www.w3.org/2000/svg';
    const prefix = 'teia.' + (opts.storageKey ? opts.storageKey + '.' : '');
    const store = {
      get(k, d) { try { const v = localStorage.getItem(prefix + k); return v == null ? d : JSON.parse(v); } catch { return d; } },
      set(k, v) { try { localStorage.setItem(prefix + k, JSON.stringify(v)); } catch { /* private mode */ } },
    };
    const motionMQ = matchMedia('(prefers-reduced-motion: reduce)');

    const el = (tag, attrs = {}, ...kids) => {
      const e = document.createElement(tag);
      for (const [k, v] of Object.entries(attrs)) {
        if (v == null) continue;
        if (k === 'class') e.className = v; else if (k.startsWith('on')) e.addEventListener(k.slice(2), v); else e.setAttribute(k, v);
      }
      for (const k of kids.flat()) if (k != null) e.append(k);
      return e;
    };
    const sv = (tag, attrs = {}) => {
      const e = document.createElementNS(SVGNS, tag);
      for (const [k, v] of Object.entries(attrs)) if (v != null) e.setAttribute(k, v);
      return e;
    };
    const iconSVG = (paths, cls = '') => {
      const s = sv('svg', { viewBox: '0 0 20 20', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.5', 'stroke-linecap': 'square', 'stroke-linejoin': 'miter', class: cls, 'aria-hidden': 'true' });
      s.innerHTML = paths;
      return s;
    };
    // The map's grammar drawn small, for the legend and the plan line: a solid
    // stroke is seen, a dashed one only declared, "+" seen but not declared,
    // a hatched card a blind spot. Health glyphs match the cards'.
    const GRAMMAR = {
      seen: '<path d="M1 6H21"/>',
      undeclared: '<path d="M1 6H6M16 6H21"/><rect x="6" y="1" width="10" height="10" rx="2"/><path d="M8.5 6H13.5M11 3.5V8.5"/>',
      declared: '<path d="M1 6H21" stroke-dasharray="4 3"/>',
      firsthand: '<rect x="2" y="1.5" width="18" height="9" rx="2"/>',
      blind: '<rect x="2" y="1.5" width="18" height="9" rx="2"/><path class="hatch" d="M2 7L7.5 1.5M2.5 10.5L11.5 1.5M6.5 10.5L15.5 1.5M10.5 10.5L19.5 1.5M14.5 10.5L20 5"/>',
      notraffic: '<rect x="2" y="1.5" width="18" height="9" rx="2" stroke-dasharray="3 2.5"/>',
    };
    const HEALTH_GLYPH = {
      healthy: '<rect x="-5" y="-5" width="10" height="10" rx="2"/>',
      degraded: '<path d="M0,-6 L6,5 L-6,5 Z"/>',
      failing: '<path d="M-5,-3 L-3,-5 L0,-2 L3,-5 L5,-3 L2,0 L5,3 L3,5 L0,2 L-3,5 L-5,3 L-2,0 Z"/>',
    };
    const grammarSVG = kind => {
      const glyph = has(HEALTH_GLYPH, kind);
      const s = sv('svg', glyph ? { viewBox: '-7 -7 14 14', width: '12', height: '12', fill: 'currentColor', class: 'gram', 'aria-hidden': 'true' }
        : { viewBox: '0 0 22 12', width: '22', height: '12', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.5', 'stroke-linecap': 'round', class: 'gram', 'aria-hidden': 'true' });
      s.innerHTML = glyph ? HEALTH_GLYPH[kind] : GRAMMAR[kind];
      return s;
    };
    const css = name => getComputedStyle(host).getPropertyValue(name).trim();
    const timeOf = iso => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    const stored = (k, d) => { const v = store.get(k, null); return v == null ? d : v; };

    // -------------------------------------------------------------------- state
    const state = {
      snap: null, sig: '', pending: null, connected: false, error: '', lastData: 0,
      historyId: '', historyLoading: false, slice: '',
      // cluster is the one asked for ('' is the server's default); clusters
      // lists every cluster the server maps.
      cluster: '', clusters: [],
      investigation: { mode: 'all', focus: '', target: '', depth: 1, direction: 'both' }, display: 'graph', tablePage: 0, tableSort: 'health', operation: { id: '', traceId: '' },
      view: { x: 0, y: 0, k: 1 }, fitted: false, userMoved: false,
      positions: stored('positions', {}),
      selected: null, hover: null, hoverEdge: null,
      states: stored('states', { ok: true, ghost: true, stale: true }),
      signals: new Set(stored('signals', [])),
      query: '',
      groupBy: stored('groupBy', opts.groupBy || ''),
      // A grouping someone chose (stored, a host option, a link) is kept;
      // without one, a map too large to draw whole opens grouped (autoGroup).
      groupChosen: store.get('groupBy', null) !== null || !!opts.groupBy,
      autoGroup: '',
      expanded: new Set(),
      flow: stored('flow', opts.flow === false ? false : !motionMQ.matches),
      live: true,
      visible: true,
      tab: 'steps',
      activity: [],
      legendIcons: false,
      legend: stored('legend', false), // expand on demand without covering the map
      filters: emptyFilters(), // what the map shows: journeys and facets (see scopeOf)
      journeyKey: opts.journeyKey || '', // the label naming journeys; empty: detected
      jkey: JOURNEY_KEY,
      values: stored('values', opts.values !== false), // traffic and latency on every line
    };
    let index = indexSnapshot(sanitize({}));

    // ------------------------------------------------------------------- the map
    const svg = $('#map');
    const stage = $('#stage');
    let viewport, layerFrame, layerEdges, layerPorts, layerMarks, layerReasons, layerParticles, layerLabels, layerNodes;
    const portEls = new Map(); // port id -> { port, g }
    const nodeEls = new Map(), edgeEls = new Map();
    let currentView = null, layoutPos = {}, topologyKey = '';

    const SVG_CSS = `
      .card { fill: var(--card); stroke: var(--line-2); stroke-width: 1; transition: fill .15s, stroke .15s; }
      .node { cursor: pointer; outline: none; }
      .node.nodiff .title { fill: var(--muted); }
      .node.nodiff .icon { opacity: .55; }
      .node:hover .card, .node.sel .card, .node:focus-visible .card { fill: var(--card-hi); stroke: var(--accent); }
      .node.sel .card { stroke-width: 1.5; }
      .node:focus-visible .card { stroke-width: 2; }
      /* The map's grammar: a solid stroke is seen, a dashed one is only declared, a
         hatched card is a blind spot (only its callers report it). Colour is health. */
      .node .blind { fill: url(#teia-blind); pointer-events: none; display: none; }
      .node.vis-partial .blind { display: inline; }
      .blind-line { stroke: var(--ink); stroke-opacity: .13; stroke-width: 1.3; }
      .node.vis-none .card { stroke-dasharray: 5 4; }
      .node.vis-none .title { fill: var(--ink-2); }
      .h-degraded .card { stroke: var(--degraded); }
      .h-failing .card { stroke: var(--failing); stroke-width: 1.5; }
      .node .icon { color: var(--unknown); }
      .h-healthy .icon { color: var(--healthy); } .h-degraded .icon { color: var(--degraded); }
      .h-failing .icon { color: var(--failing); }
      .glyph { display: none; } .h-degraded .glyph.warn, .h-failing .glyph.fail { display: inline; }
      .glyph path { stroke: var(--card); stroke-width: 2; paint-order: stroke; stroke-linejoin: round; }
      .glyph.warn path { fill: var(--degraded); } .glyph.fail path { fill: var(--failing); }
      .kind-group .card { stroke: var(--ink-2); stroke-width: 1.2; }
      .node .title { fill: var(--ink); font-weight: 600; font-size: 13.5px; }
      .compact .node :is(.sub, .alert, .why-tag, .icon, .glyph, .blind),
      .compact :is(.value-tag, .why-tag, .elabel, .frame-stats, .lane-caption) { display: none; }
      .compact .h-degraded .card { stroke: var(--warn-text); stroke-width: 1.5; }
      .compact .h-failing .card { stroke: var(--err-text); stroke-width: 1.5; }
      .dense .node .title { display: none; }
      .dense .node:is(.sel, :hover, :focus-visible) .title { display: block; paint-order: stroke; stroke: var(--bg); stroke-width: 3px; stroke-linejoin: round; }
      .node .sub { fill: var(--muted); font-size: 11px; }
      .node .alert rect { fill: var(--panel-2); stroke: var(--line-2); stroke-width: 1; }
      .node .alert.critical rect { stroke: var(--failing); }
      .node .alert text { fill: var(--ink-2); font-size: 10px; font-weight: 600; text-anchor: middle; }
      .node .alert.critical text { fill: var(--err-text); }
      .node .halo { display: none; }
      .why-tag rect { fill: var(--card); stroke-width: 1; }
      .why-tag.degraded rect { stroke: var(--degraded); } .why-tag.failing rect { stroke: var(--failing); }
      .why-tag text { font-size: 10.5px; font-weight: 600; }
      .why-tag svg { stroke-width: 2.2; }
      .why-tag.degraded text { fill: var(--warn-text); } .why-tag.failing text { fill: var(--err-text); }
      .why-tag.degraded svg { color: var(--warn-text); } .why-tag.failing svg { color: var(--err-text); }
      .why-tag { cursor: pointer; }
      .value-tag rect { fill: var(--bg); fill-opacity: .9; stroke: none; }
      .value-tag text { fill: var(--muted); font-size: 10.5px; font-weight: 500; }
      .value-tag { cursor: pointer; }
      .value-tag:hover text { fill: var(--ink); }
      .far .value-tag { display: none; }
      .frame { fill: color-mix(in srgb, var(--ink) 2.5%, transparent); stroke: var(--line-2); stroke-width: 1; }
      .lane { fill: color-mix(in srgb, var(--ink) 3%, transparent); }
      .lane-caption, .frame-stats { fill: var(--muted); font-size: 11px; }
      .frame-name { fill: var(--ink); font-size: 14px; font-weight: 600; }
      .frame-health.healthy { fill: var(--healthy); } .frame-health.degraded { fill: var(--degraded); }
      .frame-health.failing { fill: var(--failing); } .frame-health.unknown { fill: var(--unknown); }
      .anchor { fill: var(--panel); stroke: var(--muted); stroke-width: 1; }
      .port { cursor: pointer; outline: none; }
      .port rect.chip { fill: var(--panel); stroke: var(--line-2); stroke-width: 1; }
      .port:hover rect.chip, .port:focus-visible rect.chip, .port.sel rect.chip { stroke: var(--accent); }
      .port.degraded rect.chip { stroke: var(--degraded); } .port.failing rect.chip { stroke: var(--failing); }
      .port.sel rect.chip, .port:focus-visible rect.chip { stroke: var(--accent); stroke-width: 1.5; }
      .port text { fill: var(--ink-2); font-size: 11.5px; font-weight: 500; }
      .port text .rate { fill: var(--muted); font-weight: 400; }
      .port svg { color: var(--muted); }
      .port.degraded svg { color: var(--degraded); } .port.failing svg { color: var(--failing); }
      .port-line { fill: none; stroke: var(--muted); stroke-opacity: .55; stroke-width: 1.2; stroke-linejoin: round; }
      .port-line.degraded { stroke: var(--degraded); stroke-opacity: .9; } .port-line.failing { stroke: var(--failing); stroke-opacity: .9; }
      .port-line.stale { stroke-dasharray: 5 4; }
      .node.match .card { stroke: var(--accent); stroke-width: 2; }
      .node.enter { animation: enter .15s ease-out; }
      @keyframes enter { from { opacity: 0; } to { opacity: 1; } }
      .edge path.line { fill: none; stroke-linecap: round; transition: stroke .2s, stroke-width .2s; }
      .edge path.hit { fill: none; stroke: transparent; stroke-width: 14; cursor: pointer; }
      .edge.h-healthy path.line { stroke: color-mix(in srgb, var(--healthy) 55%, var(--edge)); } .edge.h-degraded path.line { stroke: var(--degraded); }
      .edge.h-failing path.line { stroke: var(--failing); } .edge.h-unknown path.line { stroke: var(--edge); }
      .edge path.line { stroke-opacity: .9; }
      .edge.s-stale path.line { stroke-dasharray: 5 4; stroke: var(--stale); stroke-opacity: .9; }
      .edge.diff-added path.line { stroke: var(--healthy); stroke-width: 3; stroke-opacity: 1; }
      .edge.diff-removed path.line { stroke: var(--failing); stroke-width: 3; stroke-opacity: 1; stroke-dasharray: 7 5; }
      .edge.diff-changed path.line { stroke: var(--degraded); stroke-width: 3; stroke-opacity: 1; }
      .edge.nodiff path.line { stroke-opacity: .3; }
      .edge.sel path.line, .edge:hover path.line { stroke-opacity: 1; }
      .edge.sel path.line { stroke: var(--accent); }
      .edge:focus-visible path.line { stroke: var(--accent); stroke-width: 3; stroke-opacity: 1; }
      .arrow.healthy { fill: color-mix(in srgb, var(--healthy) 55%, var(--edge)); } .arrow.degraded { fill: var(--degraded); } .arrow.failing { fill: var(--failing); }
      .arrow.unknown { fill: var(--edge); } .arrow.ghost { fill: var(--stale); } .arrow.accent { fill: var(--accent); }
      .mark { cursor: pointer; }
      .mark rect { fill: var(--card); stroke: var(--ink-2); stroke-width: 1.2; }
      .mark path { stroke: var(--ink); stroke-width: 1.6; stroke-linecap: round; }
      .mark:hover rect { stroke: var(--accent); }
      .elabel rect { fill: var(--panel); stroke: var(--line-2); }
      .elabel text { fill: var(--ink); font-size: 11px; }
      .elabel, .particles { pointer-events: none; }
      .particles circle { fill: var(--particle); }
      .particles circle.err { fill: var(--failing); }
      .particles circle.faint { opacity: .5; }
      .dim { opacity: .35; transition: opacity .15s; }
      .node.dim { opacity: 1; }
      .node.dim .title { fill: var(--muted); }
      .node.dim :is(.icon, .why-tag, .blind) { opacity: .45; }
      .node.dim .card { stroke: var(--line); }
      /* Comparison marks outrank the compact health stroke; selection and focus outrank them. */
      .node.diff-added:not(.sel):not(:focus-visible) .card { stroke: var(--healthy); stroke-width: 2.5; }
      .node.diff-removed:not(.sel):not(:focus-visible) .card { stroke: var(--failing); stroke-width: 2.5; stroke-dasharray: 7 5; }
      .node.diff-changed:not(.sel):not(:focus-visible) .card { stroke: var(--degraded); stroke-width: 2.5; }
      @media (prefers-reduced-motion: reduce) { .node.enter { animation: none; } .dim, .card, .edge path.line { transition: none; } }
    `;

    function initSVG() {
      svg.replaceChildren();
      const defs = sv('defs');
      for (const [id, d] of [['arrow-port', 'M0,1 L9,5 L0,9 z'], ['arrow-port-in', 'M9,1 L0,5 L9,9 z']]) {
        const m = sv('marker', { id, viewBox: '0 0 10 10', refX: id === 'arrow-port' ? '9' : '0', refY: '5', markerWidth: '6', markerHeight: '6', orient: 'auto' });
        m.append(sv('path', { d, class: 'arrow unknown' }));
        defs.append(m);
      }
      for (const h of ['healthy', 'degraded', 'failing', 'unknown', 'ghost', 'accent']) {
        const m = sv('marker', { id: 'arrow-' + h, viewBox: '0 0 10 10', refX: '9', refY: '5', markerWidth: '7', markerHeight: '7', orient: 'auto-start-reverse' });
        m.append(sv('path', { d: 'M0,1 L9,5 L0,9 z', class: 'arrow ' + h }));
        defs.append(m);
      }
      // Blind spots are hatched, like unsurveyed ground on a map.
      const hatch = sv('pattern', { id: 'teia-blind', width: '5', height: '5', patternUnits: 'userSpaceOnUse', patternTransform: 'rotate(45)' });
      hatch.append(sv('path', { d: 'M0,0 V5', class: 'blind-line' }));
      defs.append(hatch);
      const st = sv('style');
      st.textContent = SVG_CSS;
      defs.append(st);
      svg.append(defs);
      viewport = sv('g', { id: 'viewport' });
      layerFrame = sv('g'); layerEdges = sv('g'); layerPorts = sv('g'); layerMarks = sv('g'); layerReasons = sv('g'); layerParticles = sv('g', { class: 'particles' }); layerLabels = sv('g'); layerNodes = sv('g', { role: 'list' });
      viewport.append(layerFrame, layerEdges, layerPorts, layerParticles, layerMarks, layerReasons, layerLabels, layerNodes);
      svg.append(viewport);
    }

    const posOf = id => state.positions[id] || layoutPos[id] || { x: 0, y: 0 };

    function edgeVisible(e) {
      if (!state.states[e.state]) return false;
      if (state.signals.size && !e.signals.some(s => state.signals.has(s))) return false;
      return true;
    }

    // scopeKey names the filters, so each filtered view keeps its own layout.
    const scopeKey = () => {
      const f = state.filters;
      return [[...f.journeys].sort().join(','), ...Object.keys(f.facets).sort().map(k => k + '=' + [...f.facets[k]].sort().join(','))].join('|').toLowerCase();
    };
    // ("@scope:" keeps layouts saved before frames, laid out for stubs, out)
    const posKey = () => 'positions' + (state.cluster ? '@cluster:' + state.cluster : '') + (state.slice ? '@slice:' + state.slice : '') + (filtersActive(state.filters) ? '@scope:' + scopeKey() : '');

    let ports = [];
    function renderMap() {
      const scope = state.scope;
      const scoped = scope ? scopeSnapshot(state.snap, scope) : state.snap;
      state.investigated = investigateSnapshot(scoped, state.investigation);
      const view = buildView(state.investigated.snapshot, index, state);
      currentView = view;
      ports = scope ? journeyPorts(state.snap, scope, { journeyKey: state.jkey, maxPerSide: page ? 8 : 5 }) : [];
      // in a grouped view, a port anchors on the group card
      for (const p of ports) p.anchors = [...new Set(p.anchors.map(id => view.nodes.has(id) ? id : view.groupOf(id)).filter(Boolean))];
      ports = ports.filter(p => p.anchors.length);
      const pin = scope ? scopePins(view, ports) : new Map();
      const key = [...view.nodes.keys()].sort().join('|') + '#' + [...view.edges.keys()].sort().join('|') + '#' + [...pin].sort().join(';');
      if (key !== topologyKey) { layoutPos = layered(view, { pin }); topologyKey = key; }
      if (state.hover && !view.nodes.has(state.hover)) state.hover = null;
      if (state.hoverEdge && !view.edges.has(state.hoverEdge)) state.hoverEdge = null;

      const seen = new Set();
      for (const n of view.nodes.values()) {
        seen.add(n.id);
        let g = nodeEls.get(n.id);
        if (!g) { g = createNode(n); nodeEls.set(n.id, g); layerNodes.append(g); }
        updateNode(g, n);
        const p = posOf(n.id);
        g.setAttribute('transform', `translate(${p.x},${p.y})`);
        g._pos = p;
      }
      for (const [id, g] of nodeEls) if (!seen.has(id)) { g.remove(); nodeEls.delete(id); }

      const eSeen = new Set();
      for (const e of view.edges.values()) {
        eSeen.add(e.id);
        let g = edgeEls.get(e.id);
        if (!g) { g = createEdge(); edgeEls.set(e.id, g); layerEdges.append(g); }
        g._edge = e;
        g.setAttribute('aria-label', `${e.from} to ${e.to}, ${healthText(e)}, ${errorText(e)}${diffOf(e) ? ', ' + DIFF_TEXT[diffOf(e)] : ''}`);
        g.style.display = edgeVisible(e) ? '' : 'none';
        g.setAttribute('class', `edge s-${e.state} h-${e.health}${state.selected?.id === e.id ? ' sel' : ''}${diffClass(e)}`);
        routeEdge(g, posOf(e.from), posOf(e.to), view.edges.has(e.to + '->' + e.from) ? 7 : 0);
      }
      for (const [id, g] of edgeEls) if (!eSeen.has(id)) { g.remove(); edgeEls.delete(id); }

      const empty = $('#empty');
      empty.hidden = view.nodes.size > 0;
      if (!empty.hidden) {
        empty.replaceChildren(el('div', {}, el('p', {}, scope ? 'Nothing matches these filters.' : 'No communication discovered yet.'),
          el('button', { class: 'btn', type: 'button', onclick: () => { if (scope) setFilters(emptyFilters()); else { showTab('sources'); setDrawer(true); } } }, scope ? 'Clear filters' : 'Inspect sources')));
      }

      renderFrame(scope, pin);
      renderMarks();
      renderReasons();
      rebuildParticles();
      applyHighlight();
      renderEdgeLabel();
      if (!state.fitted && view.nodes.size) state.fitted = fit();
      else applyView();
      renderInvestigation();
    }

    // renderFrame draws a scope: the frame with its name, health and what
    // enters it; the lane of its datastores, queues and third parties; and
    // the ports beyond the frame, each line routed around the cards.
    function renderFrame(scope, pin) {
      layerFrame.replaceChildren();
      layerPorts.replaceChildren();
      portEls.clear();
      state.frameBox = null;
      if (!scope || !nodeEls.size) return;
      const pos = {};
      for (const [id, g] of nodeEls) pos[id] = g._pos;
      const lane = [...pin].filter(([, where]) => where === 'last').map(([id]) => id);
      const f = frameLayout(pos, ports, { nodeW: NODE_W, nodeH: NODE_H, lane });
      const fr = f.frame;
      state.frameBox = { ...fr, chips: f.chips };
      layerFrame.append(sv('rect', { class: 'frame', x: fr.x0, y: fr.y0, width: fr.x1 - fr.x0, height: fr.y1 - fr.y0, rx: 10 }));
      if (f.band) {
        layerFrame.append(sv('rect', { class: 'lane', x: f.band.x0, y: fr.y0 + 74, width: f.band.x1 - f.band.x0, height: fr.y1 - fr.y0 - 82, rx: 6 }));
        const cap = sv('text', { class: 'lane-caption', x: (f.band.x0 + f.band.x1) / 2, y: fr.y0 + 91, 'text-anchor': 'middle' });
        cap.textContent = 'Data, queues and third parties';
        layerFrame.append(cap);
      }
      // the header: health, name, what enters it
      const why = journeyWhy(state.snap, scope), st = scopeStats(state.snap, scope);
      layerFrame.append(sv('circle', { class: 'frame-health ' + why.health, cx: fr.x0 + 26, cy: fr.y0 + 27, r: 6 }));
      const name = sv('text', { class: 'frame-name', x: fr.x0 + 42, y: fr.y0 + 32 });
      const f0 = state.filters, single = f0.journeys.length === 1 && !activeFacets(f0).length && !f0.journeys[0].startsWith('from:');
      name.textContent = single ? f0.journeys[0] : scopeName();
      layerFrame.append(name);
      const stats = [...(single ? ['journey'] : []), `${st.parts}${scope.truncated ? '+' : ''} parts`];
      if (st.rate > 0) stats.push(`entry calls ${fmtRate(st.rate)}/s`);
      if (st.p95 > 0) stats.push(latencyText(st, true));
      if (st.rate > 0) stats.push(errorText(st, true));
      const line = sv('text', { class: 'frame-stats num', x: fr.x0 + 42, y: fr.y0 + 50 });
      line.textContent = stats.join(' · ');
      const hint = sv('title');
      hint.textContent = `Entry-edge calls, not unique user transactions. ${errorText(st)}. ${latencyText(st)}. Health includes downstream dependencies; this is not end-to-end journey latency.`;
      line.append(hint);
      layerFrame.append(line);
      // the ports
      for (const c of f.chips) drawPort(c, fr);
    }

    // scopeName says what the filters keep, for the frame and announcements.
    function scopeName() {
      const f = state.filters;
      return [...f.journeys.map(j => filterLabel('journey', j)), ...Object.entries(f.facets).flatMap(([k, vs]) => vs.map(v => filterLabel(k, v)))].join(' · ');
    }

    // drawPort draws one chip beyond the frame and its lines into the scope.
    // A line runs straight when nothing is in its way, else along the top or
    // bottom corridor of the frame, and never across a card.
    function drawPort(c, fr) {
      const p = c.port, side = p.side === 'in' ? 1 : -1;
      const chipEdge = c.x - side * c.w / 2, frameEdge = side > 0 ? fr.x0 : fr.x1;
      const edgeState = p.edges.length && p.edges.every(e => e.state === 'stale') ? ' stale' : '';
      for (const r of c.routes) {
        let pts;
        if (r.mode === 'direct') {
          const mx = (frameEdge + r.from.x) / 2;
          pts = `M${chipEdge},${c.y} L${frameEdge},${c.y} C${mx},${c.y} ${mx},${r.from.y} ${r.from.x},${r.from.y}`;
        } else {
          pts = `M${chipEdge},${c.y} L${frameEdge},${c.y} L${frameEdge + side * 16},${r.y} L${r.from.x},${r.y} L${r.from.x},${r.from.y}`;
        }
        // calls run from the caller: into the scope for callers and tenants, out for exits
        const path = sv('path', { class: `port-line ${p.health}${edgeState}`, d: pts });
        if (p.kind === 'exit') path.setAttribute('marker-start', 'url(#arrow-port-in)'); else path.setAttribute('marker-end', 'url(#arrow-port)');
        layerPorts.append(path);
      }
      if (c.routes.length) layerPorts.append(sv('rect', { class: 'anchor', x: frameEdge - 4, y: c.y - 4, width: 8, height: 8, rx: 2 }));
      const g = sv('g', { class: `port ${p.health}${state.selected?.id === p.id ? ' sel' : ''}`, tabindex: '0', role: 'button', transform: `translate(${c.x - c.w / 2},${c.y - 13})` });
      g.append(sv('rect', { class: 'chip', width: c.w, height: 26, rx: 6 }));
      const first = state.snap.nodes.find(n => n.id === p.of[0]);
      const worst = p.reasons[0];
      const ic = iconSVG(worst ? REASON[worst.cause].icon : p.kind === 'more' ? ICON.group : first ? iconOf(first) : ICON.unknown);
      ic.setAttribute('x', 8); ic.setAttribute('y', 6); ic.setAttribute('width', 14); ic.setAttribute('height', 14);
      const text = sv('text', { x: 27, y: 17 });
      const [who, rateText] = chipText(p).split('  ');
      text.textContent = who;
      if (rateText) {
        const rate = sv('tspan', { class: 'rate', dx: 6 });
        rate.textContent = rateText;
        text.append(rate);
      }
      const tip = sv('title');
      const role = { caller: `calls into ${p.anchors.join(', ')}`, tenant: `also uses ${p.anchors.join(', ')}`, exit: `called by ${p.anchors.join(', ')}`, more: 'outside the view' }[p.kind];
      tip.textContent = `${p.of.join(', ')}: ${role}${worst ? ' · ' + (worst.summary || reasonTag(worst)) : ''}`;
      g.setAttribute('aria-label', tip.textContent);
      g.append(ic, text, tip);
      g.addEventListener('click', ev => { ev.stopPropagation(); select({ type: 'port', id: p.id }); });
      g.addEventListener('keydown', ev => {
        if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); ev.stopPropagation(); select({ type: 'port', id: p.id }); }
      });
      layerPorts.append(g);
      portEls.set(p.id, { port: p, g });
    }

    function createNode(n) {
      const g = sv('g', { class: 'node enter', tabindex: '0', role: 'button' });
      g.dataset.id = n.id;
      const halo = sv('rect', { class: 'halo', x: -NODE_W / 2, y: -NODE_H / 2, width: NODE_W, height: NODE_H, rx: 6, style: 'transform-box: fill-box; transform-origin: center' });
      const card = sv('rect', { class: 'card', x: -NODE_W / 2, y: -NODE_H / 2, width: NODE_W, height: NODE_H, rx: 6 });
      const blind = sv('rect', { class: 'blind', x: -NODE_W / 2, y: -NODE_H / 2, width: NODE_W, height: NODE_H, rx: 6 });
      // The category icon carries the health colour; the card stays neutral until something is wrong.
      const icon = sv('g', { class: 'icon', transform: `translate(${-NODE_W / 2 + 14},-10)` });
      // Health is never colour alone: a triangle for degraded, a cross for failing.
      const warn = sv('g', { class: 'glyph warn', transform: `translate(${-NODE_W / 2 + 33},9)` });
      warn.append(sv('path', { d: 'M0,-6 L6,5 L-6,5 Z' }));
      const fail = sv('g', { class: 'glyph fail', transform: `translate(${-NODE_W / 2 + 33},9)` });
      fail.append(sv('path', { d: 'M-5,-3 L-3,-5 L0,-2 L3,-5 L5,-3 L2,0 L5,3 L3,5 L0,2 L-3,5 L-5,3 L-2,0 Z' }));
      const title = sv('text', { class: 'title', x: -NODE_W / 2 + 46, y: -3 });
      const sub = sv('text', { class: 'sub num', x: -NODE_W / 2 + 46, y: 14 });
      const alert = sv('g', { class: 'alert', transform: `translate(${NODE_W / 2 - 6},${-NODE_H / 2})` });
      // The worst reason sits on the card's top edge, like a tab.
      const why = sv('g', { transform: `translate(${-NODE_W / 2 + 12},${-NODE_H / 2 - 2})` });
      const tip = sv('title');
      g.append(halo, card, blind, icon, warn, fail, title, sub, alert, why, tip);
      g._parts = { icon, title, sub, alert, why, tip };
      g.addEventListener('pointerdown', ev => startNodeDrag(ev, g));
      g.addEventListener('mouseenter', () => { state.hover = g.dataset.id; applyHighlight(); });
      g.addEventListener('mouseleave', () => { state.hover = null; applyHighlight(); });
      g.addEventListener('keydown', ev => {
        if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); ev.stopPropagation(); select({ type: 'node', id: g.dataset.id }); }
      });
      later(() => g.classList.remove('enter'), 600);
      return g;
    }

    // diffOf: a comparison map's mark for a drawn item (opts.diff from
    // compareSnapshots). A group or aggregate takes its members' common mark,
    // or "changed" when they differ.
    function diffOf(item) {
      if (!opts.diff || !item) return '';
      const marks = (item.from ? opts.diff.edges : opts.diff.nodes) || {};
      const keys = item.from ? (item.members || [item]).map(e => e.from + '->' + e.to) : item.group ? item.members.map(m => m.id) : [item.id];
      const found = [...new Set(keys.map(k => marks[k]).filter(Boolean))];
      return !found.length ? '' : found.length === 1 && keys.every(k => marks[k] === found[0]) ? found[0] : 'changed';
    }
    function diffClass(item) {
      if (!opts.diff) return '';
      const mark = diffOf(item);
      return mark ? ' diff-' + mark : ' nodiff';
    }

    function updateNode(g, n) {
      const p = g._parts;
      const sel = state.selected?.id === n.id;
      g.setAttribute('class', `node kind-${n.kind} h-${n.health} vis-${n.visibility}${sel ? ' sel' : ''}${g.classList.contains('enter') ? ' enter' : ''}${diffClass(n)}`);
      const cat = categoryOf(n);
      if (p.cat !== cat) {
        p.icon.replaceChildren(iconSVG(iconOf(n)));
        p.icon.firstChild.setAttribute('width', 20); p.icon.firstChild.setAttribute('height', 20);
        p.cat = cat;
      }
      p.title.textContent = n.title.length > 18 ? n.title.slice(0, 17) + '…' : n.title;
      p.sub.textContent = cardSubtitle(n);
      const fs = state.snap.findings.filter(f => n.group ? n.members.some(m => m.id === f.subject) : f.subject === n.id);
      p.alert.replaceChildren();
      if (fs.length) {
        p.alert.setAttribute('class', 'alert' + (fs.some(f => f.severity === 'critical') ? ' critical' : ''));
        p.alert.append(sv('rect', { x: -8, y: -8, width: 16, height: 16, rx: 3 }));
        const t = sv('text', { y: 3.6 }); t.textContent = fs.length; p.alert.append(t);
      }
      const traffic = n.requestRate > 0 ? `, ${fmtRate(n.requestRate)} requests per second${n.latencyP95Ms > 0 ? `, ${latencyText(n)}` : ''}, ${errorText(n)}` : '';
      drawWhy(p.why, currentView.nodes.size > 200 ? [] : mapReasons(n, currentView.nodes));
      const because = n.reasons.length ? `, because of ${n.reasons.map(r => r.summary || reasonTag(r)).join('; ')}` : '';
      const label = `${n.title}, ${n.group ? 'group' : CATEGORY[cat]?.label || n.kind}${n.tech ? ' (' + techLabel(n) + ')' : ''}, ${healthText(n)}${because}${traffic}, ${n.visibility} visibility${fs.length ? `, ${fs.length} next step${fs.length > 1 ? 's' : ''}` : ''}${diffOf(n) ? ', ' + DIFF_TEXT[diffOf(n)] : ''}`;
      p.tip.textContent = label;
      g.setAttribute('aria-label', label);
    }

    // drawWhy fills g with a pill for the worst reason, or empties it.
    function drawWhy(g, list) {
      g.replaceChildren();
      if (!list.length) return;
      const r = list[0];
      g.setAttribute('class', 'why-tag ' + r.health);
      const text = sv('text', { x: 21, y: 4 });
      const tag = reasonTag(r, list.length - 1);
      text.textContent = tag.length > 30 ? tag.slice(0, 29) + '…' : tag;
      const tip = sv('title');
      tip.textContent = list.map(x => x.summary || reasonTag(x)).join('\n');
      g.append(tip);
      const ic = iconSVG(REASON[r.cause].icon);
      ic.setAttribute('x', 6); ic.setAttribute('y', -6); ic.setAttribute('width', 12); ic.setAttribute('height', 12);
      g.append(text, ic);
      const w = text.getComputedTextLength ? text.getComputedTextLength() : text.textContent.length * 6;
      g.insertBefore(sv('rect', { x: 0, y: -9, width: Math.ceil(w) + 28, height: 18, rx: 3 }), text);
    }

    function createEdge() {
      const g = sv('g', { class: 'edge', tabindex: '0', role: 'button' });
      const line = sv('path', { class: 'line' });
      const hit = sv('path', { class: 'hit' });
      g.append(line, hit);
      g._line = line; g._hit = hit;
      hit.addEventListener('click', ev => { ev.stopPropagation(); select({ type: 'edge', id: g._edge.id }); });
      hit.addEventListener('mouseenter', () => { state.hoverEdge = g._edge.id; renderEdgeLabel(); });
      hit.addEventListener('mouseleave', () => { state.hoverEdge = null; renderEdgeLabel(); });
      g.addEventListener('keydown', ev => {
        if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); ev.stopPropagation(); select({ type: 'edge', id: g._edge.id }); }
      });
      g.addEventListener('focus', () => { state.hoverEdge = g._edge.id; renderEdgeLabel(); });
      g.addEventListener('blur', () => { state.hoverEdge = null; renderEdgeLabel(); });
      return g;
    }

    function edgeD(a, b, offset) {
      const sx = a.x + NODE_W / 2, sy = a.y + offset, tx = b.x - NODE_W / 2 - 4, ty = b.y + offset;
      if (tx < sx + 30) {
        // backwards or stacked: swing below both cards
        const drop = Math.max(NODE_H, Math.abs(sy - ty) / 2) + 48;
        return `M${sx},${sy} C${sx + 140},${sy + drop} ${tx - 140},${ty + drop} ${tx},${ty}`;
      }
      const dx = Math.max(60, (tx - sx) * 0.5);
      return `M${sx},${sy} C${sx + dx},${sy} ${tx - dx},${ty} ${tx},${ty}`;
    }

    function routeEdge(g, a, b, offset) {
      const d = edgeD(a, b, offset);
      const e = g._edge;
      if (g._d !== d) {
        g._line.setAttribute('d', d); g._hit.setAttribute('d', d);
        g._d = d; g._samples = null; g._mid = null;
      }
      const w = e.requestRate > 0 ? 1.3 + Math.min(2.2, Math.log10(1 + e.requestRate * 4) * 1.1) : 1.2;
      g._line.setAttribute('stroke-width', w.toFixed(2));
      g._line.setAttribute('marker-end', `url(#arrow-${e.state === 'stale' ? 'ghost' : e.health})`);
    }

    // samples and mid caches a path's geometry until the path changes.
    function samples(g) {
      if (!g._samples) {
        const len = g._line.getTotalLength();
        const pts = [];
        for (let i = 0; i <= 48; i++) { const p = g._line.getPointAtLength((i / 48) * len); pts.push([p.x, p.y]); }
        g._samples = pts; g._len = len;
      }
      return g._samples;
    }
    function mid(g) {
      if (!g._mid) { const p = g._line.getPointAtLength(g._line.getTotalLength() / 2); g._mid = { x: p.x, y: p.y }; }
      return g._mid;
    }

    function renderMarks() {
      layerMarks.replaceChildren();
      if (!state.snap.drift) return;
      for (const g of edgeEls.values()) {
        if (g.style.display === 'none' || g._edge.state !== 'ghost') continue;
        const p = mid(g);
        const m = sv('g', { class: 'mark', transform: `translate(${p.x},${p.y})` });
        const tt = sv('title'); tt.textContent = 'Seen in traffic, not declared in any flows file';
        m.append(sv('rect', { x: -7, y: -7, width: 14, height: 14, rx: 3 }), sv('path', { d: 'M-3.5,0H3.5M0,-3.5V3.5' }), tt);
        m.addEventListener('click', ev => { ev.stopPropagation(); select({ type: 'edge', id: g._edge.id }); });
        layerMarks.append(m);
      }
    }

    // renderReasons pins each amber or red line's worst reason above its
    // middle, so the map says why without hovering.
    // Each tag sits on its own line, at the first spot from the middle out
    // where it covers no card and no tag placed before it.
    function renderReasons() {
      layerReasons.replaceChildren();
      // Global annotations stop being readable on a dense overview. Every
      // connection retains its keyboard target, hover label and full details.
      if (currentView.nodes.size > 200) return;
      const obstacles = [...nodeEls.values()].map(n => ({
        x0: n._pos.x - NODE_W / 2 - 6, x1: n._pos.x + NODE_W / 2 + 6,
        y0: n._pos.y - NODE_H / 2 - 14, y1: n._pos.y + NODE_H / 2 + 6, // the card and its own tag
      }));
      if (state.frameBox) {
        const fb = state.frameBox;
        obstacles.push({ x0: fb.x0, x1: fb.x1, y0: fb.y0, y1: fb.y0 + 70 }); // the frame's header and its corridor
        for (const c of fb.chips) obstacles.push({ x0: c.x - c.w / 2, x1: c.x + c.w / 2, y0: c.y - 13, y1: c.y + 13 });
      }
      const shown = [...edgeEls.values()].filter(g => g.style.display !== 'none');
      // The "+" of a line seen but not declared keeps the middle of its line.
      if (state.snap.drift) for (const g of shown) if (g._edge.state === 'ghost') { const p = mid(g); obstacles.push({ x0: p.x - 10, x1: p.x + 10, y0: p.y - 10, y1: p.y + 10 }); }
      const spotsOf = g => { const pts = samples(g); return [24, 20, 28, 16, 32, 12, 36, 8, 40, 5, 43].map(i => ({ x: pts[i][0], y: pts[i][1] })); };
      const pin = (tag, g, w, at) => {
        tag.setAttribute('transform', `translate(${at.x - w / 2},${at.y})`);
        tag.addEventListener('click', ev => { ev.stopPropagation(); select({ type: 'edge', id: g._edge.id }); });
      };
      // Reasons first: they matter most and take the best spots.
      for (const g of shown) {
        const e = g._edge;
        const shownReasons = mapReasons(e, currentView.nodes);
        if (!shownReasons.length) continue;
        const tag = sv('g');
        tag._edge = e;
        layerReasons.append(tag);
        drawWhy(tag, shownReasons);
        const w = Number(tag.querySelector('rect')?.getAttribute('width')) || 80;
        const at = placeTag(spotsOf(g), w, 18, obstacles);
        obstacles.push(at.box);
        pin(tag, g, w, at);
      }
      // Then the quiet values, only where they fit: a value never covers a
      // card or a reason, it is left out instead.
      if (!state.values) return;
      for (const g of shown) {
        const e = g._edge;
        const text = lineValue(e);
        if (e.reasons.length || !text) continue;
        const tag = sv('g', { class: 'value-tag' });
        tag._edge = e;
        const label = sv('text', { x: 7, y: 3.5 });
        label.textContent = text;
        tag.append(label);
        layerReasons.append(tag);
        const w = Math.ceil(label.getComputedTextLength ? label.getComputedTextLength() : text.length * 5.6) + 14;
        tag.insertBefore(sv('rect', { x: 0, y: -8, width: w, height: 16, rx: 3 }), label);
        const at = placeTag(spotsOf(g), w, 16, obstacles, { strict: true });
        if (!at) { tag.remove(); continue; }
        obstacles.push(at.box);
        pin(tag, g, w, at);
      }
    }

    // lineValue is what a line says when nothing is wrong: its traffic and
    // p95, the two numbers people look for on an arrow.
    function lineValue(e) {
      const parts = [];
      if (e.requestRate > 0) parts.push(`${fmtRate(e.requestRate)}/s`);
      if (e.latencyP95Ms > 0) parts.push((e.latencyApproximate ? '≈ ' : '') + fmtMs(e.latencyP95Ms));
      return parts.join(' · ');
    }

    function renderEdgeLabel() {
      layerLabels.replaceChildren();
      const id = state.hoverEdge || (state.selected?.type === 'edge' ? state.selected.id : null);
      const g = id && edgeEls.get(id);
      if (!g || g.style.display === 'none') return;
      const e = g._edge;
      const parts = [e.protocol || (e.signals[0] || 'tcp')];
      if (e.requestRate > 0) parts.push(`${fmtRate(e.requestRate)} req/s`);
      if (e.latencyP95Ms > 0) parts.push(latencyText(e, true));
      if (e.requestRate > 0) parts.push(errorText(e));
      if (state.snap.drift && e.state !== 'ok') parts.push(STATE_LABEL[e.state]);
      const p = mid(g);
      const lbl = sv('g', { class: 'elabel', transform: `translate(${p.x},${p.y - 18})` });
      const text = sv('text', { 'text-anchor': 'middle', y: 4 }); text.textContent = parts.join(' · ');
      lbl.append(text);
      layerLabels.append(lbl);
      const bb = text.getBBox();
      lbl.insertBefore(sv('rect', { x: bb.x - 8, y: bb.y - 4, width: bb.width + 16, height: bb.height + 8, rx: 4 }), text);
    }

    // ---------------------------------------------------------------- particles
    let particles = [];
    function rebuildParticles() {
      const old = new Map(particles.map(p => [p.key, p]));
      particles = [];
      layerParticles.replaceChildren();
      if (!state.flow) return;
      const visible = [...edgeEls.values()].filter(g => g.style.display !== 'none');
      const budget = particleBudget(visible.map(g => g._edge));
      for (const g of visible) {
        const e = g._edge, count = budget.get(e.id) || 0;
        if (!count) continue;
        const pts = samples(g);
        const speed = (e.requestRate > 0 ? 70 + 45 * Math.log10(1 + e.requestRate * 5) : 38) / (g._len || 1);
        for (let i = 0; i < count; i++) {
          const key = e.id + '#' + i, prev = old.get(key);
          const c = sv('circle', { r: e.requestRate > 0 ? 2.6 : 2.1 });
          layerParticles.append(c);
          const p = { key, edge: e, pts, speed, t: prev ? prev.t : i / count, el: c, err: prev ? prev.err : false };
          paintParticle(p);
          particles.push(p);
        }
      }
      placeParticles();
      wake();
    }
    function paintParticle(p) {
      p.el.setAttribute('class', (p.err ? 'err' : '') + (p.edge.requestRate > 0 ? '' : ' faint'));
    }
    function placeParticles() {
      for (const p of particles) {
        const f = p.t * 48, i = Math.min(47, Math.floor(f)), r = f - i;
        const a = p.pts[i], b = p.pts[i + 1];
        p.el.setAttribute('cx', (a[0] + (b[0] - a[0]) * r).toFixed(1));
        p.el.setAttribute('cy', (a[1] + (b[1] - a[1]) * r).toFixed(1));
      }
    }
    // The loop only runs while there is something to animate on screen.
    const animating = () => !destroyed && state.flow && state.visible && particles.length > 0 && !document.hidden;
    let last = 0;
    function frame(now) {
      raf = 0;
      if (!animating()) return;
      const dt = last ? Math.min(0.1, (now - last) / 1000) : 0;
      last = now;
      for (const p of particles) {
        p.t += p.speed * dt;
        if (p.t >= 1) {
          p.t -= 1;
          // each lap, a share of the dots turns red, in proportion to errors
          const errLap = Math.random() < Math.min(0.9, (p.edge.errorRate || 0) * errorCoverage(p.edge) * 4);
          if (errLap !== p.err) { p.err = errLap; paintParticle(p); }
        }
      }
      placeParticles();
      raf = requestAnimationFrame(frame);
    }
    function wake() { if (!raf && animating()) { last = 0; raf = requestAnimationFrame(frame); } }

    // ------------------------------------------------------------- interaction
    function applyView() {
      const { x, y, k } = state.view;
      viewport.setAttribute('transform', `translate(${x},${y}) scale(${k})`);
      const compactView = k < 0.85;
      const denseView = k < 0.12 && nodeEls.size > 200;
      viewport.classList.toggle('compact', compactView);
      viewport.classList.toggle('dense', denseView);
      $('#mapHelp').hidden = !(compactView || state.autoGroup) || !nodeEls.size;
      $('#mapHelp span').textContent = state.autoGroup ? `Grouped by ${state.autoGroup}: ${state.snap.nodes.length} components are too many to draw one by one. Open a group, or choose No grouping.`
        : denseView ? `${nodeEls.size} components · ${edgeEls.size} connections. Group, search or focus to read names.` : 'Overview · select a component for its evidence.';
      for (const [id, g] of nodeEls) {
        const n = currentView.nodes.get(id), title = g._parts.title;
        const mark = DIFF_MARK[diffOf(n)] || '';
        const prefix = mark + (!compactView ? '' : n.health === 'failing' ? '× ' : n.health === 'degraded' ? '▲ ' : '');
        const max = compactView ? Math.max(3, Math.floor((NODE_W * k - 12) / 7) - prefix.length) : 17 - mark.length;
        const name = denseView ? n.title : n.title.length > max ? n.title.slice(0, max - 1) + '…' : n.title;
        title.textContent = prefix + name;
        title.setAttribute('x', compactView ? 0 : -NODE_W / 2 + 60);
        title.setAttribute('y', compactView ? 4 / k : -3);
        title.setAttribute('text-anchor', compactView ? 'middle' : 'start');
        title.style.fontSize = compactView ? `${12 / k}px` : '14px';
      }
      layerReasons?.classList.toggle('far', k < 0.55); // values unreadable this small
      stage.style.setProperty('--grid-x', x + 'px');
      stage.style.setProperty('--grid-y', y + 'px');
      stage.style.setProperty('--grid-size', 24 * k + 'px');
    }

    // fit returns false when there is nothing to fit or no room to fit it in.
    function fit() {
      if (!currentView || !nodeEls.size) return false;
      const r = stage.getBoundingClientRect();
      if (r.width < 40 || r.height < 40) return false;
      let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
      for (const g of nodeEls.values()) {
        const p = g._pos;
        x0 = Math.min(x0, p.x - NODE_W / 2); x1 = Math.max(x1, p.x + NODE_W / 2);
        y0 = Math.min(y0, p.y - NODE_H / 2); y1 = Math.max(y1, p.y + NODE_H / 2 + 12);
      }
      if (state.frameBox) {
        const fb = state.frameBox;
        x0 = Math.min(x0, fb.x0, ...fb.chips.map(c => c.x - c.w / 2)); x1 = Math.max(x1, fb.x1, ...fb.chips.map(c => c.x + c.w / 2));
        y0 = Math.min(y0, fb.y0, ...fb.chips.map(c => c.y - 13)); y1 = Math.max(y1, fb.y1, ...fb.chips.map(c => c.y + 13));
      }
      // Reserve space for map controls instead of covering the last column.
      const left = 16, right = 64, bottom = 58;
      let top = 24;
      let k = Math.max(0.05, Math.min(state.frameBox ? 1.1 : 1.25, (r.width - left - right) / (x1 - x0), (r.height - top - bottom) / (y1 - y0)));
      if (k < 0.85) { top = 48; k = Math.max(0.05, Math.min(k, (r.height - top - bottom) / (y1 - y0))); }
      state.view = { k, x: left + (r.width - left - right - (x1 - x0) * k) / 2 - x0 * k, y: top + (r.height - top - bottom - (y1 - y0) * k) / 2 - y0 * k };
      state.userMoved = false;
      applyView();
      return true;
    }

    function zoomAt(factor, cx, cy) {
      const v = state.view;
      const k = Math.max(0.05, Math.min(2.6, v.k * factor));
      const f = k / v.k;
      state.view = { k, x: cx - (cx - v.x) * f, y: cy - (cy - v.y) * f };
      state.userMoved = true;
      applyView();
    }

    // Inside Grafana the wheel scrolls the dashboard; zoom needs Ctrl or Cmd.
    function onWheel(ev) {
      if (!page && !ev.ctrlKey && !ev.metaKey) return;
      ev.preventDefault();
      const unit = ev.deltaMode === 1 ? 16 : ev.deltaMode === 2 ? 400 : 1;
      const r = svg.getBoundingClientRect();
      zoomAt(Math.exp(-ev.deltaY * unit * 0.0015), ev.clientX - r.left, ev.clientY - r.top);
    }

    let drag = null, lastClick = { id: null, at: 0 };
    function onPointerDown(ev) {
      if (ev.target.closest('.node, .edge, .mark, .port, .why-tag, .value-tag')) return;
      drag = { type: 'pan', sx: ev.clientX, sy: ev.clientY, vx: state.view.x, vy: state.view.y, moved: false };
      svg.classList.add('panning');
      svg.setPointerCapture(ev.pointerId);
    }
    function startNodeDrag(ev, g) {
      ev.stopPropagation();
      const p = g._pos;
      drag = { type: 'node', g, id: g.dataset.id, sx: ev.clientX, sy: ev.clientY, px: p.x, py: p.y, moved: false };
      svg.setPointerCapture(ev.pointerId);
    }
    function onPointerMove(ev) {
      if (!drag) return;
      const dx = ev.clientX - drag.sx, dy = ev.clientY - drag.sy;
      if (Math.abs(dx) + Math.abs(dy) > 3) drag.moved = true;
      if (drag.type === 'pan') {
        state.view.x = drag.vx + dx; state.view.y = drag.vy + dy; state.userMoved = true; applyView();
      } else if (drag.moved) {
        const p = { x: drag.px + dx / state.view.k, y: drag.py + dy / state.view.k };
        state.positions[drag.id] = p;
        drag.g._pos = p;
        drag.g.setAttribute('transform', `translate(${p.x},${p.y})`);
        rerouteFor(drag.id);
      }
    }
    function endDrag(commit) {
      if (!drag) return;
      svg.classList.remove('panning');
      const d = drag;
      drag = null;
      if (d.type === 'node') {
        if (d.moved) { store.set(posKey(), state.positions); renderMarks(); renderReasons(); rebuildParticles(); return; }
        if (!commit) return;
        // a second click on the same card within 350 ms expands or collapses its group
        const now = performance.now();
        if (lastClick.id === d.id && now - lastClick.at < 350) { lastClick = { id: null, at: 0 }; toggleGroup(d.id); return; }
        lastClick = { id: d.id, at: now };
        select({ type: 'node', id: d.id });
      } else if (!d.moved && commit) select(null);
    }

    function rerouteFor(id) {
      for (const g of edgeEls.values()) {
        const e = g._edge;
        if (e.from === id || e.to === id) routeEdge(g, posOf(e.from), posOf(e.to), currentView.edges.has(e.to + '->' + e.from) ? 7 : 0);
      }
      layerMarks.replaceChildren();
      layerReasons.replaceChildren();
      layerParticles.replaceChildren();
      particles = [];
      renderEdgeLabel();
    }

    function neighborhood(id) {
      const ids = new Set([id]), edges = new Set();
      for (const g of edgeEls.values()) {
        const e = g._edge;
        if (e.from === id || e.to === id) { ids.add(e.from); ids.add(e.to); edges.add(e.id); }
      }
      return { ids, edges };
    }
    function applyHighlight() {
      if (!currentView) return;
      const edge = state.selected?.type === 'edge' ? currentView.edges.get(state.selected.id) : null;
      const focus = state.hover || (state.selected?.type === 'node' ? state.selected.id : null);
      const match = matcher(state.query);
      const nb = edge ? { ids: new Set([edge.from, edge.to]), edges: new Set([edge.id]) }
        : focus && currentView.nodes.has(focus) ? neighborhood(focus) : null;
      for (const [id, g] of nodeEls) {
        const n = currentView.nodes.get(id);
        const hit = !!(match && match(n));
        g.classList.toggle('dim', !!((nb && !nb.ids.has(id)) || (match && !hit)));
        g.classList.toggle('match', hit);
      }
      for (const [id, g] of edgeEls) g.classList.toggle('dim', !!(nb && !nb.edges.has(id)));
      for (const tag of layerReasons.children) tag.classList.toggle('dim', !!(nb && !nb.edges.has(tag._edge.id)));
      layerParticles.classList.toggle('dim', !!nb);
    }

    function setDrawer(open) {
      const aside = $('aside');
      aside.classList.toggle('open', open);
      aside.inert = !open;
      app.classList.toggle('drawer-closed', !open);
      $('#stepsBtn').setAttribute('aria-expanded', String(open));
      if (open) revealSelection();
    }

    function revealSelection() {
      const aside = $('aside');
      if (!aside.classList.contains('open')) return;
      const selected = state.selected;
      const edge = selected?.type === 'edge' ? currentView?.edges.get(selected.id) : null;
      const elements = edge ? [nodeEls.get(edge.from), nodeEls.get(edge.to)]
        : [selected?.type === 'node' ? nodeEls.get(selected.id)
          : selected?.type === 'port' ? portEls.get(selected.id)?.g : null];
      const boxes = elements.filter(Boolean).map(g => g.getBoundingClientRect());
      if (!boxes.length) return;
      const box = { left: Math.min(...boxes.map(b => b.left)), right: Math.max(...boxes.map(b => b.right)) };
      const r = stage.getBoundingClientRect();
      const overlay = getComputedStyle(aside).position === 'absolute' ? aside.offsetWidth : 0;
      const right = r.right - overlay - 16, left = r.left + 16;
      if (right <= left || box.right - box.left > right - left) return;
      const dx = box.right > right ? right - box.right : box.left < left ? left - box.left : 0;
      if (dx) { state.view.x += dx; applyView(); }
    }

    function select(sel) {
      const changed = state.selected?.type !== sel?.type || state.selected?.id !== sel?.id;
      state.selected = sel;
      syncSharedURL();
      for (const [id, g] of nodeEls) g.classList.toggle('sel', sel?.type === 'node' && sel.id === id);
      for (const [id, g] of edgeEls) g.classList.toggle('sel', sel?.type === 'edge' && sel.id === id);
      for (const [id, { g }] of portEls) g.classList.toggle('sel', sel?.type === 'port' && sel.id === id);
      applyHighlight();
      renderEdgeLabel();
      renderDetails();
      renderSteps();
      if (sel) { showTab('details'); setDrawer(true); }
      if (changed) $('#pane-details').scrollTop = 0;
    }

    function focusNode(id) {
      const g = nodeEls.get(id);
      if (!g) return;
      const r = stage.getBoundingClientRect();
      const k = Math.max(state.view.k, 0.9);
      state.view = { k, x: r.width / 2 - g._pos.x * k, y: r.height / 2 - g._pos.y * k };
      state.userMoved = true;
      applyView();
      select({ type: 'node', id });
    }

    function focusSelection() {
      if ((!state.selected || state.selected.type === 'port') && currentView?.nodes.size) {
        const node = [...currentView.nodes.values()].sort((a, b) => HEALTH_RANK[b.health] - HEALTH_RANK[a.health] || a.id.localeCompare(b.id))[0];
        select({ type: 'node', id: node.id });
      }
      const sel = state.selected;
      const edge = sel?.type === 'edge' ? currentView.edges.get(sel.id) : null;
      const ids = edge ? [edge.from, edge.to] : sel?.type === 'node' ? [sel.id] : [];
      const positions = ids.map(id => nodeEls.get(id)?._pos).filter(Boolean);
      setDrawer(false);
      const r = stage.getBoundingClientRect();
      if (positions.length) {
        const x = positions.reduce((a, p) => a + p.x, 0) / positions.length;
        const y = positions.reduce((a, p) => a + p.y, 0) / positions.length;
        const k = Math.max(0.9, state.view.k);
        state.view = { k, x: (r.width - 48) / 2 - x * k, y: r.height / 2 - y * k };
        state.userMoved = true; applyView();
        (edge ? edgeEls.get(sel.id) : nodeEls.get(sel?.id))?.focus({ preventScroll: true });
      } else zoomAt(Math.max(1, 0.9 / state.view.k), r.width / 2, r.height / 2);
    }

    function toggleGroup(id) {
      const node = currentView.nodes.get(id);
      const representative = node?.group ? node.members[0]?.id : id;
      if (node?.group) state.expanded.add(node.expansionKey);
      else {
        const path = groupingPath(index.nodes.get(id), state.groupBy);
        const last = [...path].reverse().find(g => state.expanded.has(g.expansionKey));
        if (last) state.expanded.delete(last.expansionKey); else return;
      }
      renderMap();
      locate(representative, { move: false }); fit();
    }

    // --------------------------------------------------------------- top bar
    function renderRings() {
      if (state.operation.id) { $('#rings').replaceChildren(el('span', { class: 'pill' }, 'Sample coverage unknown')); return; }
      // The plan line: where the declarations and the traffic agree, read like
      // a diff, then teia's own blind spots. Each count shows only those.
      const internal = state.snap.nodes.filter(n => n.kind !== 'external');
      const blind = internal.filter(n => n.visibility !== 'full').length;
      const item = (kind, n, label, hint, pressed, onclick) => el('button', { type: 'button', class: 'ring', title: hint, 'aria-pressed': String(pressed), onclick },
        grammarSVG(kind), el('b', { class: 'num' }, String(n)), el('span', {}, label));
      const items = [];
      if (state.snap.drift) {
        const count = st => state.snap.edges.filter(e => e.state === st).length;
        const hint = { ok: 'A flows file declares them and traffic confirms them', ghost: 'Seen in traffic; no flows file declares them', stale: 'A flows file declares them; no traffic showed them while the caller was visible' };
        for (const [st, kind] of [['ok', 'seen'], ['ghost', 'undeclared'], ['stale', 'declared']]) {
          items.push(item(kind, count(st), STATE_LABEL[st], `${hint[st]}. Show only these communications`, soloEdge(st), () => soloEdgeState(st)));
        }
      }
      items.push(item('blind', blind, blind === 1 ? 'blind spot' : 'blind spots',
        `${blind} of ${internal.length} internal components are not seen first-hand: only their callers report them, or nothing does. Show only these`, blindFilter(), toggleBlindSpots));
      $('#rings').replaceChildren(...items);
    }

    function renderLive() {
      $('#timeContext').textContent = temporalText(state.snap, opts);
      $('#timeContext').title = opts.requestedRange ? `Dashboard requests ${opts.requestedRange.from} – ${opts.requestedRange.to}. Collection time is not the age of the original measurement.` : 'Collection time is not the age of the original measurement. A source lookback is its query window.';
      const l = $('#live');
      if (opts.comparison) { l.className = 'live paused'; l.querySelector('span').textContent = 'frozen capture'; return; }
      if (state.error) { l.className = 'live error'; l.querySelector('span').textContent = state.error; return; }
      if (state.snap?.timeMode === 'historical') { l.className = 'live paused'; l.querySelector('span').textContent = 'historical · stored snapshot'; return; }
      l.className = 'live ' + (!state.live ? 'paused' : state.connected ? 'on' : '');
      const since = opts.api && state.snap ? new Date(state.snap.generatedAt).getTime() : state.lastData;
      const ago = since ? Math.max(0, Math.round((Date.now() - since) / 1000)) : 0;
      l.querySelector('span').textContent = !state.live ? 'paused'
        : !state.connected ? (opts.api ? 'reconnecting…' : 'waiting for data…')
          : opts.api ? `live · round ${state.snap?.round ?? '–'} · ${ago}s ago` : `received ${ago}s ago`;
    }

    function renderLegend() {
      const box = $('#legend');
      box.classList.toggle('closed', !state.legend);
      const setLegend = open => { state.legend = open; store.set('legend', open); renderLegend(); $(open ? '#legend .close-legend' : '#legend .open-legend')?.focus(); };
      if (!state.legend) {
        box.replaceChildren(el('button', { type: 'button', class: 'open-legend', 'aria-expanded': 'false', title: 'What the colours, lines and tags mean', onclick: () => setLegend(true) },
          iconSVG(UI_ICON.info), 'Legend'));
        return;
      }
      const lg = (kind, label) => el('div', { class: 'lg ' + kind }, grammarSVG(kind), label);
      const parts = [
        el('h4', {}, 'Health'), el('h4', {}, 'Communication'), el('h4', {}, 'Visibility'),
        lg('healthy', HEALTH_LABEL.healthy), lg('seen', STATE_LABEL.ok), lg('firsthand', VIS_LABEL.full),
        lg('degraded', HEALTH_LABEL.degraded), lg('undeclared', STATE_LABEL.ghost), lg('blind', 'blind spot: only its callers report it'),
        lg('failing', HEALTH_LABEL.failing), lg('declared', STATE_LABEL.stale), lg('notraffic', VIS_LABEL.none),
        el('h4', { class: 'span3' }, 'Why: the tag on a card or line'),
        el('div', { class: 'reasons' }, ...Object.values(REASON).map(r => el('span', { class: 'lg' }, iconSVG(r.icon), r.label))),
      ];
      if (state.legendIcons) {
        parts.push(el('h4', { class: 'span3' }, 'Components'));
        for (const [id, c] of Object.entries(CATEGORY)) {
          const examples = Object.values(TECH).filter(t => t.cat === id && !['Microservice', 'External', 'Frontend', 'Job', 'Function'].includes(t.label)).map(t => t.label).slice(0, 3).join(', ');
          parts.push(el('div', { class: 'lg icon', title: examples }, iconSVG(c.icon), c.label));
        }
      }
      parts.push(el('button', { type: 'button', class: 'legend-toggle', 'aria-expanded': String(state.legendIcons), onclick: () => { state.legendIcons = !state.legendIcons; renderLegend(); } },
        state.legendIcons ? 'Hide icon library' : 'Show icon library'));
      parts.push(el('button', { type: 'button', class: 'close-legend', 'aria-label': 'Hide the legend', 'aria-expanded': 'true', title: 'Hide the legend', onclick: () => setLegend(false) }, iconSVG(UI_ICON.x)));
      box.replaceChildren(...parts);
    }

    // setFilters changes what the map shows. Each filtered view keeps its
    // own layout; on teia's page the filters go in the address bar, so the
    // view can be shared; screen readers hear what is shown.
    function setFilters(next) {
      next = { journeys: [...new Set(next.journeys || [])], facets: Object.fromEntries(Object.entries(next.facets || {}).filter(([, v]) => v.length)) };
      state.filters = next;
      state.positions = stored(posKey(), {});
      topologyKey = '';
      state.fitted = false;
      if (page && typeof history !== 'undefined' && typeof location !== 'undefined') {
        const url = new URL(location.href);
        const { journey, filter } = filtersToParams(next);
        for (const [k, v] of [['journey', journey], ['filter', filter]]) { if (v) url.searchParams.set(k, v); else url.searchParams.delete(k); }
        history.replaceState(null, '', url);
      }
      if (!state.snap) return;
      renderAll();
      if (state.selected && state.scope && !state.scope.roles.has(state.selected.id) && !String(state.selected.id).startsWith('port:')) select(null);
      state.fitted = fit();
      if (filterPanelOpen()) renderFilterPanel();
      const sc = state.scope;
      $('#announce').textContent = !sc ? 'Whole map' : `${scopeName()}: ${sc.roles.size} components${headlineText(sc)}`;
    }
    // setJourney shows one journey (the Grafana option and older links).
    const setJourney = spec => setFilters({ journeys: spec ? [String(spec).trim()] : [], facets: state.filters.facets });

    const toggle = (list, v) => list.includes(v) ? list.filter(x => x !== v) : [...list, v];
    function toggleFacet(key, value) {
      const f = state.filters;
      if (key === 'journey') setFilters({ ...f, journeys: toggle(f.journeys, value) });
      else setFilters({ ...f, facets: { ...f.facets, [key]: toggle(f.facets[key] || [], value) } });
    }

    // headlineText says why a scope is amber or red.
    function headlineText(sc) {
      const why = journeyWhy(state.snap, sc);
      return why.reason ? `, ${why.health} because of ${why.subject.replace('->', ' → ')}: ${why.reason.summary || reasonTag(why.reason)}` : '';
    }

    // renderScope draws the bar under the header: the Filter button, the
    // filters in use as chips (or the journeys, one click away, with their
    // health), and why the filtered view is amber or red.
    function renderScope() {
      const bar = $('#scope');
      const f = state.filters;
      const edgeFilters = ['ok', 'ghost', 'stale'].filter(st => !state.states[st]).length + state.signals.size;
      const count = f.journeys.length + activeFacets(f).reduce((a, [, v]) => a + v.length, 0) + edgeFilters;
      const parts = [el('button', {
        type: 'button', class: 'btn filter-btn', id: 'filterBtn', 'aria-expanded': String(filterPanelOpen()), 'aria-controls': 'filterPanel',
        title: 'Filter by journey, namespace, team, health, component… (J)', onclick: ev => { ev.stopPropagation(); setFilterPanel(!filterPanelOpen()); },
      }, iconSVG(UI_ICON.filter), 'Filter', count ? el('span', { class: 'count' }, String(count)) : null)];
      const chip = (label, onRemove) => el('span', { class: 'fchip' }, label, el('button', { type: 'button', 'aria-label': 'Remove ' + label, title: 'Remove', onclick: onRemove }, iconSVG(UI_ICON.x)));
      for (const j of f.journeys) parts.push(chip(filterLabel('journey', j), () => toggleFacet('journey', j)));
      for (const [k, vs] of activeFacets(f)) for (const v of vs) parts.push(chip(filterLabel(k, v), () => toggleFacet(k, v)));
      for (const st of ['ok', 'ghost', 'stale']) if (!state.states[st]) parts.push(chip(`without ${STATE_LABEL[st]}`, () => toggleEdgeState(st)));
      for (const sg of state.signals) parts.push(chip(`seen by ${SIGNAL[sg]?.label.toLowerCase() || sg}`, () => toggleSignal(sg)));
      if (count) {
        parts.push(el('button', { type: 'button', class: 'clear', onclick: () => { state.states = { ok: true, ghost: true, stale: true }; state.signals.clear(); store.set('states', state.states); store.set('signals', []); setFilters(emptyFilters()); } }, 'Clear all'));
      } else {
        const names = journeyNames(state.snap, state.jkey);
        if (names.length) {
          parts.push(el('span', { class: 'caption' }, 'Journeys'));
          for (const name of names.slice(0, 8)) {
            const health = journeyWhy(state.snap, journeyOf(state.snap, name, { key: state.jkey })).health;
            parts.push(el('button', { type: 'button', class: 'jtab', title: `Show only the journey ${name}`, onclick: () => setJourney(name) },
              el('i', { class: 'dot', style: `background:${css('--' + health)}`, 'aria-hidden': 'true' }), name,
              health === 'failing' || health === 'degraded' ? el('span', { class: 'sr-only' }, ` (${health})`) : null));
          }
          if (names.length > 8) parts.push(el('span', { class: 'caption' }, `+${names.length - 8} in Filter`));
        }
      }
      parts.push(...sliceBar());
      if (state.scope) {
        const sc = state.scope, why = journeyWhy(state.snap, sc);
        const line = el('span', { class: 'headline ' + why.health }, el('span', {}, `${sc.roles.size}${sc.truncated ? '+' : ''} components`));
        if (why.reason) {
          line.append(iconSVG(REASON[why.reason.cause].icon), el('b', {}, why.subject.replace('->', ' → ')), reasonTag(why.reason, why.more),
            el('button', { type: 'button', class: 'btn', title: 'Show it on the map', onclick: () => locate(why.subject) }, 'go to'));
        } else if (why.health === 'healthy') {
          line.append(' · every signal within its limits');
        }
        parts.push(line);
      }
      bar.replaceChildren(...parts);
    }

    // sliceBar offers the business dimension values seen on calls, and says
    // what a slice in use shows: only those calls, with their own metrics.
    function sliceBar() {
      const sl = state.snap?.slice, dims = state.snap?.dimensions || {};
      // teia's API slices directly; a host that feeds frames (a Grafana panel)
      // picks through its own controls, or only labels the slice it was given.
      const pickable = !!opts.api || typeof opts.onSliceChange === 'function';
      if (state.slice) {
        const label = sliceLabel(state.slice);
        const out = [el('span', { class: 'fchip slice' }, 'Calls of ' + label,
          pickable ? el('button', { type: 'button', 'aria-label': 'Show every call', title: 'Show every call', onclick: () => setSlice('') }, iconSVG(UI_ICON.x)) : null)];
        if (sl && !sl.matched) out.push(el('span', { class: 'caption slice-note' }, 'no call carried this selection'));
        else if (sl) out.push(el('span', { class: 'caption slice-note', title: 'Rates, errors and latencies come only from these calls. Node readings from exporters stay node-wide.' }, 'metrics of these calls only'));
        if (sl?.excluded.length) out.push(el('span', { class: 'caption slice-note', title: 'These sources cannot split their connections by the selected dimensions, so their connections are left out of the slice.' }, 'without ' + sl.excluded.join(', ')));
        if (!pickable) return out;
        const others = [];
        for (const [k, vs] of Object.entries(dims)) {
          const current = Object.fromEntries(state.slice.split(',').map(p => p.split(':')))[k];
          if (current === undefined) continue;
          for (const v of vs.slice(0, 6)) if (v !== current) others.push(el('button', { type: 'button', class: 'jtab', title: `Only the calls with ${k} = ${v}`, onclick: () => setSlice(state.slice.split(',').map(p => p.startsWith(k + ':') ? k + ':' + v : p).join(',')) }, `${k}: ${v}`));
        }
        if (others.length) out.push(el('span', { class: 'slice-pick' }, ...others));
        return out;
      }
      const entries = Object.entries(dims);
      if (!entries.length || !pickable) return [];
      const out = [el('span', { class: 'caption', title: 'Show only the calls whose own telemetry carried this value, with their own metrics' }, 'Calls of')];
      for (const [k, vs] of entries) for (const v of vs.slice(0, 6)) {
        out.push(el('button', { type: 'button', class: 'jtab', title: `Only the calls with ${k} = ${v}`, onclick: () => setSlice(k + ':' + v) }, `${k}: ${v}`));
      }
      return [el('span', { class: 'slice-pick' }, ...out)];
    }

    // ----------------------------------------------------- the filter panel
    const filterPanelOpen = () => !$('#filterPanel').hidden;
    let panelQuery = '', panelAdvanced = false;
    function setFilterPanel(open) {
      const panel = $('#filterPanel');
      panel.hidden = !open;
      $('#filterBtn')?.setAttribute('aria-expanded', String(open));
      if (open) {
        const top = $('#scope').getBoundingClientRect().bottom - app.getBoundingClientRect().top;
        panel.style.top = `${top}px`;
        panel.style.maxHeight = `${Math.max(48, app.clientHeight - top - 8)}px`;
        renderFilterPanel(); panel.querySelector('input')?.focus();
      } else $('#filterBtn')?.focus();
    }
    function toggleEdgeState(st) { state.states[st] = !state.states[st]; store.set('states', state.states); renderAll(); if (filterPanelOpen()) renderFilterPanel(); }
    // soloEdgeState shows only one kind of communication; again, all of them.
    const soloEdge = st => Object.entries(state.states).every(([k, v]) => v === (k === st));
    function soloEdgeState(st) {
      const all = soloEdge(st);
      state.states = { ok: all, ghost: all, stale: all, [st]: true };
      store.set('states', state.states); renderAll(); if (filterPanelOpen()) renderFilterPanel();
    }
    const blindFilter = () => (state.filters.facets.visibility || []).length > 0;
    const toggleBlindSpots = () => setFilters({ journeys: state.filters.journeys, facets: { ...state.filters.facets, visibility: blindFilter() ? [] : ['partial', 'none'] } });
    function toggleSignal(sg) {
      if (state.signals.has(sg)) state.signals.delete(sg); else state.signals.add(sg);
      store.set('signals', [...state.signals]); renderAll(); if (filterPanelOpen()) renderFilterPanel();
    }

    // renderFilterPanel lists everything the map can be filtered by, with
    // counts and health: journeys, every label the services share, health,
    // kind of component, technology, and the communication itself.
    function renderFilterPanel() {
      const panel = $('#filterPanel');
      const focused = panel.getRootNode().activeElement?.getAttribute('data-filter-key');
      const q = panelQuery.trim().toLowerCase();
      const f = state.filters;
      const value = (on, label, n, dot, icon, onclick, key) => el('button', { type: 'button', class: 'fv' + (on ? ' on' : ''), 'data-filter-key': key, 'aria-pressed': String(on), onclick },
        el('span', { class: 'check', 'aria-hidden': 'true' }, on ? iconSVG(UI_ICON.check) : null), dot ? el('i', { class: 'dot', style: `background:${css('--' + dot)}` }) : null,
        icon ? iconSVG(icon) : null, label, el('span', { class: 'n num' }, String(n)));
      const sections = [], advanced = [];
      for (const sec of facetsOf(state.snap, state.jkey)) {
        const chosen = sec.key === 'journey' ? f.journeys : (f.facets[sec.key] || []);
        const values = sec.values.filter(v => !q || String(v.label).toLowerCase().includes(q) || sec.title.toLowerCase().includes(q));
        if (!values.length) continue;
        const icon = v => sec.key === 'category' && has(CATEGORY, v.value) ? CATEGORY[v.value].icon : null;
        const dot = v => sec.key === 'category' || sec.key === 'tech' ? null : v.health;
        const destination = ['journey', 'k8s.namespace', 'namespace', 'health'].includes(sec.key) ? sections : advanced;
        destination.push(el('section', {}, el('h5', {}, sec.title), el('div', { class: 'values' },
          ...values.map(v => value(chosen.includes(v.value), v.label, v.count, dot(v), icon(v), () => toggleFacet(sec.key, v.value), JSON.stringify([sec.key, v.value]))))));
      }
      // Slices: the values calls carried. Narrow bars leave their choice to this panel.
      const dims = Object.entries(state.snap.dimensions || {});
      if (dims.length && (!!opts.api || typeof opts.onSliceChange === 'function')) {
        const current = Object.fromEntries((state.slice ? state.slice.split(',') : []).map(p => p.split(':')));
        const choices = [];
        for (const [k, vs] of dims) for (const v of vs) {
          const label = `${k}: ${v}`;
          if (q && !label.toLowerCase().includes(q) && !'calls of'.includes(q)) continue;
          const pick = () => { const next = { ...current }; if (current[k] === v) delete next[k]; else next[k] = v; setSlice(sliceText(next)); renderFilterPanel(); };
          choices.push(value(current[k] === v, label, '', null, null, pick, 'slice:' + k + ':' + v));
        }
        if (choices.length) sections.unshift(el('section', {}, el('h5', { title: 'Only the calls whose own telemetry carried the value, with their own metrics' }, 'Calls of'), el('div', { class: 'values' }, ...choices)));
      }
      if (state.snap.drift) {
        const count = st => state.snap.edges.filter(e => e.state === st).length;
        const states = ['ok', 'ghost', 'stale'].map(st => [st, STATE_LABEL[st]]).filter(([, l]) => !q || l.includes(q) || 'communication'.includes(q));
        if (states.length) advanced.push(el('section', {}, el('h5', {}, 'Communication'), el('div', { class: 'values' },
          ...states.map(([st, label]) => value(state.states[st], label, count(st), null, null, () => toggleEdgeState(st), 'state:' + st)))));
      }
      const present = new Map();
      for (const e of state.snap.edges) for (const sg of e.signals) present.set(sg, (present.get(sg) || 0) + 1);
      const signals = [...present.entries()].filter(([sg]) => has(SIGNAL, sg) && (!q || SIGNAL[sg].label.toLowerCase().includes(q) || 'seen by'.includes(q))).sort();
      if (signals.length) advanced.push(el('section', {}, el('h5', {}, 'Seen by'), el('div', { class: 'values' },
        ...signals.map(([sg, n]) => value(state.signals.has(sg), SIGNAL[sg].label, n, null, SIGNAL[sg].icon, () => toggleSignal(sg), 'signal:' + sg)))));
      const search = el('input', { type: 'search', placeholder: 'Find a journey, namespace, team…', 'aria-label': 'Find a filter', value: panelQuery });
      search.addEventListener('input', ev => {
        panelQuery = ev.target.value;
        const at = ev.target.selectionStart;
        renderFilterPanel();
        const again = panel.querySelector('input');
        again.focus(); again.setSelectionRange(at, at);
      });
      panel.replaceChildren(...[
        el('div', { class: 'top' }, search, el('button', { type: 'button', class: 'btn', onclick: () => setFilterPanel(false) }, 'Done')),
        el('p', { class: 'explain' }, 'Counts cover the whole snapshot. Different filters intersect; values within one filter are combined.'),
        sections.length ? el('div', { class: 'facets' }, ...sections) : null,
        advanced.length ? el('details', { open: q || panelAdvanced ? '' : null, ontoggle: ev => { if (!q) panelAdvanced = ev.target.open; } }, el('summary', {}, 'Advanced filters'), el('div', { class: 'facets' }, ...advanced)) : null,
        !sections.length && !advanced.length ? el('p', { class: 'muted' }, 'Nothing matches. Try a different filter search.') : null,
      ].filter(Boolean));
      if (focused) [...panel.querySelectorAll('[data-filter-key]')].find(b => b.getAttribute('data-filter-key') === focused)?.focus({ preventScroll: true });
    }

    let groupKeys = null;
    function renderGroupBy() {
      const sel = $('#groupBy');
      const keys = labelKeys(state.snap, state.jkey);
      const path = [['k8s.cluster.name', 'cluster'], ['k8s.namespace', 'namespace'], ['service.name', 'app.kubernetes.io/name']].map(choices => choices.find(key => keys.includes(key))).filter(Boolean);
      const hierarchical = path.length > 1 ? 'hierarchy:' + JSON.stringify(path) : '';
      if (hierarchical) keys.push(hierarchical);
      // A journey, a filter or an investigation already narrows the map.
      if (!state.groupChosen) {
        const auto = state.scope || state.investigation.mode !== 'all' || state.operation.id ? '' : autoGroupKey(state.snap, keys);
        if (auto !== state.autoGroup) { state.groupBy = auto; state.autoGroup = auto; state.expanded.clear(); }
      }
      if (state.groupBy && !keys.includes(state.groupBy)) { state.groupBy = ''; state.autoGroup = ''; state.expanded.clear(); }
      if (groupKeys !== keys.join('|')) { // rebuilding would close an open dropdown on every update
        groupKeys = keys.join('|');
        sel.replaceChildren(el('option', { value: '' }, 'No grouping'), ...keys.map(k => el('option', { value: k }, k === hierarchical ? path.join(' → ') : 'Group by ' + k)));
      }
      sel.value = state.groupBy;
    }

    function renderButtons() {
      syncSharedURL();
      const flow = $('#flowBtn');
      flow.replaceChildren(iconSVG(state.flow ? UI_ICON.pause : UI_ICON.play), 'Flow');
      flow.classList.toggle('on', state.flow);
      flow.setAttribute('aria-pressed', String(state.flow));
      svg.classList.toggle('flowing', state.flow);
      const live = $('#liveBtn');
      live.replaceChildren(iconSVG(UI_ICON.live), 'Live');
      live.classList.toggle('on', state.live);
      live.setAttribute('aria-pressed', String(state.live));
      live.disabled = !!opts.comparison || !!state.historyId || state.snap?.timeMode === 'historical';
      live.title = live.disabled ? 'Historical snapshot: return to current state to follow updates' : 'Follow live data updates; Flow only animates traffic';
      $('#historyBtn').hidden = !opts.api;
      $('#exploreBtn').hidden = !!opts.comparison;
      const values = $('#valuesBtn');
      values.replaceChildren(iconSVG(UI_ICON.values), 'Values');
      values.classList.toggle('on', state.values);
      values.setAttribute('aria-pressed', String(state.values));
    }

    // ------------------------------------------------------------- side panel
    function showTab(tab) {
      state.tab = tab;
      app.querySelectorAll('.tab').forEach(t => {
        const on = t.dataset.tab === tab; t.classList.toggle('on', on); t.setAttribute('aria-selected', String(on));
        t.id = 'tab-' + t.dataset.tab; t.setAttribute('aria-controls', 'pane-' + t.dataset.tab); t.tabIndex = on ? 0 : -1;
      });
      app.querySelectorAll('.pane').forEach(p => { p.classList.toggle('on', p.id === 'pane-' + tab); p.setAttribute('aria-labelledby', 'tab-' + p.id.slice(5)); p.tabIndex = 0; });
      if (tab === 'activity') $('#activityCount').textContent = '0';
    }

    function snippet(fix) {
      if (!fix?.snippet) return null;
      const pre = el('pre', {}, fix.snippet);
      const btn = el('button', {
        type: 'button', class: 'btn copy', onclick: ev => {
          ev.stopPropagation();
          navigator.clipboard?.writeText(fix.snippet);
          btn.textContent = 'copied';
          later(() => (btn.textContent = 'copy'), 1200);
        },
      }, 'copy');
      return el('div', { class: 'snippet' }, btn, pre);
    }

    const SEVERITY_LABEL = { critical: 'Critical', warning: 'Warning', info: 'Info' };
    function findingCard(f, open = false) {
      const head = el('button', { type: 'button', class: 'card-head', title: 'Show it on the map', onclick: () => locate(f.subject) },
        el('span', { class: 'meta' }, `${SEVERITY_LABEL[f.severity] || f.severity} · ${f.kind || f.rule || ''} · ${f.subject}`),
        el('span', { class: 'h4' }, f.title));
      const card = el('div', { class: 'card-f ' + f.severity }, head);
      if (f.detail) card.append(el('p', {}, f.detail));
      if (f.fix) {
        card.append(el('p', { class: 'next' }, f.fix.summary));
        const s = snippet(f.fix);
        if (s) card.append(open ? s : el('details', {}, el('summary', {}, 'show ' + (f.fix.language || 'snippet')), s));
      }
      return card;
    }

    // locate shows a subject (node, edge or "a->b") on the map, through its
    // group when it is folded into one. move: false selects it in place.
    function locate(subject, { move = true } = {}) {
      if (!subject || !currentView) return;
      if (portEls.has(subject)) { select({ type: 'port', id: subject }); return; }
      const viewId = id => currentView.nodes.has(id) ? id : currentView.groupOf(id);
      if (subject.includes('->')) {
        const [a, b] = subject.split('->');
        const id = viewId(a) + '->' + viewId(b);
        if (edgeEls.has(id)) { select({ type: 'edge', id }); return; }
        subject = a;
      }
      const id = viewId(subject);
      if (!id || !nodeEls.has(id)) return;
      if (move) focusNode(id); else select({ type: 'node', id });
    }

    function renderSteps() {
      const fs = state.snap.findings;
      $('#stepsCount').textContent = fs.length;
      $('#stepsBadge').textContent = fs.length;
      const pane = $('#pane-steps');
      if (!fs.length) { pane.replaceChildren(el('p', { class: 'muted' }, 'No findings from the configured sources. Missing telemetry may still hide problems.')); return; }
      const sel = state.selected;
      const selected = sel?.type === 'node' ? currentView.nodes.get(sel.id) : sel?.type === 'edge' ? currentView.edges.get(sel.id) : null;
      const ids = selected ? new Set(selected.from ? selected.members.map(e => e.from + '->' + e.to) : selected.group ? selected.members.map(n => n.id) : [selected.id])
        : state.scope ? new Set(state.scope.roles.keys()) : null;
      const local = ids ? fs.filter(f => ids.has(f.subject) || f.subject.split('->').some(id => ids.has(id))) : fs;
      const other = fs.filter(f => !local.includes(f));
      pane.replaceChildren(...[el('h3', {}, ids ? 'Next steps for this selection' : 'Next steps for the whole map'),
        ...local.map(f => findingCard(f)),
        !local.length ? el('p', { class: 'explain' }, 'No advisor actions for this selection. Inspect its measured signals and evidence in Details.') : null,
        other.length ? el('details', {}, el('summary', {}, `Other platform advice (${other.length})`), ...other.map(f => findingCard(f))) : null].filter(Boolean));
    }

    // measureText reads a measure out: "12 of 97 (12%)", "3, held by legacy-reports".
    const measureText = m => (m.unit === 'share' || !(m.max > 0) ? fmtValue(m.value, m.unit) : `${fmtValue(m.value, m.unit)} of ${fmtValue(m.max, m.unit)} (${pct(m.value / m.max)})`) + (m.via ? `, held by ${m.via}` : '');

    // whyBlock explains an amber or red node or line, worst reason first,
    // with a way to the dependency it most likely comes from.
    function whyBlock(list) {
      if (!list.length) return [];
      const unique = [...new Map(list.map(r => [JSON.stringify(r), r])).values()];
      const card = r => el('div', { class: 'evidence-card' },
        el('strong', {}, r.summary || reasonTag(r)),
        el('p', { class: 'explain' }, r.via ? `Dependency signal from ${r.via}; this suggests a relationship, not proof of causation.` : `Measured ${r.what || REASON[r.cause].label.toLowerCase()}${r.subject ? ` on ${r.subject}` : ''}.`),
        el('p', {}, `${r.approximate ? 'Approximate ' : ''}value ${r.cause === 'latency' ? fmtMs(r.value) : fmtValue(r.value, r.unit)} · ${r.health} limit ${r.cause === 'latency' ? fmtMs(r.limit) : fmtValue(r.limit, r.unit)}${r.rule ? ` · rule ${r.rule}` : ''}`),
        r.evidence?.length ? el('p', { class: 'explain' }, 'Sources: ' + [...new Set(r.evidence.map(e => e.source))].join(', ')) : null,
        r.via ? el('button', { type: 'button', class: 'btn', title: `Show ${r.via} on the map`, onclick: () => locate(r.via) }, 'Inspect ' + r.via) : null);
      return [el('h3', {}, 'Primary signal'), card(unique[0]),
        unique.length > 1 ? el('details', {}, el('summary', {}, `Other signals (${unique.length - 1})`), ...unique.slice(1).map(card)) : null];
    }

    function rel(label, sub, health, onclick) {
      return el('button', { type: 'button', class: 'rel', onclick }, el('i', { class: 'dot', style: `background:${css('--' + health)}` }), label, el('span', {}, sub));
    }

    function evidenceBlock(item) {
      const entries = [...new Map((item.evidence || []).map(e => [JSON.stringify(e), e])).values()];
      return [el('h3', {}, 'Evidence and time'), ...(entries.length ? entries.map(e => {
        const original = e.oldestSampleAt || e.observedAt;
        const summary = e.retained ? 'Retained topology; current measurements unavailable'
          : original && e.collectedAt ? `Sample age at collection: ${ageText(original, Date.parse(e.collectedAt))}` : 'Original sample age unknown';
        const subject = e.subject?.node || (e.subject?.from ? `${e.subject.from} → ${e.subject.to}` : '');
        return el('article', { class: 'evidence-card' },
          el('strong', {}, `${e.source}${e.origin ? ` · ${e.origin}` : ''}`),
          subject ? el('p', { class: 'explain' }, subject) : null,
          el('p', {}, e.metrics.join(', ') || e.signal || 'inventory'), el('p', { class: 'explain' }, summary),
          e.windowSeconds ? el('p', { class: 'explain' }, `Query window: ${fmtSec(e.windowSeconds)}${e.sampleCount ? ` · ${e.sampleCount} sampled calls` : ''}`) : null,
          el('details', { 'data-disclosure-key': JSON.stringify(['evidence', e.source, e.origin, e.signal, e.metrics, e.subject]) }, el('summary', {}, 'Collection and sample times'), el('p', { class: 'explain' }, evidenceText(e))));
      })
        : [el('p', { class: 'explain' }, 'Source timing was not supplied; the age of these measurements is unknown.')])];
    }

    function renderIdentity(item) {
      const entries = item.from
        ? (item.members || [item]).flatMap(member => ['from', 'to'].flatMap(endpoint => (member.identity?.[endpoint] || []).map(entry => ({
          entry, subject: `${member.from} → ${member.to} · ${endpoint === 'from' ? 'caller' : 'callee'}`,
        }))))
        : (item.group ? item.members : [item]).flatMap(member => (member.identity || []).map(entry => ({ entry, subject: member.id })));
      // One card per decision: the same input resolved the same way by several
      // sources is one finding. Consequential methods first, plain names last.
      const ORDER = ['ambiguous', 'unresolved', 'inferred', 'ip-temporal', 'alias', 'explicit', 'name'];
      const groups = new Map();
      for (const { entry, subject } of entries) {
        const key = JSON.stringify([subject, entry.method, entry.input, entry.resolvedId, entry.basis, entry.detail]);
        if (!groups.has(key)) groups.set(key, { subject, entry, records: new Map() });
        groups.get(key).records.set(JSON.stringify(entry), entry);
      }
      const decisions = [...groups.values()].map(d => ({ ...d, records: [...d.records.values()] }))
        .sort((a, b) => ORDER.indexOf(a.entry.method) - ORDER.indexOf(b.entry.method));
      const count = m => decisions.filter(({ entry }) => entry.method === m).length;
      const retained = decisions.filter(({ records }) => records.some(r => r.retained || r.candidates.some(candidate => candidate.retained))).length;
      const method = { explicit: 'Explicit name', name: 'Reported name', alias: 'Configured alias', 'ip-temporal': 'IP temporal match', inferred: 'Inferred identity', ambiguous: 'Ambiguous identity', unresolved: 'Unresolved address' };
      const times = { unknown: 'unknown', 'evaluation-window': 'query evaluation window', 'collection-window': 'collection window; not a call timestamp', 'sample-envelope': 'original sample envelope', 'sample-point': 'original observation point' };
      const summary = `Identity and provenance (${decisions.length}${['ambiguous', 'unresolved', 'inferred'].map(m => count(m) ? ` · ${count(m)} ${m}` : '').join('')}${retained ? ` · ${retained} retained` : ''})`;
      return el('details', { 'data-disclosure-key': 'identity-provenance' }, el('summary', {}, summary),
        !decisions.length ? el('p', { class: 'explain' }, 'Identity provenance was not supplied for this snapshot; a matching name alone does not establish ownership.') : null,
        ...decisions.map(({ entry, subject, records }) => {
          const sources = [...new Set(records.map(r => r.source + (r.sourceType && r.sourceType !== r.source ? ` (${r.sourceType})` : '')))];
          const timed = records.filter(r => r.timeBasis !== 'not-applicable');
          const collected = records.map(r => r.collectedAt).filter(Boolean).sort().at(-1);
          const candidates = [...new Map(records.flatMap(r => r.candidates).map(c => [JSON.stringify(c), c])).values()];
          const alternatives = [...new Set(records.flatMap(r => r.alternatives))];
          const isRetained = records.some(r => r.retained);
          return el('article', { class: 'evidence-card' },
            el('strong', {}, `${method[entry.method]}${isRetained ? ' · retained decision' : ''}`),
            el('p', { class: 'explain' }, subject),
            el('p', {}, el('code', {}, entry.input), ' → ', el('code', {}, entry.resolvedId)),
            el('p', {}, `${sources.length > 1 ? 'Sources' : 'Source'}: ${sources.join(', ')}${entry.basis ? ` · ${entry.basis}` : ''}`),
            entry.method === 'ambiguous' ? el('p', { class: 'explain' }, 'No workload owner was selected for this decision. Inspect the candidates and observation period.') : null,
            entry.method === 'inferred' ? el('p', { class: 'explain' }, 'This decision includes an inference; inspect its basis and source limits.') : null,
            entry.detail ? el('p', { class: 'explain' }, entry.detail) : null,
            !timed.length ? el('p', { class: 'explain' }, `No time anchor is required for this match. Last collected: ${collected || 'unknown'}.`) : null,
            ...timed.map(r => el('p', { class: 'explain' }, `${sources.length > 1 ? r.source + ' · ' : ''}Time basis: ${times[r.timeBasis]}. Collected: ${r.collectedAt || 'unknown'}. Query evaluated: ${r.evaluatedAt || 'unknown'}. ` +
              `Original observation: ${r.observedFrom || 'unknown'} → ${r.observedAt || 'unknown'}. ${r.windowSeconds > 0 ? `Lookback: ${fmtSec(r.windowSeconds)}.` : 'No lookback supplied.'}`)),
            isRetained ? el('p', { class: 'explain' }, 'This is the original retained decision; its timestamps do not establish current ownership.') : null,
            alternatives.length ? el('p', { class: 'explain' }, `Reading candidates, in source order: ${alternatives.join(', ')}.`) : null,
            candidates.length ? el('div', {}, el('strong', {}, `Inventory candidates (${candidates.length})`), el('ul', {}, ...candidates.map(candidate => el('li', {},
              el('code', {}, candidate.id), ` · ${candidate.source}${candidate.sourceType ? ` (${candidate.sourceType})` : ''}${candidate.retained ? ' · retained inventory; current ownership not refreshed' : ''}`,
              candidate.basis ? el('p', { class: 'explain' }, candidate.basis) : null,
              el('p', { class: 'explain' }, `Reported start: ${candidate.since || 'unknown'}. Collected: ${candidate.collectedAt || 'unknown'}. The reported start is not independently verified IP lease history.`))))) : null);
        }));
    }

    // One-step entries into the bounded investigation of a component.
    function investigationActions(item) {
      if (item.from || item.group || opts.comparison || state.operation.id) return [];
      return [
        el('button', { type: 'button', class: 'btn', onclick: () => {
          state.investigation = { mode: 'neighbors', focus: item.id, target: '', depth: 1, direction: 'both' };
          state.display = 'graph'; state.tablePage = 0; state.fitted = false;
          renderAll(); syncSharedURL();
        } }, 'Show neighborhood'),
        el('button', { type: 'button', class: 'btn', 'aria-haspopup': 'dialog', onclick: () => openWorkspace('scope', { mode: 'path', focus: item.id, target: '' }) }, 'Find path…'),
      ];
    }

    function detailActions(item) {
      return el('div', { class: 'detail-actions' },
        el('button', { type: 'button', class: 'btn', onclick: focusSelection }, 'Focus on map'),
        ...investigationActions(item),
        ...[...new Map((item.links || []).map(l => [l.url, l])).values()].map(l => el('span', {}, el('a', {
          class: 'btn', href: l.url, target: '_blank', rel: 'noopener noreferrer', title: l.description,
        }, l.label), l.description ? el('small', { class: 'explain' }, l.description) : null)));
    }

    function effectiveRules(item) {
      const rules = [...new Map((item.healthRules || []).map(r => [JSON.stringify(r), r])).values()];
      return rules.length ? el('details', {}, el('summary', {}, 'Effective health rules'), ...rules.map(r =>
        el('p', { class: 'explain' }, `${r.subject ? r.subject + ' · ' : ''}${REASON[r.cause].label}: degraded ${fmtValue(r.degraded, r.unit)}, failing ${fmtValue(r.failing, r.unit)} · ${r.name} (${r.match}${r.priority ? `, priority ${r.priority}` : ''})`))) : null;
    }

    function presenceBlock(type, ids) {
      const presence = state.snap.history?.[type];
      if (!presence) return [];
      return [el('details', {}, el('summary', {}, 'Historical identity and presence'), ...ids.flatMap(id => {
        const p = presence[id];
        return p ? [el('p', { class: 'explain' }, `${id} · ${p.identityConfidence || 'identity confidence unknown'} · first seen ${p.firstSeen} · last seen ${p.lastSeen} · original observation ${p.lastObservedAt || 'unknown'}. Graph presence is not proof of continuous activity.`)] : [];
      }))];
    }

    function renderDetails() {
      const pane = $('#pane-details'), key = JSON.stringify(state.selected);
      const same = pane.dataset.selection === key;
      const disclosureKey = d => d.getAttribute('data-disclosure-key') || d.querySelector('summary')?.textContent;
      const opened = new Set(same ? [...pane.querySelectorAll('details[open]')].map(disclosureKey) : []);
      const scroll = same ? pane.scrollTop : 0;
      const active = root.activeElement;
      const focusIndex = same && pane.contains(active) ? [...pane.querySelectorAll('button,a,summary,select')].indexOf(active) : -1;
      renderDetailsContent();
      pane.dataset.selection = key;
      for (const d of pane.querySelectorAll('details')) d.open = opened.has(disclosureKey(d));
      if (focusIndex >= 0) pane.querySelectorAll('button,a,summary,select')[focusIndex]?.focus({ preventScroll: true });
      pane.scrollTop = scroll;
    }

    function renderDetailsContent() {
      const pane = $('#pane-details');
      const sel = state.selected;
      if (!sel || !currentView) { pane.replaceChildren(el('p', { class: 'muted' }, 'Click a service, a datastore or a communication on the map.')); return; }
      if (sel.type === 'port') { renderPortDetails(pane, portEls.get(sel.id)?.port); return; }
      if (sel.type === 'node') {
        const n = currentView.nodes.get(sel.id);
        if (!n) { pane.replaceChildren(el('p', { class: 'muted' }, 'That node is no longer in the graph.')); return; }
        const tile = el('div', { class: 'tile ' + n.health }, iconSVG(iconOf(n)));
        const parts = [el('div', { class: 'hero' }, tile, el('div', {}, el('h2', {}, n.title),
          el('span', { class: 'pill' }, el('i', { class: 'dot', style: `background:${css('--' + n.health)}` }), healthText(n)), ' ',
          el('span', { class: 'pill' }, n.kind), ' ', el('span', { class: 'pill' }, n.visibility + ' visibility'))), detailActions(n), ...whyBlock(n.reasons), ...evidenceBlock(n), effectiveRules(n)];
        parts.push(renderIdentity(n));
        const fs = state.snap.findings.filter(f => n.group ? n.members.some(m => m.id === f.subject) : f.subject === n.id || f.subject.startsWith(n.id + '->'));
        if (fs.length) { parts.push(el('h3', {}, 'Next steps')); parts.push(...fs.map(f => findingCard(f))); }
        if (n.group) {
          const inside = n.internal ? `, talking to each other through ${n.internal} communication${n.internal === 1 ? '' : 's'}` : '';
          parts.push(el('p', { class: 'explain' }, `${n.members.length} workload${n.members.length === 1 ? '' : 's'} with ${n.labelKey}=${n.title}${inside}. Double-click the card to expand it.`));
          parts.push(el('button', { type: 'button', class: 'btn', onclick: () => toggleGroup(n.id) }, 'Expand group'));
          if (state.groupBy.startsWith('hierarchy:')) {
            const parent = groupingPath(n.members[0], state.groupBy).find(g => state.expanded.has(g.expansionKey));
            if (parent) parts.push(el('button', { type: 'button', class: 'btn', onclick: () => { state.expanded.delete(parent.expansionKey); renderMap(); locate(n.members[0].id, { move: false }); fit(); } }, 'Collapse to ' + parent.title));
          }
          if (n.latencyP95Ms > 0) parts.push(el('p', { class: 'explain' }, latencyText(n)));
          parts.push(el('h3', {}, 'Members'));
          parts.push(...n.members.map(m => rel(m.id, m.kind, m.health, () => { for (const g of groupingPath(m, state.groupBy)) state.expanded.add(g.expansionKey); renderMap(); focusNode(m.id); })));
        } else {
          if (state.scope) {
            const others = state.snap.edges.some(e => e.to === n.id && !state.scope.roles.has(e.from));
            parts.push(el('p', { class: 'explain' }, state.scope.roles.get(n.id) === 'dependency'
              ? `Used by ${scopeName()}${others ? ', and by others outside it: its health includes their traffic' : ''}.`
              : `Part of ${scopeName()}${others ? '; others outside it call it too' : ''}.`));
          }
          parts.push(el('p', { class: 'explain' }, VIS_EXPLAIN[n.visibility]));
          const kv = el('dl', { class: 'kv' });
          const row = (k, v) => kv.append(el('dt', {}, k), el('dd', {}, v));
          if (n.requestRate > 0) row(n.trafficDirection ? `${n.trafficDirection} calls` : 'traffic', `${fmtRate(n.requestRate)} req/s · ${errorText(n)}`);
          if (n.latencyP95Ms > 0) row('latency', latencyText(n));
          row('component', `${CATEGORY[categoryOf(n)]?.label || n.kind}${n.tech ? ' · ' + techLabel(n) : ''}`);
          for (const m of n.measures) row(m.what, measureText(m));
          row('known from', n.sources.join(', ') || '—');
          if (n.addresses.length) row('addresses', n.addresses.join(', '));
          parts.push(kv);
          if (n.signals.length) {
            parts.push(el('h3', {}, 'Seen by'));
            parts.push(el('div', { class: 'labels' }, ...n.signals.map(s => el('span', { class: 'label' }, SIGNAL[s].label))));
          }
          const labels = Object.entries(n.labels).filter(([k]) => userLabel(k));
          if (labels.length) {
            parts.push(el('h3', {}, 'Labels'));
            parts.push(el('div', { class: 'labels' }, ...labels.map(([k, v]) => el('button', {
              type: 'button', class: 'label', title: 'Search for this label', onclick: () => { $('#search').value = `${k}:${v}`; state.query = `${k}:${v}`; applyHighlight(); },
            }, el('b', {}, k + '='), String(v)))));
          }
        }
        parts.push(...presenceBlock('nodes', n.group ? n.members.map(m => m.id) : [n.id]));
        if (!n.group && groupingPath(n, state.groupBy).length) parts.push(el('button', { type: 'button', class: 'btn', onclick: () => toggleGroup(n.id) }, 'Collapse parent group'));
        const order = el('select', { 'aria-label': 'Sort neighbours', onchange: event => { state.neighbourSort = event.target.value; renderDetails(); } },
          ...['traffic', 'errors', 'latency'].map(key => el('option', { value: key }, key)));
        order.value = state.neighbourSort || 'traffic';
        const out = neighbourOrder([...currentView.edges.values()].filter(e => e.from === n.id), order.value);
        const inn = neighbourOrder([...currentView.edges.values()].filter(e => e.to === n.id), order.value);
        if (out.length + inn.length) parts.push(el('label', {}, 'Sort neighbours ', order));
        const relEdge = (e, other) => rel(other, `${e.requestRate > 0 ? fmtRate(e.requestRate) + '/s · ' : ''}${errorText(e)}${e.latencyP95Ms > 0 ? ' · ' + latencyText(e) : ''}`, e.state === 'stale' ? 'unknown' : e.health, () => select({ type: 'edge', id: e.id }));
        if (out.length) { parts.push(el('h3', {}, `Calls (${out.length})`)); parts.push(...out.map(e => relEdge(e, e.to))); }
        if (inn.length) { parts.push(el('h3', {}, `Called by (${inn.length})`)); parts.push(...inn.map(e => relEdge(e, e.from))); }
        pane.replaceChildren(...parts.filter(Boolean));
        return;
      }
      const e = currentView.edges.get(sel.id);
      if (!e) { pane.replaceChildren(el('p', { class: 'muted' }, 'That communication is no longer in the graph.')); return; }
      const color = e.state === 'stale' ? 'unknown' : e.health;
      const pills = [el('span', { class: 'pill' }, el('i', { class: 'dot', style: `background:${css('--' + color)}` }), healthText(e))];
      if (state.snap.drift) pills.push(' ', el('span', { class: 'pill' }, STATE_LABEL[e.state]));
      const parts = [el('div', { class: 'hero' },
        el('div', { class: 'tile ' + color }, iconSVG(UI_ICON.arrow)),
        el('div', {}, el('h2', {}, `${e.from} → ${e.to}`), ...pills)), detailActions(e), ...whyBlock(e.reasons)];
      for (const member of e.members.filter(m => m.frequency)) parts.push(el('p', { class: 'explain' }, `${e.members.length > 1 ? `${member.from} → ${member.to}: ` : ''}Expected every ${fmtSec(member.expectedFrequencySeconds)} · ${member.frequency.status}. ${text(member.frequency.detail)} Last observed: ${timestamp(member.frequency.lastObservedAt) || 'unknown'}. Next expected: ${timestamp(member.frequency.nextExpectedAt) || 'unknown'}.`));
      parts.push(...presenceBlock('edges', e.members.map(m => m.from + '->' + m.to)));
      // Plain query frames carry no declared intent: "not declared" there is not a contract gap.
      if (state.snap.drift && !state.operation.id && e.members.length === 1 && e.members[0].observed && !e.members[0].declared) parts.push(el('button', { type: 'button', class: 'btn', onclick: () => { state.reviewEdge = e.members[0]; openWorkspace('review'); } }, 'Review contract proposal'));
      if (state.snap.drift) {
        parts.push(el('p', { class: 'explain' }, {
          ok: 'Declared in a flows file and observed in traffic: intent and reality agree.',
          ghost: 'Observed in traffic but no flows file declares it. Either the declaration is out of date, or this call was never meant to exist.',
          stale: 'Declared, but no traffic was observed while the caller was visible. Remove it if the dependency is gone; keep it if it is rare.',
        }[e.state]));
      }
      const kv = el('dl', { class: 'kv' });
      const row = (k, v) => kv.append(el('dt', {}, k), el('dd', {}, v));
      row('protocol', [e.protocol || '—', e.port ? ':' + e.port : ''].join(''));
      if (e.requestRate > 0) {
        row('traffic', `${fmtRate(e.requestRate)} req/s`);
        row('errors', errorText(e));
      }
      if (e.latencyP95Ms > 0) row('latency', latencyText(e));
      const edgeDims = {};
      for (const m of e.members) for (const [k, vs] of Object.entries(m.dimensions || {})) for (const v of strings(vs)) (edgeDims[k] ||= new Set()).add(v);
      if (state.snap.slice) row('measured', 'only calls with ' + sliceText(state.snap.slice.selector).replaceAll(',', ', '));
      else if (Object.keys(edgeDims).length) row('measured', 'all calls, every ' + Object.keys(edgeDims).sort().join(' and '));
      for (const [k, vs] of Object.entries(edgeDims).sort()) row(k, [...vs].sort().join(', '));
      for (const m of e.measures) row(m.what, measureText(m));
      if (state.snap.drift) row('reason', e.reason || (e.declared ? '—' : 'not declared'));
      row('seen by', e.signals.map(s => has(SIGNAL, s) ? SIGNAL[s].label : s).join(', ') || (e.observed ? '—' : 'nobody yet'));
      if (e.sources.length) row('sources', e.sources.join(', '));
      if (e.members.length > 1) row('aggregates', `${e.members.length} communications`);
      parts.push(kv, ...evidenceBlock(e), renderIdentity(e), effectiveRules(e));
      const fs = state.snap.findings.filter(f => e.members.some(m => f.subject === m.from + '->' + m.to) || (e.state === 'ghost' && f.subject === e.from && f.rule === 'undeclared-flow'));
      if (fs.length) { parts.push(el('h3', {}, 'Next steps')); parts.push(...fs.map(f => findingCard(f))); }
      pane.replaceChildren(...parts.filter(Boolean));
    }

    // renderPortDetails explains a port: who is outside the view, how they
    // touch it, and ways to see them.
    function renderPortDetails(pane, p) {
      if (!p) { pane.replaceChildren(el('p', { class: 'muted' }, 'That port is no longer on the map.')); return; }
      const role = {
        caller: `Outside ${scopeName()}, calling into ${p.anchors.join(', ')}.`,
        tenant: `Outside ${scopeName()}, using ${p.anchors.join(', ')} too: their traffic is part of its health.`,
        exit: `Where ${scopeName()} leaves to, from ${p.anchors.join(', ')}.`,
        more: `More outside nodes around ${scopeName()}.`,
      }[p.kind];
      const parts = [el('h2', {}, p.label), el('p', { class: 'explain' }, role), ...whyBlock(p.reasons.slice(0, 3))];
      parts.push(el('h3', {}, `Outside (${p.of.length})`));
      for (const id of p.of) {
        const n = state.snap.nodes.find(x => x.id === id);
        parts.push(rel(id, n ? techLabel(n) : '', n ? n.health : 'unknown', () => { setFilters(emptyFilters()); locate(id); }));
      }
      const actions = [el('button', { type: 'button', class: 'btn', onclick: () => { setFilters(emptyFilters()); locate(p.of[0]); } }, 'Show on the whole map')];
      if (p.journey) actions.push(el('button', { type: 'button', class: 'btn', onclick: () => setFilters({ journeys: [p.journey], facets: {} }) }, `Open journey ${p.journey}`));
      parts.push(el('div', { class: 'labels' }, ...actions));
      pane.replaceChildren(...parts.filter(Boolean));
    }

    function renderSources() {
      const pane = $('#pane-sources');
      if (!state.snap.sources.length) { pane.replaceChildren(el('p', { class: 'muted' }, 'No source details in this data.')); return; }
      pane.replaceChildren(el('h3', {}, 'Signals teia reads'), ...state.snap.sources.map(s => el('div', { class: 'src' },
        el('div', {}, s.name, el('small', {}, `${s.type} · ${s.message || `${s.nodes} nodes, ${s.edges} edges`}`),
          el('small', {}, `Last successful collection: ${timestamp(s.lastSuccessAt) || 'unknown'}${s.retained ? ' · retained topology' : ''}`)),
        el('span', { class: 'state ' + s.state }, s.state))));
    }

    // The history feed speaks the map's grammar: a line, a card, a hatch.
    const CHANGE_ICON = {
      'edge.added': '<path d="M2.75 10H10.25M14.25 6.25V13.75M10.5 10H18"/>',
      'edge.removed': '<path d="M2.75 10H8.25M11.75 10H17.25"/>',
      'edge.state': '<path d="M2.75 10H8.25"/><path d="M11 10H17.25" stroke-dasharray="2 2"/>',
      'edge.health': '<path d="M10 3.25L17.25 16.25H2.75Z"/>',
      'node.added': '<rect x="2.75" y="5.75" width="9.5" height="8.5" rx="1"/><path d="M15.25 7V13M12.25 10H18.25"/>',
      'node.removed': '<rect x="2.75" y="5.75" width="9.5" height="8.5" rx="1"/><path d="M14.25 10H18.25"/>',
      'node.health': '<path d="M10 3.25L17.25 16.25H2.75Z"/>',
      'node.visibility': '<rect x="2.75" y="5.75" width="14.5" height="8.5" rx="1"/><path d="M5.5 14.25L10.25 5.75M9.75 14.25L14.5 5.75" stroke-width="1"/>',
      'finding.opened': '<path d="M4.25 17.25V2.75M4.25 3.75H15.25L12.75 7.25L15.25 10.75H4.25"/>',
      'finding.resolved': '<path d="M4.5 10.25L8.25 14L15.5 6.5"/>',
    };
    const changeTone = c => c.to === 'failing' || (c.type === 'finding.opened' && c.to === 'critical') ? 'bad'
      : c.to === 'healthy' || c.type === 'finding.resolved' ? 'good'
        : (c.type === 'edge.added' && c.to === 'ghost') || c.to === 'degraded' ? 'warn' : '';
    const changeKey = c => `${c.at}|${c.type}|${c.id || c.subject}`;

    function renderActivity(freshCount = 0) {
      const pane = $('#pane-activity');
      if (!state.activity.length) { pane.replaceChildren(el('p', { class: 'muted' }, 'Changes between discovery rounds show up here as they happen.')); return; }
      pane.replaceChildren(el('h3', {}, 'What changed, round by round'), ...state.activity.map((c, i) => {
        const tone = changeTone(c);
        const ic = iconSVG(has(CHANGE_ICON, c.type) ? CHANGE_ICON[c.type] : CHANGE_ICON['edge.state']);
        ic.style.color = tone === 'bad' ? css('--failing') : tone === 'good' ? css('--healthy') : tone === 'warn' ? css('--degraded') : css('--accent');
        return el('button', { type: 'button', class: 'ev' + (i < freshCount ? ' new' : ''), onclick: () => locate(c.subject) },
          el('time', { class: 'num' }, timeOf(c.at)), ic, el('span', {}, c.message));
      }));
    }

    function pushActivity(changes, fresh) {
      const known = new Set(state.activity.map(changeKey));
      const add = (changes || []).filter(c => c && c.message && !known.has(changeKey(c)));
      if (!add.length) return;
      state.activity = [...add.slice().reverse(), ...state.activity].slice(0, 300);
      renderActivity(fresh ? add.length : 0);
      if (fresh && state.tab !== 'activity') $('#activityCount').textContent = Number($('#activityCount').textContent) + add.length;
      if (fresh) toast(add);
    }

    function toast(changes, message) {
      const items = message ? [{ tone: 'bad', small: 'teia', text: message }]
        : changes.filter(c => changeTone(c) !== '' || c.type === 'edge.added').slice(0, 3)
          .map(c => ({ tone: changeTone(c), small: `${c.type} · ${timeOf(c.at)}`, text: c.message, subject: c.subject }));
      for (const it of items) {
        const t = el('button', { type: 'button', class: 'toast ' + it.tone, onclick: () => it.subject && locate(it.subject) }, el('small', {}, it.small), it.text);
        $('#toasts').prepend(t);
        later(() => t.remove(), 6000);
      }
    }

    // ---------------------------------------------------------------- export
    function exportMap(kind) {
      if (!nodeEls.size) { toast(null, 'Nothing to export yet.'); return; }
      const clone = svg.cloneNode(true);
      clone.querySelector('.particles')?.remove();
      clone.querySelectorAll('.dim').forEach(e => e.classList.remove('dim'));
      let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
      for (const g of nodeEls.values()) {
        x0 = Math.min(x0, g._pos.x - NODE_W / 2); x1 = Math.max(x1, g._pos.x + NODE_W / 2);
        y0 = Math.min(y0, g._pos.y - NODE_H / 2); y1 = Math.max(y1, g._pos.y + NODE_H / 2 + 12);
      }
      const pad = 50, w = x1 - x0 + pad * 2, h = y1 - y0 + pad * 2;
      clone.querySelector('#viewport').setAttribute('transform', `translate(${pad - x0},${pad - y0})`);
      clone.setAttribute('viewBox', `0 0 ${w} ${h}`);
      clone.setAttribute('width', w); clone.setAttribute('height', h);
      clone.setAttribute('xmlns', SVGNS);
      const vars = ['--bg', '--bg-2', '--panel', '--line', '--line-2', '--ink', '--ink-2', '--muted', '--accent', '--healthy', '--degraded', '--failing', '--unknown', '--ghost', '--stale', '--edge', '--particle', '--card', '--card-hi'];
      const st = clone.querySelector('style');
      st.textContent = `svg{${vars.map(v => `${v}:${css(v)}`).join(';')};font-family:"IBM Plex Sans",system-ui,sans-serif}` + st.textContent;
      clone.insertBefore(sv('rect', { width: '100%', height: '100%', fill: css('--bg') }), clone.firstChild.nextSibling);
      const text = new XMLSerializer().serializeToString(clone);
      const name = `teia-map-${state.snap?.cluster ? state.snap.cluster.replace(/[^\w.-]+/g, '_') + '-' : ''}${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}`;
      if (kind === 'svg') { download(new Blob([text], { type: 'image/svg+xml' }), name + '.svg'); return; }
      // Browsers refuse canvases over ~16k px a side or ~120M pixels.
      const scale = Math.max(0.1, Math.min(2, 16000 / Math.max(w, h), Math.sqrt(120e6 / (w * h))));
      const img = new Image();
      img.onerror = () => toast(null, 'The map could not be rendered as PNG; try SVG.');
      img.onload = () => {
        const c = document.createElement('canvas');
        c.width = Math.round(w * scale); c.height = Math.round(h * scale);
        const ctx = c.getContext('2d');
        if (!ctx) { toast(null, 'The browser refused a canvas this large; try SVG.'); return; }
        ctx.scale(scale, scale); ctx.drawImage(img, 0, 0);
        c.toBlob(b => b ? download(b, name + '.png') : toast(null, 'The map is too large for PNG; try SVG.'), 'image/png');
      };
      img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(text);
    }
    function download(blob, name) {
      const a = el('a', { href: URL.createObjectURL(blob), download: name });
      document.body.append(a); a.click(); a.remove();
      later(() => URL.revokeObjectURL(a.href), 2000);
    }

    // ------------------------------------------------ investigation workspace
    let comparisonMaps = [], workspaceEpoch = 0;
    const comparison = { before: null, after: null };
    function clearComparisonMaps() { comparisonMaps.forEach(controller => controller.destroy()); comparisonMaps = []; }
    function choice(label, id, values, selected) {
      const control = el('select', { id });
      for (const [value, title] of values) control.append(el('option', { value }, title));
      control.value = String(selected ?? '');
      return el('label', {}, label, control);
    }
    function workspaceStatus(message) { const target = $('#workspaceStatus'); if (target) target.textContent = message; }
    function openWorkspace(tab, preset) {
      if (opts.comparison) return;
      ++workspaceEpoch; clearComparisonMaps();
      state.workspace = tab;
      const pane = $('#workspaceContent'); pane.replaceChildren();
      if (!$('#investigateDialog').open) $('#investigateDialog').showModal();
      app.querySelectorAll('[data-workspace]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.workspace === tab)));
      if (tab === 'compare') renderComparisonSetup(pane);
      else if (tab === 'setup') renderSetup(pane);
      else if (tab === 'operations') renderOperations(pane);
      else if (tab === 'review') renderReview(pane);
      else renderFocusSetup(pane, preset);
    }
    function renderFocusSetup(pane, preset) {
      const current = { ...state.investigation, ...preset }, selected = state.selected?.type === 'node' ? state.selected.id : '';
      const names = el('datalist', { id: 'investigationNames' }, ...(state.snap?.nodes || []).map(n => el('option', { value: n.id })));
      pane.append(el('p', {}, 'A topology path shows directed relationships, not proof that one request traversed them. Existing journey and facet filters still apply' +
          (state.slice ? `, and so does the slice: only calls with ${sliceLabel(state.slice)}.` : '.')),
        el('div', { class: 'workspace-grid' },
          choice('Scope', 'investigationMode', [['all','Whole filtered map'],['neighbors','Limited neighborhood'],['path','Shortest directed path']], current.mode),
          el('label', {}, 'Start component', el('input', { id: 'investigationFocus', list: 'investigationNames', value: current.focus || selected, autocomplete: 'off' })),
          el('label', {}, 'Destination (path)', el('input', { id: 'investigationTarget', list: 'investigationNames', value: current.target, autocomplete: 'off' })),
          choice('Neighborhood depth', 'investigationDepth', [[1,'1 hop'],[2,'2 hops'],[3,'3 hops']], current.depth),
          choice('Neighborhood direction', 'investigationDirection', [['both','Callers and dependencies'],['in','Callers only'],['out','Dependencies only']], current.direction),
          choice('Presentation', 'investigationDisplay', [['graph','Graph'],['list','Components and connections table']], state.display)), names,
        el('p', {}, 'Focused views show at most 200 components and disclose omitted context. A path longer than that is rejected rather than cut.'),
        el('p', { id: 'workspaceStatus', class: 'workspace-status', role: 'status' }),
        el('button', { type: 'button', class: 'btn', onclick: () => {
          const mode = $('#investigationMode').value, focus = $('#investigationFocus').value.trim(), target = $('#investigationTarget').value.trim();
          if (mode !== 'all' && !state.snap?.nodes.some(n => n.id === focus)) return workspaceStatus('Choose an existing start component.');
          if (mode === 'path' && !state.snap?.nodes.some(n => n.id === target)) return workspaceStatus('Choose an existing destination.');
          state.investigation = { mode, focus, target, depth: Number($('#investigationDepth').value), direction: $('#investigationDirection').value };
          state.display = $('#investigationDisplay').value; state.tablePage = 0; state.fitted = false; state.selected = null; setDrawer(false);
          if (state.snap) renderAll(); syncSharedURL(); $('#investigateDialog').close();
        } }, 'Apply investigation'));
      if (preset?.mode === 'path') $('#investigationTarget').focus();
    }
    function renderInvestigation() {
      const info = state.investigated, summary = $('#investigationSummary'), table = $('#entityTable');
      const active = state.investigation.mode !== 'all' || state.display === 'list' || !!state.operation.id;
      summary.hidden = !active; stage.classList.toggle('has-investigation', active);
      if (active && info) {
        const message = (state.operation.id ? `Sampled operation: ${state.snap.operationContext?.operation} · ` : '') + `${info.snapshot.nodes.length} components · ${info.snapshot.edges.length} connections · ${info.hiddenNodes} components outside view · ${info.boundaryEdges} crossing connections`;
        summary.replaceChildren(el('span', {}, message + (info.limited ? ' · 200-component limit reached; narrow the scope.' : '') + (!info.found ? ' · No directed path in the available topology.' : '')),
          el('button', { class: 'btn', type: 'button', onclick: () => { state.display = state.display === 'list' ? 'graph' : 'list'; renderInvestigation(); syncSharedURL(); } }, state.display === 'list' ? 'Show graph' : 'Show table'),
          el('button', { class: 'btn', type: 'button', onclick: () => { state.investigation.mode = 'all'; state.display = 'graph'; state.operation = { id: '', traceId: '' }; state.fitted = false; apply(state.rawSnapshot, false, true); renderAll(); syncSharedURL(); } }, 'Reset investigation'));
      }
      table.hidden = state.display !== 'list';
      // The table replaces the map: its zoom, legend and help would only cover rows.
      stage.classList.toggle('list-view', state.display === 'list');
      if (table.hidden || !info) return;
      const matches = matcher(state.query) || (() => true);
      const rows = [...currentView.nodes.values()].filter(n => matches(n)).map(n => ({ type: 'Component', id: n.id, name: n.title, item: n }));
      for (const e of currentView.edges.values()) if (edgeVisible(e) && (!state.query || matches(currentView.nodes.get(e.from)) || matches(currentView.nodes.get(e.to)))) rows.push({ type: 'Connection', id: e.id, name: `${e.from} → ${e.to}`, item: e });
      rows.sort((a, b) => state.tableSort === 'name' ? a.name.localeCompare(b.name) : state.tableSort === 'traffic'
        ? (b.item.requestRate || 0) - (a.item.requestRate || 0) || a.name.localeCompare(b.name)
        : HEALTH_RANK[b.item.health] - HEALTH_RANK[a.item.health] || a.name.localeCompare(b.name));
      const pages = Math.max(1, Math.ceil(rows.length / 50)); state.tablePage = Math.min(state.tablePage, pages - 1);
      const sorting = choice('Sort table', 'investigationSort', [['health','Health'],['name','Name'],['traffic','Traffic']], state.tableSort);
      sorting.querySelector('select').addEventListener('change', event => { state.tableSort = event.target.value; state.tablePage = 0; renderInvestigation(); table.scrollTop = 0; $('#investigationSort').focus(); });
      const body = el('tbody');
      for (const row of rows.slice(state.tablePage * 50, state.tablePage * 50 + 50)) body.append(el('tr', {},
        el('td', {}, row.type), el('td', {}, el('button', { type: 'button', class: 'btn', onclick: () => { select({ type: row.type === 'Component' ? 'node' : 'edge', id: row.id }); } }, row.name)),
        el('td', {}, healthText(row.item)), el('td', {}, row.item.requestRate > 0 ? `${fmtRate(row.item.requestRate)}/s` : 'No positive measured rate'),
        el('td', {}, errorText(row.item))));
      // A new page starts at its first row; focus moves to the top pager, which
      // is in view there, whichever pager was used.
      const turn = step => {
        state.tablePage += step; renderInvestigation(); table.scrollTop = 0;
        (step > 0 ? ['#tableNext', '#tablePrevious'] : ['#tablePrevious', '#tableNext']).map(s => $(s)).find(b => b && !b.disabled)?.focus({ preventScroll: true });
      };
      const pager = suffix => pages < 2 ? null : el('div', { class: 'detail-actions' },
        el('button', { id: 'tablePrevious' + suffix, class: 'btn', type: 'button', disabled: state.tablePage === 0 ? '' : null, onclick: () => turn(-1) }, 'Previous rows'),
        el('button', { id: 'tableNext' + suffix, class: 'btn', type: 'button', disabled: state.tablePage + 1 >= pages ? '' : null, onclick: () => turn(1) }, 'Next rows'));
      table.replaceChildren(...[sorting, el('p', {}, `${rows.length} matching rows · page ${state.tablePage + 1} of ${pages}. Groups can be expanded from their details.`), pager(''),
        el('table', {}, el('caption', { class: 'sr-only' }, 'Investigation components and connections'), el('thead', {}, el('tr', {}, ...['Type','Identity','Health','Traffic','Errors'].map(label => el('th', { scope: 'col' }, label)))), body),
        pager('End')].filter(Boolean));
      table.style.top = Math.ceil(summary.offsetHeight + 16) + 'px';
    }
    function validSnapshot(raw) {
      if (!raw || !Array.isArray(raw.nodes) || !Array.isArray(raw.edges)) throw new Error('Expected a Teia snapshot with nodes and edges arrays.');
      if (raw.nodes.length > 10000 || raw.edges.length > 50000) throw new Error('Comparison input exceeds 10,000 components or 50,000 connections. Narrow the capture first.');
      return sanitize(raw);
    }
    function renderComparisonSetup(pane) {
      pane.append(el('p', {}, 'Compare exact observations. “Only before” is absence from the second capture, not proof that traffic stopped or that a declaration should be deleted. Rate jitter is excluded from structural changes.'));
      if (state.slice) pane.append(el('p', {}, `The map shows only calls with ${sliceLabel(state.slice)}, and “Capture current snapshot” takes that slice. Stored observations hold every call, so compare a slice with another capture of the same slice.`));
      const inputs = el('div', { class: 'workspace-grid' });
      for (const side of ['before','after']) {
        const upload = el('input', { type: 'file', accept: '.json,application/json', 'aria-label': `Upload ${side} snapshot` });
        upload.addEventListener('change', async () => {
          const file = upload.files[0]; if (!file) return;
          const epoch = workspaceEpoch;
          try {
            if (file.size > 16 * 1024 * 1024) throw new Error('Snapshot files must be at most 16 MiB.');
            const raw = JSON.parse(await file.text()); if (epoch !== workspaceEpoch || destroyed) return;
            comparison[side] = validSnapshot(raw); updateComparison();
          } catch (error) { if (epoch === workspaceEpoch) workspaceStatus(error.message); }
        });
        inputs.append(el('div', { class: 'workspace-card' }, el('h3', {}, side === 'before' ? 'Before' : 'After'), upload,
          el('button', { type: 'button', class: 'btn', disabled: state.snap ? null : '', onclick: () => { comparison[side] = sanitize(JSON.parse(JSON.stringify(state.baseSnapshot || state.snap))); updateComparison(); } }, `Capture current snapshot as ${side}`),
          // Stored history exists only on a reachable Teia server.
          opts.api ? choice('Stored observation', 'compare-' + side, [['','Choose a stored snapshot']], '') : null, el('p', { id: 'compare-label-' + side })));
      }
      pane.append(inputs, el('p', { id: 'workspaceStatus', class: 'workspace-status', role: 'status' }), el('div', { id: 'comparisonResult' }));
      updateComparison();
      if (!opts.api) { workspaceStatus('Query mode: upload two exported snapshots or capture two received observations. Server history is available in direct API mode.'); return; }
      const epoch = workspaceEpoch;
      fetch(apiURL('history'), { signal: aborter.signal }).then(async response => {
        if (!response.ok) throw new Error('Stored history could not be loaded. You can still compare uploaded snapshots.');
        return response.json();
      }).then(listing => {
        if (destroyed || workspaceEpoch !== epoch) return;
        if (!listing.enabled) workspaceStatus('History is disabled on this server. Uploaded/captured snapshots remain available.');
        const entries = [...(listing.snapshots || [])].sort((a,b) => b.generatedAt.localeCompare(a.generatedAt)).slice(0,200);
        for (const side of ['before','after']) {
          const select = $('#compare-' + side);
          for (const entry of entries) select.append(el('option', { value: entry.id }, `${entry.generatedAt} · ${entry.nodes} components`));
          select.addEventListener('change', async () => {
            const id = select.value; if (!id) return;
            try {
              const response = await fetch(apiURL('history/' + encodeURIComponent(id)), { signal: aborter.signal });
              if (!response.ok) throw new Error('This stored observation is unavailable or expired. No traffic conclusion can be drawn.');
              const raw = await response.json(); if (destroyed || epoch !== workspaceEpoch || select.value !== id) return;
              if (raw.history?.id !== id) throw new Error('The response does not match the selected observation.');
              comparison[side] = validSnapshot(raw); updateComparison();
            } catch (error) { if (epoch === workspaceEpoch && select.value === id) workspaceStatus(error.message); }
          });
        }
        if ((listing.snapshots || []).length > 200) workspaceStatus('Showing the newest 200 stored observations. Export an older capture by its ID to compare it here.');
      }).catch(error => { if (!destroyed && epoch === workspaceEpoch) workspaceStatus(error.message); });
    }
    function updateComparison() {
      for (const side of ['before','after']) {
        const snap = comparison[side];
        $('#compare-label-' + side).textContent = snap ? `${snap.generatedAt || 'Capture time unknown'} · ${snap.history?.id || 'uploaded/captured observation'} · ${snap.nodes.length} components` +
          (snap.cluster ? ` · cluster ${snap.cluster}` : '') + (snap.slice ? ` · only calls with ${sliceLabel(sliceText(snap.slice.selector))}` : '') : 'No observation selected';
      }
      clearComparisonMaps(); const result = $('#comparisonResult'); result.replaceChildren();
      if (!comparison.before || !comparison.after) return;
      const diff = compareSnapshots(comparison.before, comparison.after);
      if (!diff.comparable) {
        const holds = scope => (scope.slice ? 'only calls with ' + sliceLabel(scope.slice) : 'every call') + (scope.cluster ? ` of cluster ${scope.cluster}` : '');
        result.append(el('p', { role: 'status' }, `Not compared: “before” holds ${holds(diff.scope.before)} and “after” holds ${holds(diff.scope.after)}. Their differences would be the scope, not changes. Choose two captures of the same scope.`));
        return;
      }
      result.append(el('p', { role: 'status' }, `${diff.counts.added} only after · ${diff.counts.removed} only before · ${diff.counts.changed} changed · ${diff.counts.unchanged} unchanged`));
      if (Date.parse(comparison.before.generatedAt) > Date.parse(comparison.after.generatedAt)) result.append(el('p', {}, 'The selected “before” capture is newer than “after”. The comparison preserves your selected order.'));
      result.append(el('p', {}, 'Map marks: + only after (green outline) · − only before (red dashed outline) · ~ changed health, state, kind, labels, addresses or identity resolution (amber outline). Unmarked items are unchanged and muted.'));
      const maps = el('div', { class: 'workspace-grid' });
      // Each map marks only what it can show: absences belong to "before", arrivals to "after".
      const marks = (rows, side) => Object.fromEntries(rows.filter(row => row.status === 'changed' || row.status === (side === 'before' ? 'removed' : 'added')).map(row => [row.id, row.status]));
      for (const side of ['before','after']) {
        const host = el('div', { class: 'comparison-map', 'aria-label': `${side} communication map` });
        maps.append(el('div', {}, el('h3', {}, side === 'before' ? 'Before' : 'After'), host));
        result.append(maps);
        const controller = mount(host, { mode: 'panel', flow: false, theme: hostTheme(), comparison: true, storageKey: prefix + 'compare-' + side,
          diff: { nodes: marks(diff.nodes, side), edges: marks(diff.edges, side) } });
        controller.update(comparison[side]); comparisonMaps.push(controller);
      }
      const rows = [...diff.nodes, ...diff.edges].filter(row => row.status !== 'unchanged');
      const body = el('tbody');
      for (const row of rows.slice(0,500)) body.append(el('tr', { class: 'change-' + row.status },
        el('td', {}, row.status === 'removed' ? '− Only before' : row.status === 'added' ? '+ Only after' : '~ Changed'),
        el('td', {}, row.type, ' ', el('button', { type: 'button', class: 'btn', onclick: () => comparisonMaps.forEach(controller => controller.focus(row.id)) }, row.id)),
        el('td', {}, row.changes.map(change => `${change.field}: ${JSON.stringify(change.before)} → ${JSON.stringify(change.after)}`).join('; ') || 'Presence in these two captures')));
      result.append(el('table', { class: 'workspace-table' }, el('caption', {}, 'Structural and health differences'), el('thead', {}, el('tr', {}, ...['Change','Identity — locate on both maps','Before → after'].map(label => el('th', { scope: 'col' }, label)))), body),
        el('button', { class: 'btn', type: 'button', onclick: () => download(new Blob([JSON.stringify({ before: comparison.before, after: comparison.after, differences: diff }, null, 2)], { type: 'application/json' }), 'teia-comparison.json') }, 'Download comparison'));
      if (rows.length > 500) result.append(el('p', {}, `${rows.length - 500} additional differences are available in the comparison download.`));
    }
    function renderSetup(pane) {
      pane.append(el('h3', {}, 'Connect data and check coverage'), el('p', {}, '1. Connect a source. 2. Check identities and field units. 3. Confirm what is measured before interpreting health.'));
      const sources = state.baseSnapshot?.sources || state.snap?.sources || [];
      pane.append(el('div', { class: 'workspace-grid' }, ...sources.map(source => el('section', { class: 'workspace-card' }, el('h4', {}, source.name),
        el('p', {}, `${source.type} · ${source.state} · ${source.nodes || 0} components · ${source.edges || 0} connections`), el('p', {}, source.message || 'No source error reported.'),
        source.retained ? el('p', {}, 'Retained topology: this round did not renew its observation time.') : null))));
      if (!sources.length) pane.append(el('p', {}, 'No source diagnostics supplied. A query returning topology does not prove complete instrumentation.'));
      const items = [...(state.snap?.nodes || []), ...(state.snap?.edges || [])];
      pane.append(el('p', {}, `${items.filter(x => x.errorsUnknown).length} entities have unknown errors. Metrics require requestRate in requests/second, errorRate as a 0–1 fraction, and latencyP95Ms in milliseconds. Missing fields stay unknown.`));
      if (typeof opts.onMappingChange === 'function') {
        pane.append(el('h3', {}, 'Map Grafana query fields'), el('p', {}, 'Choose query frames and their identity columns. Convert units in the query before mapping. Saving persists in panel options; save the dashboard to retain it.'));
        const frames = Array.isArray(opts.inputFrames) ? opts.inputFrames : [];
        const mapping = opts.fieldMapping || {}, cards = el('div', { class: 'workspace-grid' });
        const roles = [['nodes', ['id', 'kind', 'health', 'requestRate', 'errorRate', 'latencyP95Ms']], ['edges', ['source', 'target', 'requestRate', 'errorRate', 'latencyP95Ms']]];
        for (const [role, fields] of roles) {
          const card = el('section', { class: 'workspace-card' }, el('h4', {}, role === 'nodes' ? 'Components' : 'Connections'));
          const select = choice('Query frame', 'mapping-' + role, [['', 'Automatic / unmapped'], ...frames.map(f => [f.key, f.key])], mapping[role]?.frame || '');
          const columns = el('div');
          const refresh = () => {
            const selected = select.querySelector('select').value, frame = frames.find(f => f.key === selected);
            columns.replaceChildren(...(frame ? fields.map(field => choice(field + (['id','source','target'].includes(field) ? ' (required)' : ' (optional)'), `mapping-${role}-${field}`,
              [['','Choose field'], ...frame.fields.map(name => [name, name])], mapping[role]?.[field] || (frame.fields.includes(field) ? field : ''))) : []));
          };
          select.querySelector('select').addEventListener('change', refresh); refresh(); card.append(select, columns); cards.append(card);
        }
        pane.append(cards, el('button', { type: 'button', class: 'btn', onclick: () => {
          const next = {};
          for (const [role, fields] of roles) {
            const frame = $('#mapping-' + role).value;
            if (!frame) continue;
            next[role] = { frame };
            for (const field of fields) { const val = $(`#mapping-${role}-${field}`)?.value; if (val) next[role][field] = val; }
            if ((role === 'nodes' && !next[role].id) || (role === 'edges' && (!next[role].source || !next[role].target))) { workspaceStatus('Select all required identity columns.'); return; }
          }
          try { opts.onMappingChange(next); workspaceStatus('Mapping sent to panel options. Check the returned map and query diagnostics, then save the dashboard.'); }
          catch (error) { workspaceStatus(error.message); }
        } }, 'Save field mapping'));
      } else {
        pane.append(el('h3', {}, 'Prepare a server source'), el('p', {}, 'The endpoint must be reachable from the Teia server. This downloads a configuration fragment for review; it does not change the running server. Configure credentials through server environment variables.'));
        const type = choice('Source type', 'setupSource', [['prometheus','Prometheus metrics'],['tempo','Tempo sampled traces']], 'prometheus');
        const url = el('input', { id: 'setupURL', type: 'url', placeholder: 'http://prometheus:9090' });
        const preview = el('pre', { id: 'setupPreview' });
        pane.append(type, el('label', {}, 'Server endpoint', url), el('button', { type: 'button', class: 'btn', onclick: () => {
          try {
            const endpoint = new URL(url.value);
            if (!['http:','https:'].includes(endpoint.protocol) || endpoint.username || endpoint.password || endpoint.search || endpoint.hash) throw new Error('Use an HTTP(S) endpoint without credentials, query parameters or fragment.');
            const kind = $('#setupSource').value;
            const config = `sources:\n  ${kind}:\n    - url: ${JSON.stringify(endpoint.href)}\n      window: ${kind === 'tempo' ? '10m' : '2m'}\n${kind === 'prometheus' ? '      presets: [auto]\n' : ''}`;
            preview.textContent = config; workspaceStatus('Merge this sources entry into teia.yaml, restart Teia, and check source status and coverage here.');
            download(new Blob([config], { type: 'application/yaml' }), 'teia-source.yaml');
          } catch (error) { workspaceStatus(error.message || 'Enter a valid endpoint.'); }
        } }, 'Prepare configuration'), preview);
      }
      pane.append(el('p', { id: 'workspaceStatus', role: 'status' }), el('p', {}, 'If identities appear as IPs, review inventory and temporal ownership. If errors are unknown, add an error measurement; zero is only meaningful when measured. Operation views require route or RPC attributes and parent-span linkage.'));
    }
    function renderOperations(pane) {
      const operations = state.baseSnapshot?.operations;
      pane.append(el('h3', {}, 'Investigate a sampled operation'), el('p', {}, 'Operations use span ancestry and normalized route/RPC attributes. Ownership journeys remain a separate topological view. Sample outcomes and durations do not establish service health, request rates or total traffic coverage.'));
      // In a slice: the traces whose entry carried it, and in them its calls.
      const sliced = state.baseSnapshot?.slice ? sliceText(state.baseSnapshot.slice.selector) : state.slice;
      if (sliced) pane.append(el('p', {}, `Only calls with ${sliceLabel(sliced)}: the sampled traces whose entry carried it, and in them the calls that did. Counts, outcomes and durations are those of these traces.`));
      if (!operations?.scopes.length) {
        pane.append(el('p', {}, sliced ? `No sampled entry carried ${sliceLabel(sliced)} in this capture. Missing metadata is not evidence that no requests occurred.`
          : 'No attributed operations in this capture. Connect Tempo and instrument http.route + method, RPC service/method, or teia.operation. Missing metadata is not evidence that no requests occurred.'));
      }
      if (operations) pane.append(...operations.sources.map(source => el('p', {}, source.status === 'excluded'
        ? `${source.source}: excluded · it cannot split its traces by the selected dimensions, so its operations are left out of this slice`
        : `${source.source}: ${source.status} · ${source.fetchedTraces}/${source.requestedTraces} requested traces returned · ${source.failedTraces} failures${source.truncated ? ' · truncated' : ''} · ${source.queryPeriod?.from || '?'} – ${source.queryPeriod?.to || '?'}`)),
        el('p', {}, `${operations.unattributed.length} spans without an attributed operation. Sampling policy and traffic denominator are unknown.`));
      const scopes = operations?.scopes || [];
      // The operation on the map may be absent here (another slice): offer the choice again.
      const current = scopes.some(s => s.id === state.operation.id) ? state.operation.id : '';
      const select = choice('Operation', 'operationSelect', [['','Choose an operation'], ...scopes.map(s => [s.id, `${s.entryService} · ${s.operation} · ${s.source}`])], current);
      const details = el('section', { id: 'operationDetails' });
      const refresh = () => {
        const scope = scopes.find(s => s.id === $('#operationSelect').value); details.replaceChildren();
        if (!scope) return;
        const outcomes = scope.rootOutcomes || {};
        details.append(el('p', {}, `${scope.sampledRootCount} sampled roots · ${scope.incompleteTraceCount} traces with gaps · root outcomes: ${outcomes.ok || 0} OK / ${outcomes.error || 0} error / ${outcomes.unknown || 0} unknown${scope.truncated ? ' · truncated' : ''}`),
          el('p', {}, `Query period: ${scope.queryPeriod?.from || '?'} – ${scope.queryPeriod?.to || '?'}. ${(() => {
            // A range of individual samples; no percentile or rate is derived from them.
            const ms = (Array.isArray(scope.sampleRootDurationsMs) ? scope.sampleRootDurationsMs : []).filter(Number.isFinite);
            return ms.length ? `Sampled root durations range from ${fmtMs(Math.min(...ms))} to ${fmtMs(Math.max(...ms))} across ${ms.length} roots; these are individual samples, not a traffic percentile.`
              : 'No valid root durations were sampled.';
          })()}`));
        const traceIds = [...new Set(scope.traces.map(t => t.traceId))];
        const traces = choice('Trace', 'operationTrace', [['','All returned traces'], ...traceIds.map(id => [id, `${id} · ${scope.traces.filter(t => t.traceId === id).length} sampled roots`])], state.operation.id === scope.id ? state.operation.traceId : '');
        const spans = el('div', { id: 'operationSpans' });
        const showTrace = () => {
          const selected = scope.traces.filter(t => t.traceId === traces.querySelector('select').value); spans.replaceChildren();
          if (!selected.length) return;
          for (const trace of selected) spans.append(el('p', {}, `Root span ${trace.rootSpanId} · ${trace.rootOutcome} · ${trace.structuralStatus}${trace.rootDurationMs != null ? ' · ' + fmtMs(trace.rootDurationMs) : ''}. Missing parents: ${strings(trace.missingParentSpanIds).join(', ') || 'none reported'}. Unattributed spans: ${strings(trace.unattributedSpanIds).join(', ') || 'none reported'}.${trace.truncated ? ' Truncated sample.' : ''} Linked sample does not mean a complete trace.`));
          const uniqueSpans = [...new Map(selected.flatMap(t => t.spans).map(p => [p.spanId,p])).values()];
          for (const span of uniqueSpans.slice(0, 200)) spans.append(el('p', {}, `${span.nodeId} · ${span.kind} · span ${span.spanId} ← ${span.parentSpanId || 'root'} · ${span.startedAt || '?'} – ${span.endedAt || '?'}${Array.isArray(span.links) && span.links.length ? ' · asynchronous links (not traversed): ' + span.links.filter(Boolean).map(l => `${l.traceId}/${l.spanId}`).join(', ') : ''}`));
          if (uniqueSpans.length > 200) spans.append(el('p', {}, `${uniqueSpans.length - 200} more spans in the snapshot JSON export.`));
        };
        traces.querySelector('select').addEventListener('change', showTrace); details.append(traces, spans); showTrace();
      };
      pane.append(select, details); select.querySelector('select').addEventListener('change', refresh); refresh();
      pane.append(el('button', { type: 'button', class: 'btn', onclick: () => {
        const id = $('#operationSelect').value; if (!id) { workspaceStatus('Choose an available operation.'); return; }
        state.operation = { id, traceId: $('#operationTrace')?.value || '' }; state.investigation.mode = 'all'; state.filters = emptyFilters(); state.groupBy = ''; state.selected = null; state.fitted = false;
        apply(state.rawSnapshot, false, true); fit(); syncSharedURL(); $('#investigateDialog').close();
      } }, 'Show sampled operation'), el('button', { type: 'button', class: 'btn', onclick: () => {
        state.operation = { id: '', traceId: '' }; state.fitted = false; apply(state.rawSnapshot, false, true); fit(); syncSharedURL(); $('#investigateDialog').close();
      } }, 'Return to topology'), el('p', { id: 'workspaceStatus', role: 'status' }));
    }
    function renderReview(pane) {
      const edge = state.reviewEdge;
      if (!edge || state.operation.id) { pane.append(el('p', {}, 'Select one observed, undeclared connection in the topology to prepare a proposal.')); return; }
      // Freeze both the displayed evidence and its exact timestamp at review time.
      const snapshot = JSON.parse(JSON.stringify(state.rawSnapshot)), epoch = workspaceEpoch;
      pane.append(el('h3', {}, `Review ${edge.from} → ${edge.to}`), el('p', {}, `Capture: ${snapshot.generatedAt || 'unknown'}${snapshot.history?.id ? ' · ' + snapshot.history.id : ''}. Review ownership, protocol and identity before proposing a contract change.`));
      const rationale = el('textarea', { id: 'reviewRationale', rows: '3', maxlength: '4000', placeholder: 'Why should this communication be part of the declared contract?' });
      const output = el('div', { id: 'reviewOutput' });
      pane.append(el('label', {}, 'Review rationale', rationale), el('button', { type: 'button', class: 'btn', onclick: async event => {
        const request = { from: edge.from, to: edge.to, rationale: rationale.value.trim(), generatedAt: snapshot.generatedAt, ...(snapshot.history?.id ? { historyId: snapshot.history.id } : {}) };
        if (!request.rationale) { workspaceStatus('Explain the reason for this proposed communication.'); rationale.focus(); return; }
        output.replaceChildren();
        if (!opts.api) {
          output.append(el('p', {}, 'This panel uses query data. Export the input and run local validation to produce the review bundle; it is not a server-authenticated capture.'),
            el('button', { type: 'button', class: 'btn', onclick: () => download(new Blob([JSON.stringify(snapshot, null, 2)], { type: 'application/json' }), 'snapshot.json') }, 'Download snapshot.json'),
            el('button', { type: 'button', class: 'btn', onclick: () => download(new Blob([JSON.stringify(request, null, 2)], { type: 'application/json' }), 'request.json') }, 'Download request.json'),
            el('pre', {}, 'teia review propose --snapshot snapshot.json --request request.json --format bundle --output proposal.zip'));
          workspaceStatus('The CLI validates identities and evidence before generating flows.yaml, proposal.md and proposal.json.'); return;
        }
        const button = event.currentTarget; button.disabled = true; workspaceStatus('Validating the reviewed capture…');
        try {
          const response = await fetch(apiURL('review/proposals'), { method: 'POST', signal: aborter.signal, headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: JSON.stringify(request) });
          if (!response.ok) { const message = await response.text(); throw new Error(response.status === 409 ? 'The capture changed. Close this review, inspect the latest evidence and start a new proposal.' : `Proposal rejected (${response.status}): ${message.slice(0, 500)}`); }
          const proposal = await response.json(); if (epoch !== workspaceEpoch || destroyed) return;
          output.append(el('h4', {}, proposal.title), el('p', {}, `Proposal ${proposal.id} · input ${proposal.source?.inputKind} · snapshot ${proposal.source?.snapshotId}`));
          for (const file of proposal.files || []) output.append(el('details', {}, el('summary', {}, file.name), el('pre', {}, file.content),
            el('button', { type: 'button', class: 'btn', onclick: () => download(new Blob([file.content], { type: file.mediaType }), file.name) }, 'Download ' + file.name)));
          workspaceStatus('Proposal prepared for review. No repository or running contract has been changed.');
        } catch (error) { if (epoch === workspaceEpoch && !destroyed) workspaceStatus(error.message); }
        finally { if (button.isConnected) button.disabled = false; }
      } }, 'Prepare review proposal'), el('p', { id: 'workspaceStatus', role: 'status' }), output);
    }
    function hostTheme() { return host.dataset.theme || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'); }

    // ----------------------------------------------------------------- data
    function renderAll() {
      const active = root.activeElement, focusId = active?.id;
      const portId = [...portEls].find(([, port]) => port.g === active)?.[0];
      index = indexSnapshot(state.snap);
      state.jkey = journeyKeyOf(state.snap, state.journeyKey);
      state.scope = scopeOf(state.snap, { ...state.filters, journeyKey: state.jkey });
      renderRings(); renderGroupBy(); renderScope(); renderButtons();
      renderMap(); renderSteps(); renderSources(); renderDetails(); renderLive();
      if (active && !active.isConnected) {
        const target = portId ? portEls.get(portId)?.g || $('#filterBtn') : focusId ? $('#' + focusId) : null;
        target?.focus({ preventScroll: true });
      }
    }

    function apply(raw, fresh, force = false) {
      if (destroyed) return;
      const base = sanitize(raw);
      if (opts.api && state.cluster && base.cluster !== state.cluster) return;
      const snap = state.operation.id ? operationSnapshot(base, state.operation.id, state.operation.traceId) : base;
      state.connected = true;
      state.error = '';
      if (!force && (state.historyLoading || state.historyId)) return;
      const hostReplayChanged = !opts.api && (snap.timeMode === 'historical' || state.snap?.timeMode === 'historical') &&
        (snap.history?.id !== state.snap?.history?.id || snap.timeMode !== state.snap?.timeMode);
      if (!force && !hostReplayChanged && !state.live && state.snap) { state.pending = raw; renderLive(); return; }
      if (hostReplayChanged) { state.activity = []; renderActivity(); }
      // Frames from a host hold the slice their queries asked for: the data,
      // not a choice made here, says which calls the map shows.
      if (!opts.api) {
        const held = base.slice ? sliceText(base.slice.selector) : '';
        if (held !== state.slice) { state.slice = held; state.positions = stored(posKey(), {}); topologyKey = ''; state.fitted = false; }
      }
      const sig = signature(snap);
      const changed = sig !== state.sig;
      const isNew = !state.snap || snap.round !== state.snap.round;
      state.snap = snap;
      renderCluster();
      state.baseSnapshot = base;
      state.rawSnapshot = raw;
      state.lastData = Date.now();
      if (changed) { state.sig = sig; renderAll(); } else renderLive();
      if (opts.select && !state.restoredSelect) { state.restoredSelect = true; locate(opts.select, { move: false }); }
      if (fresh && isNew) pushActivity(snap.changes, snap.timeMode !== 'historical');
      return true;
    }

    // load fetches the current snapshot; force draws it even while live
    // updates are paused, for a view the user just asked for.
    async function load(signal, force = false) {
      if (state.historyId) { await replay(state.historyId); return; }
      const get = path => fetch(apiURL(path), { signal, headers: { Accept: 'application/json' } }).then(r => { if (!r.ok) throw new Error('HTTP ' + r.status); return r.json(); });
      const [snap, changes] = await Promise.all([get('snapshot' + sliceQuery()), get('changes').catch(() => [])]);
      if (destroyed) return;
      if (snap.generatedAt && !String(snap.generatedAt).startsWith('0001')) apply(snap, false, force);
      pushActivity(changes, false);
    }

    let historyRequest = 0;
    async function replay(id) {
      const request = ++historyRequest;
      state.historyLoading = true;
      if (es) { es.close(); es = null; }
      try {
        const response = await fetch(apiURL(id ? 'history/' + encodeURIComponent(id) : 'snapshot'), { signal: aborter.signal });
        if (!response.ok) throw new Error(id ? 'Historical snapshot unavailable. It may have expired; this does not mean no traffic.' : 'Current state could not be loaded.');
        const snap = await response.json();
        if (destroyed || request !== historyRequest) return;
        if (id && (snap.timeMode !== 'historical' || snap.history?.id !== id)) throw new Error('The server did not return the requested historical snapshot.');
        state.historyId = id; state.live = !id; state.pending = null;
        state.activity = []; pushActivity(snap.changes, false); renderActivity();
        apply(snap, false, true); renderButtons();
        $('#historyDialog').close();
      } catch (error) {
        if (destroyed || request !== historyRequest) return;
        state.error = error.message; $('#historyStatus').textContent = error.message; renderLive();
      } finally {
        if (request === historyRequest) { state.historyLoading = false; if (!state.historyId) connect(); }
      }
    }

    let historyListRequest = 0;
    async function listHistory() {
      const request = ++historyListRequest;
      const from = $('#historyFrom').value, to = $('#historyTo').value;
      const query = from && to ? '?from=' + encodeURIComponent(new Date(from + 'Z').toISOString()) + '&to=' + encodeURIComponent(new Date(to + 'Z').toISOString()) : '';
      $('#historyStatus').textContent = 'Loading history…'; $('#historyList').replaceChildren();
      try {
        const response = await fetch(apiURL('history' + query), { signal: aborter.signal });
        if (!response.ok) throw new Error('History could not be loaded. Check the UTC range and source connection.');
        const result = await response.json();
        if (destroyed || request !== historyListRequest) return;
        $('#historyStatus').textContent = !result.enabled ? 'History is disabled. Enable history.directory on the Teia server to persist snapshots.'
          : result.snapshots?.length ? `${result.snapshots.length} stored snapshots. Retention: ${fmtSec(result.retentionSeconds)}.${result.writeError ? ' Latest write failed: ' + result.writeError : ''}`
            : 'No stored snapshots in this interval. This does not mean no traffic.';
        // Bound DOM work; the newest 200 snapshots are visible, narrower time ranges reach older ones.
        const snapshots = [...(result.snapshots || [])].sort((a, b) => b.generatedAt.localeCompare(a.generatedAt));
        $('#historyList').replaceChildren(...snapshots.slice(0, 200).map(s => el('button', {
          type: 'button', class: 'rel', 'data-history-id': s.id, onclick: () => replay(s.id),
        }, `${s.generatedAt} · ${s.nodes} components · ${s.edges} connections`)));
        if (snapshots.length > 200) $('#historyList').append(el('p', {}, `${snapshots.length - 200} older snapshots hidden. Narrow the UTC range to inspect them.`));
      } catch (error) { if (!destroyed && request === historyListRequest) $('#historyStatus').textContent = error.message; }
    }

    const sliceQuery = () => state.slice ? '?slice=' + encodeURIComponent(state.slice) : '';
    // apiURL is an API path for the cluster on the map: every request names
    // it, so no answer can come from another cluster.
    function apiURL(path) {
      if (!state.cluster) return opts.api + path;
      return opts.api + path + (path.includes('?') ? '&' : '?') + 'cluster=' + encodeURIComponent(state.cluster);
    }
    function refreshApiLinks() {
      if (opts.api) app.querySelectorAll('[data-api]').forEach(a => a.setAttribute('href', apiURL(a.dataset.api)));
    }
    // renderCluster shows which cluster every number on the map comes from:
    // the one the server says it answered for, not just the one asked for.
    function renderCluster() {
      const current = state.snap?.cluster || state.cluster, names = state.clusters;
      const box = $('#clusterBox'), pick = $('#clusterPick'), name = $('#clusterName');
      box.hidden = !current && names.length < 2;
      if (box.hidden) return;
      pick.hidden = names.length < 2;
      name.hidden = !pick.hidden;
      name.textContent = current;
      if (!pick.hidden) {
        if (pick.options.length !== names.length || [...pick.options].some((o, i) => o.value !== names[i])) pick.replaceChildren(...names.map(n => el('option', { value: n }, n)));
        pick.value = current;
      }
      box.title = `Every component, connection and number on this map comes from cluster ${current} only`;
      if (page && typeof document !== 'undefined') document.title = `teia · ${current}`;
    }
    // setCluster shows another cluster: its own sources, graph, numbers,
    // history and activity. Nothing of the previous cluster stays on screen.
    function setCluster(next) {
      next = String(next || '').trim();
      if (next === (state.cluster || state.snap?.cluster || '')) return Promise.resolve();
      state.cluster = next;
      state.pending = null;
      state.historyId = '';
      state.operation = { id: '', traceId: '' };
      state.activity = []; renderActivity();
      select(null);
      state.positions = stored(posKey(), {});
      topologyKey = '';
      state.fitted = false;
      if (page && typeof history !== 'undefined' && typeof location !== 'undefined') {
        const url = new URL(location.href);
        if (next) url.searchParams.set('cluster', next); else url.searchParams.delete('cluster');
        history.replaceState(null, '', url);
      }
      syncSharedURL();
      refreshApiLinks();
      renderCluster();
      if (es) { es.close(); es = null; }
      if (!opts.api) return Promise.resolve();
      const loaded = load(aborter.signal, true).catch(error => { state.error = error.message; renderLive(); }).finally(() => { state.fitted = fit(); connect(); });
      $('#announce').textContent = `Cluster ${next}`;
      return loaded;
    }
    // setSlice shows only the calls whose telemetry carried these dimension
    // values (journey:x,tenant:y), with their own metrics; '' is the whole map.
    function setSlice(next) {
      next = String(next || '').trim();
      if (next === state.slice) return Promise.resolve();
      state.slice = next;
      // An update held while paused belongs to the previous selection.
      state.pending = null;
      state.positions = stored(posKey(), {});
      topologyKey = '';
      state.fitted = false;
      if (page && typeof history !== 'undefined' && typeof location !== 'undefined') {
        const url = new URL(location.href);
        if (next) url.searchParams.set('slice', next); else url.searchParams.delete('slice');
        history.replaceState(null, '', url);
      }
      syncSharedURL();
      if (es) { es.close(); es = null; }
      $('#announce').textContent = next ? `Only calls with ${next.replaceAll(',', ', ')}` : 'Whole map';
      if (!opts.api) {
        // A host that feeds frames asks its queries for the slice (a dashboard
        // variable); the frames that come back say which slice they hold.
        if (typeof opts.onSliceChange === 'function') { opts.onSliceChange(next); renderScope(); }
        return Promise.resolve();
      }
      return load(aborter.signal, true).catch(error => { state.error = error.message; renderLive(); }).finally(() => { state.fitted = fit(); connect(); });
    }

    function connect() {
      if (destroyed || es || !state.live || state.historyId || state.historyLoading || !opts.api || opts.stream === false) return;
      try { es = new EventSource(apiURL('stream' + sliceQuery())); } catch { return; }
      es.addEventListener('snapshot', ev => {
        reconnectDelay = 1000;
        try { apply(JSON.parse(ev.data), true); } catch { /* a malformed event is skipped */ }
      });
      es.onopen = () => { state.connected = true; renderLive(); };
      es.onerror = () => {
        state.connected = false;
        renderLive();
        if (es && es.readyState === EventSource.CLOSED) {
          // The browser gives up after a non-stream answer (a proxy's 502, a
          // restart): retry with backoff and fill the gap from the snapshot.
          es = null;
          const delay = reconnectDelay;
          reconnectDelay = Math.min(30000, reconnectDelay * 2);
          later(() => {
            if (destroyed || !state.live) return;
            load(aborter.signal).catch(() => {}).finally(connect);
          }, delay);
        }
      };
    }

    // ------------------------------------------------------------------ wiring
    function onKey(ev) {
      if ($('#shareDialog').open || $('#historyDialog').open || $('#investigateDialog').open) return;
      const t = ev.composedPath()[0];
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return;
      if (ev.key === 'Escape' && filterPanelOpen()) { ev.preventDefault(); ev.stopPropagation(); setFilterPanel(false); return; }
      if (t && (t.tagName === 'INPUT' || t.tagName === 'SELECT' || t.tagName === 'TEXTAREA')) { if (ev.key === 'Escape') t.blur(); return; }
      const onControl = t && (t.tagName === 'BUTTON' || t.tagName === 'A' || t.tagName === 'SUMMARY');
      if (ev.key === 'Escape') {
        const open = $('aside').classList.contains('open');
        if (state.selected || open) { ev.stopPropagation(); select(null); setDrawer(false); $('#stepsBtn').focus(); }
        return;
      }
      if (onControl) return;
      if (ev.key === 'f') fit();
      else if (ev.key === 'j') { ev.preventDefault(); setFilterPanel(true); }
      else if (ev.key === 'v') $('#valuesBtn').click();
      else if (ev.key === ' ') { ev.preventDefault(); $('#flowBtn').click(); }
      else if (ev.key === '/') { ev.preventDefault(); $('#search').focus(); }
    }

    let searchTimer = 0;
    app.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => showTab(t.dataset.tab)));
    $('.tabs').addEventListener('keydown', ev => {
      if (!ev.target.classList.contains('tab') || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(ev.key)) return;
      ev.preventDefault();
      const tabs = [...app.querySelectorAll('.tab')], i = tabs.indexOf(ev.target);
      const next = ev.key === 'Home' ? 0 : ev.key === 'End' ? tabs.length - 1 : (i + (ev.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length;
      showTab(tabs[next].dataset.tab); tabs[next].focus();
    });
    showTab(state.tab);
    $('#search').addEventListener('input', ev => {
      // Read the value now: once dispatch ends, the event is retargeted to
      // the shadow host and ev.target.value is gone.
      const value = ev.currentTarget.value;
      $('#isolateBtn').hidden = !value.trim();
      clearTimeout(searchTimer);
      searchTimer = setTimeout(() => { state.query = value; state.tablePage = 0; syncSharedURL(); applyHighlight(); renderInvestigation(); }, 120);
    });
    $('#search').addEventListener('keydown', ev => {
      if (ev.key !== 'Enter') return;
      if (ev.shiftKey) { ev.preventDefault(); isolate(); return; }
      state.query = ev.target.value; syncSharedURL(); applyHighlight(); renderInvestigation();
      const m = matcher(state.query);
      const hit = m && currentView && [...currentView.nodes.values()].find(m);
      if (hit) focusNode(hit.id);
    });
    $('#groupBy').addEventListener('change', ev => {
      const selected = state.selected?.type === 'node' ? currentView.nodes.get(state.selected.id) : null;
      const identity = selected?.group ? selected.members[0]?.id : state.selected?.id;
      state.groupBy = ev.target.value; state.groupChosen = true; state.autoGroup = ''; state.expanded.clear(); store.set('groupBy', state.groupBy);
      syncSharedURL();
      renderMap(); if (identity) locate(identity, { move: false }); else renderDetails(); fit();
    });
    $('#flowBtn').addEventListener('click', () => { state.flow = !state.flow; store.set('flow', state.flow); renderButtons(); rebuildParticles(); wake(); });
    $('#valuesBtn').addEventListener('click', () => { state.values = !state.values; store.set('values', state.values); renderButtons(); renderReasons(); applyHighlight(); });
    // isolate turns what the search names into the journey shown.
    const isolate = () => {
      let q = $('#search').value.trim();
      if (!q) return;
      $('#search').value = ''; state.query = ''; $('#isolateBtn').hidden = true;
      // key=value (or key:value) filters by that label; a journey's name opens
      // it; a node's name shows what it depends on.
      const m = /^([^=:\s]+)\s*[=:]\s*(.+)$/.exec(q);
      if (m && m[1].toLowerCase() !== 'from' && m[1] !== state.jkey && m[1] !== 'journey') {
        setFilters({ ...state.filters, facets: { ...state.filters.facets, [m[1]]: [m[2].trim()] } });
        return;
      }
      if (m) q = m[1].toLowerCase() === 'from' ? q : m[2].trim();
      else if (state.snap && !journeyNames(state.snap, state.jkey).includes(q)) {
        const n = state.snap.nodes.find(x => x.id.toLowerCase() === q.toLowerCase());
        if (n) q = 'from:' + n.id;
      }
      setFilters({ ...state.filters, journeys: [q] });
    };
    $('#isolateBtn').addEventListener('click', isolate);
    $('#liveBtn').addEventListener('click', () => {
      state.live = !state.live; renderButtons(); renderLive();
      if (!state.live) return;
      if (state.pending) { const p = state.pending; state.pending = null; apply(p, true); }
      connect();
    });
    $('#fitBtn').addEventListener('click', fit);
    $('#readableBtn').addEventListener('click', focusSelection);
    $('#zoomIn').addEventListener('click', () => { const r = stage.getBoundingClientRect(); zoomAt(1.2, r.width / 2, r.height / 2); });
    $('#zoomOut').addEventListener('click', () => { const r = stage.getBoundingClientRect(); zoomAt(1 / 1.2, r.width / 2, r.height / 2); });
    $('#resetBtn').addEventListener('click', () => { state.positions = {}; store.set(posKey(), {}); topologyKey = ''; renderMap(); fit(); });
    const setMenu = open => { $('#exportMenu').classList.toggle('open', open); $('#exportBtn').setAttribute('aria-expanded', String(open)); };
    $('#exportBtn').addEventListener('click', ev => { ev.stopPropagation(); setMenu(!$('#exportMenu').classList.contains('open')); });
    app.addEventListener('click', ev => {
      setMenu(false);
      if (filterPanelOpen() && !ev.composedPath().some(x => x === $('#filterPanel') || x?.id === 'filterBtn')) setFilterPanel(false);
    });
    app.querySelectorAll('[data-export]').forEach(b => b.addEventListener('click', () => exportMap(b.dataset.export)));
    function viewState() {
      return { version: 1, ...filtersToParams(state.filters), group: state.groupBy, query: state.query, select: state.selected?.id || (!state.restoredSelect && opts.select) || '',
        flow: state.flow, values: state.values, live: state.live, journeyKey: state.journeyKey, slice: state.slice, cluster: state.cluster, states: state.states, signals: [...state.signals], history: state.historyId, expanded: [...state.expanded], investigation: state.investigation, display: state.display, operation: state.operation };
    }
    function syncSharedURL() {
      if (typeof location === 'undefined' || typeof history === 'undefined') return;
      const url = new URL(location.href), key = page ? 'teiaView' : 'teiaView-' + (opts.storageKey || 'panel');
      if (!url.searchParams.has(key)) return;
      const value = JSON.stringify(viewState());
      if (url.searchParams.get(key) !== value) { url.searchParams.set(key, value); history.replaceState(null, '', url); }
    }
    $('#shareBtn').addEventListener('click', () => {
      const url = new URL(location.href);
      url.searchParams.set(page ? 'teiaView' : 'teiaView-' + (opts.storageKey || 'panel'), JSON.stringify(viewState()));
      $('#shareURL').value = url.href;
      $('#shareContext').textContent = page ? 'Restores the filters, grouping, selection and update settings.'
        : 'Restores this panel’s local view. Dashboard variables and the requested time range remain in the dashboard URL.';
      $('#shareStatus').textContent = '';
      $('#shareDialog').showModal(); $('#shareURL').select();
    });
    $('#copyViewBtn').addEventListener('click', async () => {
      try { await navigator.clipboard.writeText($('#shareURL').value); $('#shareStatus').textContent = 'Link copied.'; }
      catch { $('#shareURL').focus(); $('#shareURL').select(); $('#shareStatus').textContent = 'Copy the selected link with Ctrl+C or Command+C.'; }
    });
    $('#closeShareBtn').addEventListener('click', () => $('#shareDialog').close());
    $('#exploreBtn').addEventListener('click', () => openWorkspace('scope'));
    app.querySelectorAll('[data-workspace]').forEach(button => button.addEventListener('click', () => openWorkspace(button.dataset.workspace)));
    $('#closeInvestigateBtn').addEventListener('click', () => $('#investigateDialog').close());
    $('#investigateDialog').addEventListener('close', () => { ++workspaceEpoch; clearComparisonMaps(); $('#exploreBtn').focus(); });
    $('#shareDialog').addEventListener('close', () => $('#exportBtn').focus());
    $('#historyBtn').addEventListener('click', () => { $('#historyDialog').showModal(); listHistory(); });
    $('#historyForm').addEventListener('submit', event => { event.preventDefault(); listHistory(); });
    $('#currentBtn').addEventListener('click', () => replay(''));
    $('#closeHistoryBtn').addEventListener('click', () => $('#historyDialog').close());
    $('#historyDialog').addEventListener('close', () => $('#historyBtn').focus());
    svg.addEventListener('wheel', onWheel, { passive: false });
    svg.addEventListener('pointerdown', onPointerDown);
    svg.addEventListener('pointermove', onPointerMove);
    svg.addEventListener('pointerup', () => endDrag(true));
    svg.addEventListener('pointercancel', () => endDrag(false));
    svg.addEventListener('lostpointercapture', () => endDrag(false));

    const keyTarget = page ? document : host;
    keyTarget.addEventListener('keydown', onKey);
    // Resizing refits the map until the viewer pans or zooms it themselves.
    const resizeObs = new ResizeObserver(() => {
      if (state.snap && !state.userMoved) state.fitted = fit() || state.fitted;
      revealSelection();
    });
    resizeObs.observe(host);
    resizeObs.observe(stage);
    const seenObs = typeof IntersectionObserver !== 'undefined'
      ? new IntersectionObserver(entries => { state.visible = entries.some(e => e.isIntersecting); wake(); }) : null;
    seenObs?.observe(host);
    const onVisibility = () => wake();
    document.addEventListener('visibilitychange', onVisibility);
    const schemeMQ = matchMedia('(prefers-color-scheme: dark)');
    const onScheme = () => { if (state.snap) { renderLegend(); renderAll(); renderActivity(); } };
    schemeMQ.addEventListener('change', onScheme);
    const onMotion = () => { if (store.get('flow', null) == null) { state.flow = !motionMQ.matches; renderButtons(); rebuildParticles(); wake(); } };
    motionMQ.addEventListener('change', onMotion);

    // Shareable views (page mode): ?journey=checkout&group=team&q=kind:database&theme=light&select=orders-db&legend=icons&flow=1&values=0&live=0
    if (page && typeof location !== 'undefined') {
      const params = new URLSearchParams(location.search);
      if (params.has('group')) { state.groupBy = params.get('group'); state.groupChosen = true; }
      if (params.has('q')) { state.query = params.get('q'); $('#search').value = state.query; }
      if (params.has('theme')) host.dataset.theme = params.get('theme');
      if (params.get('live') === '0') state.live = false;
      if (params.has('select')) opts.select = params.get('select');
      if (params.get('legend') === 'icons') { state.legend = true; state.legendIcons = true; }
      else if (params.get('legend') === '0') state.legend = false;
      if (params.has('flow')) state.flow = params.get('flow') !== '0';
      state.filters = filtersFromParams(params);
      if (params.has('journeyKey')) state.journeyKey = params.get('journeyKey').trim();
      if (params.has('values')) state.values = params.get('values') !== '0';
      if (params.has('slice')) state.slice = params.get('slice').trim();
      if (params.has('cluster')) state.cluster = params.get('cluster').trim();
    }
    if (opts.journey) state.filters.journeys = [String(opts.journey).trim()];
    let hostJourney = opts.journey || '';
    const hostOptions = { journeyKey: opts.journeyKey || '', groupBy: opts.groupBy || '', flow: opts.flow, values: opts.values };
    try {
      const raw = new URLSearchParams(location.search).get(page ? 'teiaView' : 'teiaView-' + (opts.storageKey || 'panel'));
      const view = raw && raw.length < 16000 ? JSON.parse(raw) : null;
      if (view?.version === 1) {
        state.filters = filtersFromParams(new URLSearchParams({ journey: text(view.journey), filter: text(view.filter) }));
        state.groupBy = text(view.group); state.groupChosen = true; state.query = text(view.query); $('#search').value = state.query;
        state.journeyKey = text(view.journeyKey); opts.select = text(view.select); state.slice = text(view.slice);
        if (opts.api && text(view.cluster)) state.cluster = text(view.cluster);
        if (opts.api) state.historyId = text(view.history);
        state.expanded = new Set(strings(view.expanded));
        state.investigation = { mode: oneOf(view.investigation?.mode, ['all','neighbors','path'], 'all'), focus: text(view.investigation?.focus), target: text(view.investigation?.target), depth: Math.max(1, Math.min(3, finite(view.investigation?.depth) || 1)), direction: oneOf(view.investigation?.direction, ['both','in','out'], 'both') };
        state.display = oneOf(view.display, ['graph','list'], 'graph');
        state.operation = { id: text(view.operation?.id), traceId: text(view.operation?.traceId) };
        for (const key of ['flow', 'values', 'live']) if (typeof view[key] === 'boolean') state[key] = view[key];
        for (const key of ['ok', 'ghost', 'stale']) if (typeof view.states?.[key] === 'boolean') state.states[key] = view.states[key];
        state.signals = new Set(strings(view.signals).filter(s => has(SIGNAL, s)));
      }
    } catch { /* invalid shared views leave the normal dashboard controls available */ }
    state.positions = stored(posKey(), {});
    if (opts.theme) host.dataset.theme = opts.theme;

    // API links only make sense when teia's API is reachable from the browser.
    app.querySelectorAll('[data-api]').forEach(a => { if (!opts.api) a.remove(); });
    refreshApiLinks();
    $('#stepsBtn').addEventListener('click', () => { const open = !$('aside').classList.contains('open'); setDrawer(open); if (open) showTab('steps'); });
    $('#drawerClose').addEventListener('click', () => { setDrawer(false); $('#stepsBtn').focus(); });
    setDrawer(false);
    if (!page) host.tabIndex = 0;

    initSVG();
    renderLegend();
    renderButtons();
    renderLive();
    const ticker = setInterval(renderLive, 1000);
    if (opts.api) {
      $('#clusterPick').addEventListener('change', ev => setCluster(ev.currentTarget.value));
      fetch(opts.api + 'clusters', { signal: aborter.signal, headers: { Accept: 'application/json' } })
        .then(r => r.ok ? r.json() : null)
        .then(list => {
          if (destroyed || !Array.isArray(list?.clusters)) return;
          state.clusters = list.clusters.map(c => String(c?.name || '')).filter(Boolean);
          renderCluster();
        })
        .catch(() => { /* an older server maps one scope */ });
      load(aborter.signal)
        .then(() => { if (!destroyed && opts.select) locate(opts.select, { move: false }); })
        .catch(() => { /* the stream will bring it */ })
        .finally(() => { if (destroyed) return; if (opts.stream !== false) connect(); else { state.live = false; renderButtons(); renderLive(); } });
    }

    return {
      // update feeds a snapshot from outside (the Grafana panel).
      update(snap, changes) { if (apply(snap, true) && changes) pushActivity(changes, state.snap.timeMode !== 'historical' && state.activity.length > 0); },
      focus(subject) { if (state.snap) locate(subject); },
      // setStatus reports the host's own query state (the panel's data.state).
      setStatus(error) { state.error = error || ''; renderLive(); },
      setOptions(options = {}) {
        for (const key of ['inputFrames', 'fieldMapping', 'onMappingChange', 'onSliceChange']) if (has(options, key)) opts[key] = options[key];
        if (has(options, 'onSliceChange') && state.snap) renderScope();
        const { groupBy, flow, journey, journeyKey, values } = options;
        if (has(options, 'requestedRange')) { opts.requestedRange = options.requestedRange; renderLive(); }
        // A journey from the host (a dashboard variable) is the viewer's
        // choice; it applies when it changes, keeping filters picked since.
        if (journey != null && journey !== hostJourney) { hostJourney = journey; setJourney(journey); }
        if (journeyKey != null && journeyKey !== hostOptions.journeyKey) { state.journeyKey = journeyKey; topologyKey = ''; if (state.snap) { renderAll(); state.fitted = fit(); } }
        if (values != null && values !== hostOptions.values) { state.values = values; renderButtons(); if (state.snap) renderReasons(); }
        if (groupBy != null && groupBy !== hostOptions.groupBy) { state.groupBy = groupBy; state.groupChosen = !!groupBy || store.get('groupBy', null) !== null; state.autoGroup = ''; state.expanded.clear(); if (state.snap) { renderGroupBy(); renderMap(); fit(); } }
        if (flow != null && flow !== hostOptions.flow) { state.flow = flow; renderButtons(); rebuildParticles(); wake(); }
        for (const key of Object.keys(hostOptions)) if (options[key] != null) hostOptions[key] = options[key];
      },
      setTheme(theme) { host.dataset.theme = theme; renderLegend(); if (state.snap) { renderAll(); renderActivity(); } },
      destroy() {
        if (destroyed) return;
        destroyed = true;
        clearComparisonMaps();
        aborter.abort();
        cancelAnimationFrame(raf);
        clearInterval(ticker);
        timers.forEach(clearTimeout);
        timers.clear();
        clearTimeout(searchTimer);
        resizeObs.disconnect();
        seenObs?.disconnect();
        document.removeEventListener('visibilitychange', onVisibility);
        schemeMQ.removeEventListener('change', onScheme);
        motionMQ.removeEventListener('change', onMotion);
        keyTarget.removeEventListener('keydown', onKey);
        if (es) { es.close(); es = null; }
        app.remove();
      },
    };
  }

  const TeiaUI = { mount, core };
  if (typeof module === 'object' && module.exports) module.exports = TeiaUI;
  else global.TeiaUI = TeiaUI;
})(typeof window !== 'undefined' ? window : globalThis);
