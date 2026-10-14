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
  --bg: #020a33; --bg-2: #020f59; --panel: #0a155f; --panel-2: #0d1a70; --line: rgba(255, 255, 255, .10); --line-2: rgba(255, 255, 255, .16);
  --ink: #f7f8ff; --ink-2: #c9d0ee; --muted: #9aa6d6; --accent: #a9f549; --accent-2: #005aff; --accent-ink: #020f59;
  --healthy: #46d18a; --degraded: #ffce4a; --failing: #ff6b6b; --unknown: #7d8ac2;
  --ghost: #ffce4a; --stale: #7d8ac2; --edge: #5b94ff; --particle: #a9f549;
  --card: #0a155f; --card-hi: #0d1a70; --grid: rgba(122, 162, 255, .07);
  --shadow: 0 10px 30px rgba(1, 5, 30, .55);
  --critical: #ff6b6b; --warning: #ffce4a; --info: #5b94ff;
  --ok-text: #46d18a; --warn-text: #ffce4a; --err-text: #ff6b6b;
  --radius: 14px;
  color-scheme: dark;
}
@media (prefers-color-scheme: light) {
  :host(:not([data-theme="dark"])) {
    --bg: #f7f8ff; --bg-2: #eef0fb; --panel: #ffffff; --panel-2: #f7f8ff; --line: #dfe3f5; --line-2: #c9d0ee;
    --ink: #1e1917; --ink-2: #3a3f5c; --muted: #5d6690; --accent: #005aff; --accent-2: #005aff; --accent-ink: #ffffff;
    --healthy: #1f9d63; --degraded: #d99a00; --failing: #e04848; --unknown: #7d8ac2;
    --ghost: #d99a00; --stale: #9aa6d6; --edge: #8fb0ff; --particle: #005aff;
    --card: #ffffff; --card-hi: #eef0fb; --grid: rgba(0, 90, 255, .06);
    --shadow: 0 8px 24px rgba(2, 15, 89, .10);
    --critical: #e04848; --warning: #d99a00; --info: #005aff;
    --ok-text: #18804f; --warn-text: #8a5a00; --err-text: #c22f2f;
    color-scheme: light;
  }
}
:host([data-theme="light"]) {
  --bg: #f7f8ff; --bg-2: #eef0fb; --panel: #ffffff; --panel-2: #f7f8ff; --line: #dfe3f5; --line-2: #c9d0ee;
  --ink: #1e1917; --ink-2: #3a3f5c; --muted: #5d6690; --accent: #005aff; --accent-2: #005aff; --accent-ink: #ffffff;
  --healthy: #1f9d63; --degraded: #d99a00; --failing: #e04848; --unknown: #7d8ac2;
  --ghost: #d99a00; --stale: #9aa6d6; --edge: #8fb0ff; --particle: #005aff;
  --card: #ffffff; --card-hi: #eef0fb; --grid: rgba(0, 90, 255, .06);
  --shadow: 0 8px 24px rgba(2, 15, 89, .10);
  --critical: #e04848; --warning: #d99a00; --info: #005aff;
  --ok-text: #18804f; --warn-text: #8a5a00; --err-text: #c22f2f;
  color-scheme: light;
}

* { box-sizing: border-box; }
:host { display: block; height: 100%; container: teia / inline-size; }
[hidden] { display: none !important; }
.teia {
  height: 100%; position: relative;
  background: var(--bg); color: var(--ink);
  font: 13px/1.45 "teia-outfit", Outfit, ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  display: grid; grid-template-rows: auto 1fr; overflow: hidden;
}
button, input, select { font: inherit; color: inherit; }
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.search input:focus-visible, #map :focus-visible { outline: none; }
.num { font-variant-numeric: tabular-nums; }

/* ---- top bar ---------------------------------------------------------- */
header {
  display: flex; align-items: center; gap: 18px; padding: 10px 16px;
  background: linear-gradient(180deg, var(--panel), var(--bg-2)); border-bottom: 1px solid var(--line);
  min-height: 62px; flex-wrap: wrap;
}
.brand { display: flex; align-items: center; gap: 10px; margin-right: 6px; }
.brand svg { width: 30px; height: 30px; }
.brand b { font-size: 17px; letter-spacing: .02em; }
.brand small { display: block; color: var(--muted); font-size: 11px; margin-top: -2px; }
.rings { display: flex; gap: 14px; }
.ring { display: flex; align-items: center; gap: 8px; }
.ring svg { width: 38px; height: 38px; transform: rotate(-90deg); }
.ring .track { stroke: var(--line); }
.ring .value { transition: stroke-dasharray .6s ease; }
.ring b { font-size: 15px; display: block; }
.ring span { color: var(--muted); font-size: 11px; }
.spacer { flex: 1; }
.live { display: flex; align-items: center; gap: 8px; color: var(--ink-2); font-size: 12px; white-space: nowrap; }
.live i { width: 8px; height: 8px; border-radius: 50%; background: var(--unknown); }
.live.on i { background: var(--healthy); animation: beat 2s infinite; }
.live.paused i { background: var(--degraded); }
.live.error i { background: var(--failing); }
.live.error span { color: var(--err-text); }
@keyframes beat { 0% { box-shadow: 0 0 0 0 rgba(70, 209, 138, .55); } 70% { box-shadow: 0 0 0 8px rgba(70, 209, 138, 0); } 100% { box-shadow: 0 0 0 0 rgba(70, 209, 138, 0); } }
.search { position: relative; }
.search input {
  width: 230px; background: var(--bg); border: 1px solid var(--line-2); border-radius: 10px;
  padding: 7px 10px 7px 30px; outline: none;
}
.search input:focus { border-color: var(--accent); }
.search svg { position: absolute; left: 9px; top: 9px; width: 14px; height: 14px; color: var(--muted); }
.ctl { display: flex; gap: 6px; align-items: center; }
.btn {
  display: inline-flex; align-items: center; gap: 6px; height: 32px; padding: 0 11px; border-radius: 9px;
  background: var(--panel-2); border: 1px solid var(--line-2); cursor: pointer; white-space: nowrap; text-decoration: none; color: var(--ink);
}
.btn:hover { border-color: var(--accent); }
.btn svg { width: 15px; height: 15px; }
.btn.on { background: var(--accent); color: var(--accent-ink); border-color: var(--accent); }
.btn.icon { width: 32px; padding: 0; justify-content: center; }
select.btn { padding-right: 8px; }
.menu { position: relative; }
.menu-list {
  position: absolute; right: 0; top: 38px; z-index: 20; background: var(--panel); border: 1px solid var(--line-2);
  border-radius: 10px; box-shadow: var(--shadow); padding: 4px; min-width: 210px; display: none;
}
.menu.open .menu-list { display: block; }
.menu-list button, .menu-list a {
  display: block; width: 100%; text-align: left; padding: 8px 10px; border: 0; background: none; border-radius: 7px;
  cursor: pointer; color: var(--ink); text-decoration: none;
}
.menu-list button:hover, .menu-list a:hover { background: var(--panel-2); }

/* ---- layout ----------------------------------------------------------- */
main { display: grid; grid-template-columns: minmax(0, 1fr) 410px; min-height: 0; }
#stage { position: relative; overflow: hidden; background: var(--bg); touch-action: none; }
#stage::before {
  content: ""; position: absolute; inset: 0; pointer-events: none;
  background-image: radial-gradient(var(--grid) 1.2px, transparent 1.3px);
  background-size: var(--grid-size, 24px) var(--grid-size, 24px);
  background-position: var(--grid-x, 0) var(--grid-y, 0);
}
#map { position: absolute; inset: 0; width: 100%; height: 100%; cursor: grab; user-select: none; }
#map.panning { cursor: grabbing; }

.floating {
  position: absolute; z-index: 5; background: color-mix(in srgb, var(--panel) 92%, transparent);
  border: 1px solid var(--line); border-radius: var(--radius); box-shadow: var(--shadow); backdrop-filter: blur(6px);
}
.filters { left: 14px; top: 14px; padding: 8px; display: flex; flex-wrap: wrap; gap: 6px; max-width: calc(100% - 110px); }
.chip {
  display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 9px; border-radius: 999px;
  border: 1px solid var(--line-2); background: transparent; cursor: pointer; font-size: 12px; color: var(--ink-2);
}
.chip .dot { width: 8px; height: 8px; border-radius: 50%; }
.chip .ln { width: 16px; height: 0; border-top: 2px solid; }
.chip b { color: var(--ink); font-weight: 600; }
.chip.off { opacity: .4; }
.chip svg { width: 12px; height: 12px; }
.sep { width: 1px; background: var(--line-2); margin: 3px 2px; }
.legend { left: 14px; bottom: 14px; padding: 10px 12px; font-size: 11.5px; color: var(--ink-2); display: grid; grid-template-columns: repeat(3, auto); gap: 5px 16px; }
.legend .lg { display: flex; align-items: center; gap: 7px; white-space: nowrap; }
.legend .sw { width: 11px; height: 11px; border-radius: 3px; }
.legend .ln { width: 20px; height: 0; border-top: 2px solid var(--ink-2); }
.legend .bx { width: 16px; height: 11px; border: 1.5px solid var(--ink-2); border-radius: 3px; }
.legend h4 { margin: 0 0 2px; font-size: 10px; letter-spacing: .08em; text-transform: uppercase; color: var(--muted); grid-column: span 1; }
.legend h4.span3 { grid-column: 1 / -1; margin-top: 6px; }
.legend .lg.icon svg { width: 16px; height: 16px; color: var(--ink); flex: none; }
.legend-toggle { grid-column: 1 / -1; justify-self: start; background: none; border: 0; color: var(--accent); cursor: pointer; padding: 2px 0 0; font-size: 11px; }
.zoom { right: 14px; bottom: 14px; display: flex; flex-direction: column; padding: 4px; gap: 4px; }
.zoom .btn { width: 30px; height: 30px; padding: 0; justify-content: center; }
.toasts { position: absolute; right: 14px; top: 14px; z-index: 6; display: flex; flex-direction: column; gap: 8px; width: 330px; pointer-events: none; }
.toast {
  background: var(--panel); border: 1px solid var(--line-2); border-left: 3px solid var(--info); border-radius: 10px;
  padding: 9px 12px; box-shadow: var(--shadow); font-size: 12.5px; animation: toast-in .25s ease-out; pointer-events: auto;
  text-align: left; color: var(--ink); cursor: pointer;
}
.toast.warn { border-left-color: var(--degraded); } .toast.bad { border-left-color: var(--failing); } .toast.good { border-left-color: var(--healthy); }
.toast small { color: var(--muted); display: block; font-size: 11px; }
@keyframes toast-in { from { transform: translateY(-6px); opacity: 0; } to { transform: none; opacity: 1; } }
.empty-state { position: absolute; inset: 0; display: grid; place-items: center; padding: 24px; text-align: center; color: var(--muted); pointer-events: none; }

/* ---- side panel ------------------------------------------------------- */
aside { background: var(--panel); border-left: 1px solid var(--line); display: grid; grid-template-rows: auto 1fr; min-height: 0; }
.tabs { display: flex; border-bottom: 1px solid var(--line); padding: 0 8px; }
.tab {
  padding: 12px 10px; border: 0; background: none; cursor: pointer; color: var(--muted); border-bottom: 2px solid transparent;
  font-weight: 600; font-size: 12.5px; display: flex; gap: 6px; align-items: center;
}
.tab.on { color: var(--ink); border-bottom-color: var(--accent); }
.tab .count { background: var(--panel-2); border: 1px solid var(--line-2); border-radius: 999px; padding: 0 6px; font-size: 11px; color: var(--ink-2); }
.pane { overflow-y: auto; padding: 14px 16px 30px; display: none; }
.pane.on { display: block; }
.pane h3 { font-size: 11px; letter-spacing: .08em; text-transform: uppercase; color: var(--muted); margin: 18px 0 8px; }
.pane h3:first-child { margin-top: 2px; }
.card-f {
  border: 1px solid var(--line); border-left: 3px solid var(--info); border-radius: 10px; padding: 10px 12px; margin-bottom: 10px;
  background: var(--panel-2);
}
.card-head { display: block; width: 100%; padding: 0; border: 0; background: none; text-align: left; cursor: pointer; }
.card-f:hover { border-color: var(--line-2); }
.card-f.critical { border-left-color: var(--critical); } .card-f.warning { border-left-color: var(--warning); }
.card-f .meta { display: block; font-size: 10.5px; color: var(--muted); text-transform: uppercase; letter-spacing: .06em; }
.card-f .h4 { display: block; margin: 3px 0 5px; font-size: 13.5px; font-weight: 600; }
.card-f p { margin: 0 0 6px; color: var(--ink-2); }
.card-f .next { color: var(--ink); }
.snippet { position: relative; }
.snippet pre {
  background: var(--bg); border: 1px solid var(--line); border-radius: 8px; padding: 9px 10px; overflow-x: auto;
  font: 11.5px/1.5 "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; margin: 6px 0 0; color: var(--ink);
}
.snippet .copy { position: absolute; right: 6px; top: 6px; height: 24px; font-size: 11px; padding: 0 8px; }
details summary { cursor: pointer; color: var(--accent); font-size: 12px; margin-top: 4px; }
.hero { display: flex; gap: 12px; align-items: center; margin-bottom: 12px; }
.hero .tile { width: 46px; height: 46px; border-radius: 12px; display: grid; place-items: center; flex: none; }
.hero .tile svg { width: 26px; height: 26px; color: #020f59; }
.hero h2 { margin: 0; font-size: 18px; word-break: break-all; }
.why { display: grid; grid-template-columns: 22px 1fr auto; gap: 8px; align-items: center; padding: 8px 10px; margin-bottom: 6px;
  border-radius: 10px; border: 1px solid var(--line); border-left: 3px solid var(--degraded); background: var(--panel-2); }
.why.failing { border-left-color: var(--failing); }
.why svg { width: 18px; height: 18px; }
.why.degraded svg { color: var(--warn-text); } .why.failing svg { color: var(--err-text); }
.why .btn { height: 26px; font-size: 11.5px; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
.search .isolate { position: absolute; right: 4px; top: 3px; height: 24px; padding: 0 8px; font-size: 11.5px; }
.journey-chip { display: inline-flex; align-items: center; gap: 8px; height: 26px; padding: 0 4px 0 10px; border-radius: 999px;
  background: var(--accent); color: var(--accent-ink); font-size: 12px; font-weight: 600; }
.journey-chip svg { width: 13px; height: 13px; }
.journey-chip .why { font-weight: 500; display: inline-flex; gap: 5px; align-items: center; }
.journey-chip button { height: 20px; min-width: 20px; border: 0; border-radius: 999px; background: rgba(2, 15, 89, .16); color: inherit; cursor: pointer; padding: 0 6px; font-size: 11.5px; }
.legend .reasons { grid-column: 1 / -1; display: flex; flex-wrap: wrap; gap: 4px 12px; }
.legend .reasons svg { width: 14px; height: 14px; color: var(--ink); flex: none; }
.pill { display: inline-flex; align-items: center; gap: 5px; border-radius: 999px; padding: 1px 8px; font-size: 11px; border: 1px solid var(--line-2); color: var(--ink-2); }
.pill .dot { width: 7px; height: 7px; border-radius: 50%; }
.kv { display: grid; grid-template-columns: 130px 1fr; gap: 6px 10px; }
.kv dt { color: var(--muted); }
.kv dd { margin: 0; word-break: break-word; }
.labels { display: flex; flex-wrap: wrap; gap: 5px; }
.label { font-size: 11px; border: 1px solid var(--line-2); border-radius: 6px; padding: 1px 6px; background: var(--bg); color: var(--ink); }
button.label { cursor: pointer; }
.label b { color: var(--muted); font-weight: 500; }
.rel { display: flex; align-items: center; gap: 8px; width: 100%; padding: 6px 8px; border: 0; border-radius: 8px; background: none; text-align: left; cursor: pointer; }
.rel:hover { background: var(--panel-2); }
.rel .dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.rel span { color: var(--muted); margin-left: auto; font-size: 11.5px; }
.explain { color: var(--ink-2); margin: 4px 0 10px; }
.feed .ev {
  display: grid; grid-template-columns: 58px 18px 1fr; gap: 6px; padding: 7px 4px; align-items: start; width: 100%;
  border: 0; border-bottom: 1px solid var(--line); background: none; text-align: left; cursor: pointer;
}
.feed .ev:hover { background: var(--panel-2); }
.feed .ev time { color: var(--muted); font-size: 11.5px; }
.feed .ev svg { width: 15px; height: 15px; margin-top: 2px; }
.feed .ev.new { animation: toast-in .4s ease-out; }
.src { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; padding: 8px 0; border-bottom: 1px solid var(--line); }
.src small { display: block; color: var(--muted); }
.state { font-size: 10.5px; text-transform: uppercase; letter-spacing: .05em; border-radius: 6px; padding: 1px 6px; border: 1px solid var(--line-2); white-space: nowrap; }
.state.ok { color: var(--ok-text); } .state.degraded { color: var(--warn-text); } .state.error { color: var(--err-text); }
.muted { color: var(--muted); }

@container teia (max-width: 960px) {
  .teia.page { overflow: auto; }
  .page main { grid-template-columns: 1fr; grid-template-rows: 64vh auto; }
  .page aside { border-left: 0; border-top: 1px solid var(--line); }
  .page .legend { display: none; }
  .search input { width: 160px; }
}
@container teia (max-width: 640px) {
  .panel .rings, .panel .legend { display: none; }
  .panel .search input { width: 120px; }
}

@media (prefers-reduced-motion: reduce) {
  .live.on i, .toast, .feed .ev.new { animation: none; }
  .ring .value, .teia.panel aside { transition: none; }
}

/* ---- panel mode (inside Grafana): compact bar, drawer instead of column -- */
.teia.panel { grid-template-rows: auto 1fr; }
.teia.panel header { min-height: 44px; padding: 6px 10px; gap: 10px; background: transparent; border-bottom: 1px solid var(--line); }
.teia.panel .brand { display: none; }
.teia.panel .ring svg { width: 28px; height: 28px; }
.teia.panel .ring b { font-size: 13px; }
.teia.panel .search input { width: 180px; padding-top: 5px; padding-bottom: 5px; }
.teia.panel .btn { height: 28px; }
.teia.panel main { grid-template-columns: minmax(0, 1fr); position: relative; }
.teia.panel aside {
  position: absolute; right: 0; top: 0; bottom: 0; width: min(400px, 90%); z-index: 10;
  transform: translateX(102%); transition: transform .25s ease; box-shadow: var(--shadow);
}
.teia.panel aside.open { transform: none; }
.teia.panel .drawer-close { display: inline-flex; }
.drawer-close { display: none; margin-left: auto; align-self: center; }
.teia.panel .legend { font-size: 11px; padding: 8px 10px; }
.stepsBtn { display: none; }
.teia.panel .stepsBtn { display: inline-flex; }
.stepsBtn .count { background: var(--degraded); color: #020f59; border-radius: 999px; padding: 0 6px; font-size: 11px; font-weight: 700; }
`;

  const MARKUP = `<header>
  <div class="brand">
    <svg viewBox="0 0 128 128" aria-hidden="true"><defs>
    <linearGradient id="h-tile" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#0a1a6e"/><stop offset="1" stop-color="#020f59"/>
    </linearGradient>
    <radialGradient id="h-glow" cx=".5" cy=".5" r=".5">
      <stop offset="0" stop-color="#005aff" stop-opacity=".45"/><stop offset="1" stop-color="#005aff" stop-opacity="0"/>
    </radialGradient>
  </defs>
  <rect width="128" height="128" rx="26" fill="url(#h-tile)"/>
  <circle cx="64" cy="64" r="44" fill="url(#h-glow)"/>
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
    <div><b>teia</b><small>service communication map</small></div>
  </div>
  <div class="rings" id="rings"></div>
  <div class="spacer"></div>
  <div class="live" id="live" title="Live updates from the discovery engine"><i></i><span>connecting…</span></div>
  <label class="search">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7"/><path d="M20 20l-4-4"/></svg>
    <input id="search" type="search" placeholder="Search…  e.g. team:checkout" aria-label="Search the map" autocomplete="off" spellcheck="false">
    <button type="button" class="btn isolate" id="isolateBtn" title="Show only what the search names, and what it calls (Shift+Enter)" hidden>Isolate</button>
  </label>
  <div class="ctl">
    <select class="btn" id="journey" title="Show one journey (J)" aria-label="Show one journey" hidden></select>
    <select class="btn" id="groupBy" title="Group nodes by a label" aria-label="Group nodes by a label"></select>
    <button type="button" class="btn" id="valuesBtn" title="Traffic and latency on every line (V)"></button>
    <button type="button" class="btn" id="flowBtn" title="Animate traffic (space)"></button>
    <button type="button" class="btn" id="liveBtn" title="Follow live updates"></button>
    <button type="button" class="btn stepsBtn" id="stepsBtn" title="Next steps">Next steps <span class="count" id="stepsBadge">0</span></button>
    <div class="menu" id="exportMenu">
      <button type="button" class="btn" id="exportBtn" title="Export" aria-haspopup="true" aria-expanded="false">Export ▾</button>
      <div class="menu-list">
        <button type="button" data-export="png">Map as PNG</button>
        <button type="button" data-export="svg">Map as SVG</button>
        <a data-api="export/matrix.csv" download>Flow matrix (CSV)</a>
        <a data-api="export/matrix.csv?lang=pt-BR" download>Matriz de fluxos (CSV, pt-BR)</a>
        <a data-api="snapshot" target="_blank" rel="noopener">Snapshot (JSON)</a>
      </div>
    </div>
  </div>
</header>
<main>
  <div id="stage">
    <svg id="map" role="group" aria-label="Communication map. Drag to pan, F to fit, slash to search."></svg>
    <div class="empty-state" id="empty"><p>Waiting for the first discovery round…</p></div>
    <div class="floating filters" id="filters"></div>
    <div class="floating legend" id="legend"></div>
    <div class="floating zoom">
      <button type="button" class="btn" id="zoomIn" title="Zoom in" aria-label="Zoom in">+</button>
      <button type="button" class="btn" id="zoomOut" title="Zoom out" aria-label="Zoom out">−</button>
      <button type="button" class="btn" id="fitBtn" title="Fit (F)" aria-label="Fit the map">⤢</button>
      <button type="button" class="btn" id="resetBtn" title="Reset layout" aria-label="Reset layout">↺</button>
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
      <button type="button" class="btn icon drawer-close" id="drawerClose" title="Close" aria-label="Close">✕</button>
    </nav>
    <section class="pane on" role="tabpanel" id="pane-steps"></section>
    <section class="pane" role="tabpanel" id="pane-details"><p class="muted">Click a service, a datastore or a communication on the map.</p></section>
    <section class="pane feed" role="tabpanel" id="pane-activity"><p class="muted">Changes between discovery rounds show up here as they happen.</p></section>
    <section class="pane" role="tabpanel" id="pane-sources"></section>
  </aside>
</main>`;

  /*! Outfit variable font, latin subset. Copyright 2021 The Outfit Project Authors
   *  (https://github.com/Outfitio/Outfit-Fonts). SIL Open Font License 1.1. */
  const OUTFIT = 'data:font/woff2;base64,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';
  let fontLoaded = false;
  function loadFont() {
    if (fontLoaded || typeof FontFace === 'undefined' || !document.fonts) return;
    fontLoaded = true;
    const face = new FontFace('teia-outfit', `url(${OUTFIT})`, { weight: '100 900', display: 'swap' });
    face.load().then(f => document.fonts.add(f)).catch(() => { /* system font fallback */ });
  }

  // ============================================================== core
  // Pure, DOM-free logic: the data model the map draws. It is unit-tested
  // under node (internal/api/ui/teia-ui.test.mjs) through TeiaUI.core.

  const has = (obj, key) => obj != null && Object.prototype.hasOwnProperty.call(obj, key);

  const ICON = {
    service: '<path d="M12 2.8l8 4.6v9.2l-8 4.6-8-4.6V7.4z"/><path d="M12 12l8-4.6M12 12v9.2M12 12L4 7.4"/>',
    database: '<ellipse cx="12" cy="5.5" rx="7.5" ry="2.8"/><path d="M4.5 5.5v13c0 1.6 3.4 2.8 7.5 2.8s7.5-1.2 7.5-2.8v-13"/><path d="M4.5 12c0 1.6 3.4 2.8 7.5 2.8s7.5-1.2 7.5-2.8"/>',
    external: '<circle cx="12" cy="12" r="8.6"/><path d="M3.4 12h17.2M12 3.4c2.4 2.4 3.6 5.3 3.6 8.6s-1.2 6.2-3.6 8.6M12 3.4C9.6 5.8 8.4 8.7 8.4 12s1.2 6.2 3.6 8.6"/>',
    unknown: '<circle cx="12" cy="12" r="8.6"/><path d="M9.6 9.4a2.5 2.5 0 014.8.9c0 1.7-2.4 2.1-2.4 3.6"/><path d="M12 17.1v.1"/>',
    group: '<rect x="3" y="3" width="7.5" height="7.5" rx="1.6"/><rect x="13.5" y="3" width="7.5" height="7.5" rx="1.6"/><rect x="3" y="13.5" width="7.5" height="7.5" rx="1.6"/><rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.6"/>',
  };
  // The teia icon library: one pictogram per category of component, drawn on
  // the same 24px grid with the same stroke, so every map looks alike.
  const CYL = '<ellipse cx="12" cy="5.5" rx="7.5" ry="2.8"/><path d="M4.5 5.5v13c0 1.6 3.4 2.8 7.5 2.8s7.5-1.2 7.5-2.8v-13"/>';
  const CATEGORY = {
    service: { label: 'Microservice', icon: ICON.service },
    frontend: { label: 'Frontend', icon: '<rect x="2.8" y="4" width="18.4" height="16" rx="2.4"/><path d="M2.8 8.6h18.4"/><path d="M5.6 6.3h.1M8 6.3h.1"/><path d="M9.6 12.4l-2.4 2.4 2.4 2.4M14.4 12.4l2.4 2.4-2.4 2.4"/>' },
    gateway: { label: 'Gateway / proxy', icon: '<circle cx="4.5" cy="12" r="2"/><path d="M6.5 12h4l4-6h6.5M10.5 12h10M10.5 12l4 6h6.5"/><path d="M18.5 4l2.5 2-2.5 2M18.5 10l2.5 2-2.5 2M18.5 16l2.5 2-2.5 2"/>' },
    job: { label: 'Job / worker', icon: '<circle cx="12" cy="13.5" r="7.5"/><path d="M12 9.5v4l2.6 2"/><path d="M9.5 2.8h5M12 2.8v3.2"/>' },
    function: { label: 'Function', icon: '<rect x="3" y="3" width="18" height="18" rx="4"/><path d="M8 18l4.2-7.6"/><path d="M9 6.4c1.9 0 2.5 1.1 3.2 2.8L15.6 16c.4.9 1 1.3 1.9 1.1"/>' },
    relational: { label: 'Relational database', icon: CYL + '<path d="M4.5 10.2c0 1.6 3.4 2.8 7.5 2.8s7.5-1.2 7.5-2.8M4.5 14.6c0 1.6 3.4 2.8 7.5 2.8s7.5-1.2 7.5-2.8"/>' },
    document: { label: 'Document database (NoSQL)', icon: CYL + '<path d="M10 10.5c-.9 0-1.3.5-1.3 1.3v1c0 .6-.4 1-1 1 .6 0 1 .4 1 1v1c0 .8.4 1.3 1.3 1.3M14 10.5c.9 0 1.3.5 1.3 1.3v1c0 .6.4 1 1 1-.6 0-1 .4-1 1v1c0 .8-.4 1.3-1.3 1.3"/>' },
    widecolumn: { label: 'Wide-column database', icon: CYL + '<path d="M9.5 8.2v13M14.5 8.2v13"/>' },
    search: { label: 'Search engine', icon: '<ellipse cx="10" cy="5.5" rx="6.5" ry="2.5"/><path d="M3.5 5.5v10c0 1.4 2.9 2.5 6.5 2.5M16.5 5.5v3.6"/><circle cx="16" cy="15" r="3.4"/><path d="M18.5 17.5l2.6 2.6"/>' },
    timeseries: { label: 'Time-series / analytics', icon: '<path d="M3 20.5h18"/><path d="M3.5 16l4.2-5 3.6 3 4.2-7.4 5 5.8"/><circle cx="15.5" cy="6.6" r="1.3"/>' },
    graphdb: { label: 'Graph database', icon: '<circle cx="5.5" cy="6.5" r="2.4"/><circle cx="18.5" cy="6.5" r="2.4"/><circle cx="12" cy="18" r="2.4"/><circle cx="12" cy="10.5" r="1.8"/><path d="M7.6 7.6l2.8 1.8M16.4 7.6l-2.8 1.8M12 12.3v3.3M7.4 7.9l3.4 8.3M16.6 7.9l-3.4 8.3"/>' },
    cache: { label: 'Cache / key-value', icon: '<rect x="5" y="5" width="14" height="14" rx="2.6"/><path d="M9 2.4V5M15 2.4V5M9 19v2.6M15 19v2.6M2.4 9H5M2.4 15H5M19 9h2.6M19 15h2.6"/><path d="M12.8 7.8l-3.2 4.6h2.9l-.7 3.8 3.3-4.7h-2.9z"/>' },
    stream: { label: 'Event stream', icon: '<rect x="2.5" y="7.5" width="4.2" height="9" rx="1.1"/><rect x="8.1" y="7.5" width="4.2" height="9" rx="1.1"/><rect x="13.7" y="7.5" width="4.2" height="9" rx="1.1"/><path d="M19.4 12h2.4M20.4 10.4l1.6 1.6-1.6 1.6"/>' },
    queue: { label: 'Message queue', icon: '<path d="M3 13l2.6-7.6h12.8L21 13v5.4a1.6 1.6 0 01-1.6 1.6H4.6A1.6 1.6 0 013 18.4z"/><path d="M3 13h5.2l1.4 2.4h4.8l1.4-2.4H21"/><path d="M8.5 8.6h7M9.5 10.8h5"/>' },
    storage: { label: 'Object storage', icon: '<ellipse cx="12" cy="6" rx="8" ry="2.6"/><path d="M4 6l1.8 12.6c.2 1.4 2.9 2.4 6.2 2.4s6-1 6.2-2.4L20 6"/><path d="M5 11.4c1.6.9 4.2 1.4 7 1.4s5.4-.5 7-1.4"/>' },
    identity: { label: 'Identity', icon: '<circle cx="8" cy="15.5" r="4.2"/><path d="M11 12.5l8.8-8.8M16.4 7.1l2.4 2.4M14.3 9.2l2 2"/><circle cx="8" cy="15.5" r="1.1"/>' },
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
    traces: { label: 'Traces', icon: '<path d="M3 12h4l3-7 4 14 3-7h4"/>' },
    mesh: { label: 'Mesh', icon: '<circle cx="6" cy="6" r="2.4"/><circle cx="18" cy="6" r="2.4"/><circle cx="12" cy="18" r="2.4"/><path d="M8.2 7.2l2.6 8.4M15.8 7.2l-2.6 8.4M8.4 6h7.2"/>' },
    network: { label: 'Network', icon: '<path d="M4 9h16M4 15h16"/><path d="M8 5l-4 4 4 4M16 11l4 4-4 4"/>' },
    logs: { label: 'Logs', icon: '<path d="M6 3.5h9l3.5 3.5v13.5H6z"/><path d="M9 11h6.5M9 14.5h6.5M9 18h4"/>' },
    database: { label: 'Database', icon: ICON.database },
    metrics: { label: 'Metrics', icon: '<path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/>' },
    inventory: { label: 'Inventory', icon: ICON.group },
    declared: { label: 'Declared', icon: '<path d="M6 3.5h9l3.5 3.5v13.5H6z"/><path d="M9 13.5l2 2 4-4"/>' },
  };
  // Why a card or line is amber or red: one pictogram per golden signal.
  const REASON = {
    errors: { label: 'Errors', icon: '<circle cx="12" cy="12" r="8.6"/><path d="M9 9l6 6M15 9l-6 6"/>' },
    latency: { label: 'Latency', icon: '<circle cx="12" cy="12" r="8.6"/><path d="M12 7v5.2l3.4 2"/>' },
    saturation: { label: 'Saturation', icon: '<path d="M3.8 17a8.2 8.2 0 0116.4 0"/><path d="M12 17l4.6-5.6"/><path d="M3.5 20.5h17"/>' },
    backlog: { label: 'Queue backlog', icon: '<rect x="3" y="3.5" width="18" height="4.6" rx="1.3"/><rect x="3" y="9.7" width="18" height="4.6" rx="1.3"/><rect x="3" y="15.9" width="12" height="4.6" rx="1.3"/>' },
    lag: { label: 'Queue lag', icon: '<path d="M6.5 3h11M6.5 21h11"/><path d="M7.5 3c0 4.5 4.5 5.5 4.5 9s-4.5 4.5-4.5 9M16.5 3c0 4.5-4.5 5.5-4.5 9s4.5 4.5 4.5 9"/><path d="M9.5 19h5"/>' },
    locks: { label: 'Locks', icon: '<rect x="5" y="10.5" width="14" height="10" rx="2.2"/><path d="M8.2 10.5V7.8a3.8 3.8 0 017.6 0v2.7"/><path d="M12 14.6v2"/>' },
    restarts: { label: 'Restarts', icon: '<path d="M20 11.5A8 8 0 006.3 6.3L4 8.6"/><path d="M4 4v4.6h4.6"/><path d="M4 12.5a8 8 0 0013.7 5.2l2.3-2.3"/><path d="M20 20v-4.6h-4.6"/>' },
  };
  // healthText says a node's or line's health in words.
  const healthText = x => x.health === 'unknown' && x.errorsUnknown && x.requestRate > 0 ? 'failures not measured' : HEALTH_LABEL[x.health];
  const HEALTH_LABEL = { healthy: 'healthy', degraded: 'degraded', failing: 'failing', unknown: 'no traffic data' };
  const HEALTH_RANK = { failing: 3, degraded: 2, healthy: 1, unknown: 0 };
  const STATE_LABEL = { ok: 'declared and observed', ghost: 'observed, not declared', stale: 'declared, not observed' };
  const VIS_EXPLAIN = {
    full: 'teia sees it first-hand: its own SDK, sidecar, eBPF probe or database explorer reports it.',
    partial: 'teia only knows it through what its peers report. Whatever it calls, nobody tells teia.',
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
  // measures are readings beyond traffic: connections in use, sessions
  // waiting on locks, consumer lag.
  const measures = v => (Array.isArray(v) ? v : [])
    .filter(m => m && has(REASON, m.cause) && typeof m.what === 'string' && m.what)
    .map(m => ({ cause: m.cause, what: m.what, value: finite(m.value), max: finite(m.max), unit: oneOf(m.unit, ['bytes', 'seconds', 'share'], ''), via: text(m.via) }));
  // reasons keeps the ones the map knows how to draw, worst first.
  const reasons = v => (Array.isArray(v) ? v : [])
    .filter(r => r && has(REASON, r.cause) && (r.health === 'degraded' || r.health === 'failing'))
    .map(r => ({ cause: r.cause, health: r.health, value: finite(r.value), limit: finite(r.limit), summary: text(r.summary), what: text(r.what), via: text(r.via) }))
    .sort((a, b) => HEALTH_RANK[b.health] - HEALTH_RANK[a.health]);

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
        kind: typeof n.kind === 'string' && n.kind ? n.kind : 'unknown',
        health: oneOf(n.health, ['healthy', 'degraded', 'failing', 'unknown'], 'unknown'),
        visibility: oneOf(n.visibility, ['full', 'partial', 'none'], 'partial'),
        requestRate: finite(n.requestRate), errorRate: finite(n.errorRate), errorsUnknown: n.errorsUnknown === true,
        latencyP50Ms: finite(n.latencyP50Ms), latencyP95Ms: finite(n.latencyP95Ms),
        reasons: reasons(n.reasons), measures: measures(n.measures),
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
        state: oneOf(String(e.state || '').toLowerCase(), ['ok', 'ghost', 'stale'], e.declared === false ? 'ghost' : 'ok'),
        health: oneOf(e.health, ['healthy', 'degraded', 'failing', 'unknown'], 'unknown'),
        requestRate: finite(e.requestRate), errorRate: finite(e.errorRate), errorsUnknown: e.errorsUnknown === true,
        latencyP50Ms: finite(e.latencyP50Ms), latencyP95Ms: finite(e.latencyP95Ms),
        reasons: reasons(e.reasons), measures: measures(e.measures),
        declared: !!e.declared, observed: e.observed !== false,
        signals: strings(e.signals), sources: strings(e.sources),
      });
    }
    const cov = s.coverage && typeof s.coverage === 'object' ? s.coverage : {};
    return {
      round: Number.isFinite(s.round) ? s.round : 0,
      generatedAt: typeof s.generatedAt === 'string' ? s.generatedAt : new Date().toISOString(),
      nodes, edges,
      findings: (Array.isArray(s.findings) ? s.findings : []).filter(f => f && typeof f.title === 'string').map(f => ({
        ...f, severity: oneOf(f.severity, ['critical', 'warning', 'info'], 'info'), subject: String(f.subject || ''),
      })),
      sources: (Array.isArray(s.sources) ? s.sources : []).filter(x => x && typeof x.name === 'string'),
      coverage: { visibility: finite(cov.visibility), declared: finite(cov.declared), observed: finite(cov.observed) },
      // Data without intent (plain Node Graph frames) has no drift to show.
      drift: s.drift !== false,
      changes: Array.isArray(s.changes) ? s.changes : [],
    };
  }

  // signature identifies what is drawn, so refreshes with the same data are no-ops.
  function signature(snap) {
    return JSON.stringify([snap.nodes, snap.edges, snap.findings, snap.sources, snap.coverage]);
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

  function labelKeys(snap) {
    const keys = new Map();
    for (const n of snap.nodes) for (const k of Object.keys(n.labels || {})) if (userLabel(k)) keys.set(k, (keys.get(k) || 0) + 1);
    return [...keys.entries()].filter(([, c]) => c > 1).map(([k]) => k).sort();
  }

  // latencySum folds the latencies of several edges or nodes into one, each
  // weighted by its traffic: an approximation, since percentiles do not add
  // up, but the one Grafana and Kiali make too. Members that measured no
  // latency do not count.
  function latencySum() {
    let weight = 0, p50 = 0, p95 = 0;
    return {
      add(x) {
        if (!(x.latencyP95Ms > 0)) return;
        const w = Math.max(x.requestRate || 0, 1e-9);
        weight += w; p50 += w * (x.latencyP50Ms || 0); p95 += w * x.latencyP95Ms;
      },
      result: () => weight > 0 ? { latencyP50Ms: p50 / weight, latencyP95Ms: p95 / weight } : { latencyP50Ms: 0, latencyP95Ms: 0 },
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
  const fmtValue = (v, unit) => unit === 'bytes' ? fmtBytes(v) : unit === 'seconds' ? fmtSec(v) : unit === 'share' ? pct(v) : compact(v);

  // fmtMs writes milliseconds the way people read them, like teia's CLI.
  function fmtMs(ms) {
    if (ms >= 999.5) return (ms / 1000).toFixed(1) + ' s';
    if (ms >= 9.95) return ms.toFixed(0) + ' ms';
    return ms.toFixed(1) + ' ms';
  }
  const fmtRate = r => r >= 100 ? r.toFixed(0) : r >= 10 ? r.toFixed(1) : r.toFixed(2);
  const pct = v => (v * 100).toFixed(v > 0 && v < 0.1 ? 1 : 0) + '%';

  // compact writes 1200 as 1.2k and 45000 as 45k.
  function compact(n) {
    if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 0 : 1) + 'M';
    if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e4 ? 0 : 1) + 'k';
    return String(Math.round(n));
  }

  // reasonTag is the few words a card or line shows for its worst reason:
  // what crossed its limit, and where it most likely comes from.
  function reasonTag(r, more = 0) {
    const what = {
      errors: () => `${pct(r.value)} errors`,
      latency: () => `p95 ${fmtMs(r.value)}`,
      saturation: () => `${pct(r.value)} ${r.what || 'used'}`,
      backlog: () => `${compact(r.value)} ${r.what || 'waiting'}`,
      lag: () => `${fmtSec(r.value)} ${r.what || 'behind'}`,
      locks: () => `${compact(r.value)} on locks`,
      restarts: () => `${compact(r.value)} restarts`,
    }[r.cause]();
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
    if (n.stubOf) return `${n.stubOf.length} more callers`;
    const what = n.stub ? 'outside the journey' : n.group ? `${n.members.length} workload${n.members.length === 1 ? '' : 's'}` : techLabel(n);
    const stats = [];
    if (n.requestRate > 0) stats.push(`${fmtRate(n.requestRate)}/s`);
    if (n.latencyP95Ms > 0) stats.push(fmtMs(n.latencyP95Ms));
    if (n.errorsUnknown && n.requestRate > 0 && !n.group) stats.push('err ?');
    else if (n.errorRate > 0.0005 && !n.group) stats.push(`${pct(n.errorRate)} err`);
    let text = [what, ...stats].join(' · ');
    if (text.length > max && stats.length) text = stats.join(' · ');
    return text.length > max ? text.slice(0, max - 1) + '…' : text;
  }

  // ---------------------------------------------------------- journeys
  // A journey is a path through the map: the services that take part in it
  // (by a label, "journey" unless said otherwise), what they call
  // downstream, and stubs where it is entered from or leaves to the rest.

  const JOURNEY_KEY = 'journey';

  // parseJourney reads "checkout" (a value of the journey label),
  // "team=checkout" or "team:checkout" (any label), or "from:orders" (what
  // a node depends on).
  function parseJourney(spec) {
    const text = String(spec || '').trim();
    if (!text) return null;
    const m = /^([^=:\s]+)\s*[=:]\s*(.+)$/.exec(text);
    if (!m) return { text, key: JOURNEY_KEY, value: text };
    if (m[1].toLowerCase() === 'from') return { text, entry: m[2].trim() };
    return { text, key: m[1], value: m[2].trim() };
  }

  // labelValues splits a label's value on commas: a service can take part in
  // several journeys ("checkout,browse").
  const labelValues = (n, key) => String(n?.labels?.[key] ?? '').split(',').map(v => v.trim().toLowerCase()).filter(Boolean);
  const isService = n => !!n && n.kind === 'service';

  // journeyOf expands a journey over a snapshot. Members are the nodes the
  // spec names; from them it follows calls downstream with plain rules:
  // datastores, caches, queues and externals are included but never walked
  // past (their other clients are not part of this journey); a service that
  // belongs to another value of the same label is where the journey leaves;
  // unlabelled services are followed maxDepth hops past the last member.
  function journeyOf(snap, spec, { maxDepth = 2, maxNodes = 200 } = {}) {
    const j = typeof spec === 'string' ? parseJourney(spec) : spec;
    const roles = new Map(); // id -> 'member' | 'dependency'
    if (!j) return { spec: j, roles, truncated: false, empty: true };
    const nodes = new Map(snap.nodes.map(n => [n.id, n]));
    const out = new Map();
    for (const e of snap.edges) {
      if (!out.has(e.from)) out.set(e.from, []);
      out.get(e.from).push(e.to);
    }
    const want = j.entry ? null : j.value.toLowerCase();
    const member = n => !!n && (j.entry ? n.id.toLowerCase() === j.entry.toLowerCase() : labelValues(n, j.key).includes(want));
    const elsewhere = n => !j.entry && isService(n) && labelValues(n, j.key).length > 0 && !member(n);
    const queue = [];
    for (const n of snap.nodes) if (member(n)) { roles.set(n.id, 'member'); queue.push([n.id, 0]); }
    let truncated = false;
    for (let q = 0; q < queue.length; q++) {
      const [id, depth] = queue[q];
      if (!isService(nodes.get(id))) continue; // included, not walked past
      for (const to of [...new Set(out.get(id) || [])].sort()) {
        if (roles.has(to)) continue;
        const n = nodes.get(to);
        if (elsewhere(n)) continue; // owned by another journey: a stub
        const next = member(n) ? 0 : depth + 1;
        if (isService(n) && next > maxDepth) continue;
        if (roles.size >= maxNodes) { truncated = true; break; }
        roles.set(to, member(n) ? 'member' : 'dependency');
        queue.push([to, next]);
      }
    }
    return { spec: j, roles, truncated, empty: roles.size === 0 };
  }

  // journeySnapshot keeps the journey and, as stubs, the nodes right outside
  // it: who calls into it and where it leaves to. A node called from more
  // than three outsiders keeps its two busiest and one "+N" stub for the rest.
  function journeySnapshot(snap, journey) {
    const inside = id => journey.roles.has(id);
    const callers = new Map(); // inside id -> outside caller edges
    const keep = new Set();
    for (const e of snap.edges) {
      if (inside(e.from) && !inside(e.to)) keep.add(e.to);
      if (!inside(e.from) && inside(e.to)) {
        if (!callers.has(e.to)) callers.set(e.to, []);
        callers.get(e.to).push(e);
      }
    }
    const extra = [];
    for (const [to, list] of callers) {
      list.sort((a, b) => b.requestRate - a.requestRate || (a.from < b.from ? -1 : 1));
      list.slice(0, list.length > 3 ? 2 : 3).forEach(e => keep.add(e.from));
      if (list.length > 3) {
        const rest = list.slice(2);
        const id = `+${rest.length} callers of ${to}`;
        extra.push({
          node: { id, kind: 'unknown', health: 'unknown', visibility: 'none', requestRate: 0, errorRate: 0, latencyP50Ms: 0, latencyP95Ms: 0, reasons: [], measures: [], sources: [], addresses: [], labels: {}, stub: true, stubOf: rest.map(e => e.from) },
          edge: { ...rest[0], from: id, to, requestRate: rest.reduce((a, e) => a + e.requestRate, 0), reasons: [], measures: [], stubEdge: true },
        });
      }
    }
    const role = id => journey.roles.get(id);
    const nodes = snap.nodes
      .filter(n => inside(n.id) || keep.has(n.id))
      .map(n => inside(n.id) ? { ...n, journeyRole: role(n.id) } : { ...n, stub: true });
    const edges = snap.edges
      .filter(e => (inside(e.from) || keep.has(e.from)) && (inside(e.to) || keep.has(e.to)) && (inside(e.from) || inside(e.to)))
      .map(e => inside(e.from) && inside(e.to) ? e : { ...e, stubEdge: true });
    return { ...snap, nodes: [...nodes, ...extra.map(x => x.node)], edges: [...edges, ...extra.map(x => x.edge)] };
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

  // journeyNames lists the journeys the label names, for the selector.
  function journeyNames(snap, key = JOURNEY_KEY) {
    const names = new Set();
    for (const n of snap.nodes) for (const v of String(n.labels?.[key] ?? '').split(',')) if (v.trim()) names.add(v.trim());
    return [...names].sort();
  }

  // buildView: what is drawn. Grouping by a label folds nodes into one card
  // and sums the traffic between groups.
  function buildView(snap, index, { groupBy = '', expanded = new Set() } = {}) {
    const stubs = new Set(snap.nodes.filter(n => n.stub).map(n => n.id));
    const groupOf = id => {
      if (!groupBy || stubs.has(id)) return null;
      const v = index.nodes.get(id)?.labels?.[groupBy];
      if (!v || expanded.has(v)) return null;
      return 'group:' + v;
    };
    const nodes = new Map();
    for (const n of snap.nodes) {
      const g = groupOf(n.id);
      if (!g) { nodes.set(n.id, { ...n, title: n.id, signals: nodeSignals(n, index) }); continue; }
      let gn = nodes.get(g);
      if (!gn) {
        gn = { id: g, group: true, kind: 'group', title: g.slice(6), labelKey: groupBy, members: [], health: 'unknown', visibility: 'none', requestRate: 0, latency: latencySum(), reasons: [], measures: [], signals: [], labels: { [groupBy]: g.slice(6) } };
        nodes.set(g, gn);
      }
      gn.members.push(n);
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
        ve = { id, from, to, members: [], requestRate: 0, failed: 0, latency: latencySum(), reasons: [], measures: [], stubEdge: true, errorsUnknown: true, signals: new Set(), states: new Set(), declared: false, observed: false, health: 'unknown', protocol: e.protocol, reason: e.reason, port: e.port, sources: new Set() };
        edges.set(id, ve);
      }
      ve.members.push(e);
      ve.requestRate += e.requestRate;
      ve.failed += e.requestRate * e.errorRate;
      ve.latency.add(e);
      ve.measures.push(...e.measures);
      if (e.reasons.length) ve.reasons.push(...e.reasons.map(r => (from === e.from && to === e.to ? r : { ...r, subject: `${e.from}→${e.to}` })));
      e.signals.forEach(s => ve.signals.add(s));
      e.sources.forEach(s => ve.sources.add(s));
      ve.states.add(e.state);
      ve.declared ||= e.declared; ve.observed ||= e.observed;
      ve.stubEdge &&= !!e.stubEdge; // a stub line only when every member is one
      ve.errorsUnknown &&= !!e.errorsUnknown; // likewise for unmeasured failures
      if (HEALTH_RANK[e.health] > HEALTH_RANK[ve.health]) ve.health = e.health;
    }
    for (const ve of edges.values()) {
      ve.errorRate = ve.requestRate > 0 ? ve.failed / ve.requestRate : 0;
      Object.assign(ve, ve.latency.result());
      delete ve.latency;
      ve.reasons.sort((a, b) => HEALTH_RANK[b.health] - HEALTH_RANK[a.health]);
      ve.state = ve.states.has('ghost') ? 'ghost' : ve.states.has('ok') ? 'ok' : 'stale';
      ve.signals = [...ve.signals]; ve.sources = [...ve.sources];
      delete ve.states; delete ve.failed;
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
  function layered(view, { colGap = 300, rowGap = 112 } = {}) {
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
      if (inc.get(id).length || !forward.get(id).length) continue;
      depth.set(id, Math.max(0, Math.min(...forward.get(id).map(t => depth.get(t))) - 1));
    }
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

  const core = { sanitize, signature, indexSnapshot, nodeSignals, labelKeys, buildView, matcher, layered, particleBudget, safeApiBase, categoryOf, cardSubtitle, fmtMs, fmtSec, fmtBytes, reasonTag, compact, placeTag, parseJourney, journeyOf, journeySnapshot, journeyWhy, journeyNames, TECH, CATEGORY, SIGNAL, REASON };

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
    app.className = `teia ${opts.mode}`;
    app.innerHTML = MARKUP;
    const style = document.createElement('style');
    style.textContent = STYLE;
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
      const s = sv('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.8', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', class: cls, 'aria-hidden': 'true' });
      s.innerHTML = paths;
      return s;
    };
    const css = name => getComputedStyle(host).getPropertyValue(name).trim();
    const timeOf = iso => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    const stored = (k, d) => { const v = store.get(k, null); return v == null ? d : v; };

    // -------------------------------------------------------------------- state
    const state = {
      snap: null, sig: '', pending: null, connected: false, error: '', lastData: 0,
      view: { x: 0, y: 0, k: 1 }, fitted: false, userMoved: false,
      positions: stored('positions', {}),
      selected: null, hover: null, hoverEdge: null,
      states: stored('states', { ok: true, ghost: true, stale: true }),
      signals: new Set(stored('signals', [])),
      query: '',
      groupBy: stored('groupBy', opts.groupBy || ''),
      expanded: new Set(),
      flow: stored('flow', opts.flow === false ? false : !motionMQ.matches),
      live: true,
      visible: true,
      tab: 'steps',
      activity: [],
      legendIcons: false,
      journey: '', // the journey shown: a value of the journey label, any key=value, or from:node
      values: stored('values', opts.values !== false), // traffic and latency on every line
    };
    let index = indexSnapshot(sanitize({}));

    // ------------------------------------------------------------------- the map
    const svg = $('#map');
    const stage = $('#stage');
    let viewport, layerEdges, layerMarks, layerReasons, layerParticles, layerLabels, layerNodes;
    const nodeEls = new Map(), edgeEls = new Map();
    let currentView = null, layoutPos = {}, topologyKey = '';

    const SVG_CSS = `
      .card { fill: var(--card); stroke: var(--line-2); stroke-width: 1.2; transition: fill .3s, stroke .3s; }
      .node { cursor: pointer; outline: none; }
      .node:hover .card, .node.sel .card, .node:focus-visible .card { fill: var(--card-hi); stroke: var(--accent); }
      .node:focus-visible .card { stroke-width: 2.4; }
      .node.vis-partial .card { stroke-dasharray: 6 4; }
      .node.vis-none .card { stroke-dasharray: 2 4; }
      .node.vis-none { opacity: .78; }
      .tile { transition: fill .5s ease; }
      .h-healthy .tile { fill: var(--healthy); } .h-degraded .tile { fill: var(--degraded); }
      .h-failing .tile { fill: var(--failing); } .h-unknown .tile { fill: var(--unknown); }
      .glyph { display: none; } .h-degraded .glyph.warn, .h-failing .glyph.fail { display: inline; }
      .glyph path { fill: #020f59; stroke: var(--card); stroke-width: 1.2; }
      .kind-group .card { stroke: var(--accent); stroke-width: 1.4; }
      .node .icon { color: #020f59; }
      .node .title { fill: var(--ink); font-weight: 600; font-size: 14px; letter-spacing: .01em; }
      .node .sub { fill: var(--muted); font-size: 11px; }
      .node .badge circle { fill: var(--bg-2); stroke: var(--line-2); }
      .node .badge svg { color: var(--ink-2); }
      .node .alert circle { fill: var(--degraded); stroke: var(--bg); stroke-width: 2; }
      .node .alert.critical circle { fill: var(--failing); }
      .node .alert text { fill: #020f59; font-size: 10.5px; font-weight: 700; text-anchor: middle; }
      .node .halo { fill: none; stroke: var(--failing); stroke-width: 2; opacity: 0; }
      .why-tag rect { stroke: var(--bg); stroke-width: 1.5; }
      .why-tag.degraded rect { fill: var(--degraded); } .why-tag.failing rect { fill: var(--failing); }
      .why-tag text { fill: #020f59; font-size: 10.5px; font-weight: 700; }
      .why-tag svg { color: #020f59; }
      .why-tag { cursor: pointer; }
      .value-tag rect { fill: var(--panel); stroke: var(--line-2); stroke-width: 1; }
      .value-tag text { fill: var(--ink-2); font-size: 10px; font-weight: 600; }
      .value-tag { cursor: pointer; }
      .far .value-tag { display: none; }
      .node.stub .card { fill: transparent; stroke: var(--line-2); stroke-dasharray: 4 4; }
      .node.stub .tile { fill: var(--line-2); }
      .node.stub { opacity: .8; }
      .edge.to-stub path.line { stroke-opacity: .3; }
      .flowing .h-failing .halo { animation: halo 1.6s ease-out infinite; }
      @keyframes halo { 0% { opacity: .7; transform: scale(1); } 100% { opacity: 0; transform: scale(1.12); } }
      .node.match .card { stroke: var(--accent); stroke-width: 2.2; }
      .node.enter { animation: enter .5s ease-out; }
      @keyframes enter { from { opacity: 0; } to { opacity: 1; } }
      .edge path.line { fill: none; stroke-linecap: round; transition: stroke .5s, stroke-width .3s; }
      .edge path.hit { fill: none; stroke: transparent; stroke-width: 14; cursor: pointer; }
      .edge.h-healthy path.line { stroke: var(--healthy); } .edge.h-degraded path.line { stroke: var(--degraded); }
      .edge.h-failing path.line { stroke: var(--failing); } .edge.h-unknown path.line { stroke: var(--edge); }
      .edge path.line { stroke-opacity: .75; }
      .edge.s-ghost path.line { stroke-dasharray: 8 6; }
      .edge.s-stale path.line { stroke-dasharray: 2 6; stroke: var(--stale); stroke-opacity: .85; }
      .edge.sel path.line, .edge:hover path.line { stroke-opacity: 1; }
      .arrow.healthy { fill: var(--healthy); } .arrow.degraded { fill: var(--degraded); } .arrow.failing { fill: var(--failing); }
      .arrow.unknown { fill: var(--edge); } .arrow.ghost { fill: var(--stale); } .arrow.accent { fill: var(--accent); }
      .mark { cursor: pointer; }
      .mark circle { fill: var(--ghost); stroke: var(--bg); stroke-width: 2; }
      .mark text { fill: #020f59; font-size: 10px; font-weight: 800; text-anchor: middle; }
      .elabel rect { fill: var(--panel); stroke: var(--line-2); }
      .elabel text { fill: var(--ink); font-size: 11px; }
      .particles circle { fill: var(--particle); }
      .particles circle.ghost { fill: var(--ghost); }
      .particles circle.err { fill: var(--failing); }
      .particles circle.faint { opacity: .55; }
      .dim, .node.dim { opacity: .14; transition: opacity .2s; }
      @media (prefers-reduced-motion: reduce) { .node.enter, .flowing .h-failing .halo { animation: none; } .dim, .card, .tile { transition: none; } }
    `;

    function initSVG() {
      svg.replaceChildren();
      const defs = sv('defs');
      for (const h of ['healthy', 'degraded', 'failing', 'unknown', 'ghost', 'accent']) {
        const m = sv('marker', { id: 'arrow-' + h, viewBox: '0 0 10 10', refX: '9', refY: '5', markerWidth: '7', markerHeight: '7', orient: 'auto-start-reverse' });
        m.append(sv('path', { d: 'M0,1 L9,5 L0,9 z', class: 'arrow ' + h }));
        defs.append(m);
      }
      const st = sv('style');
      st.textContent = SVG_CSS;
      defs.append(st);
      svg.append(defs);
      viewport = sv('g', { id: 'viewport' });
      layerEdges = sv('g'); layerMarks = sv('g'); layerReasons = sv('g'); layerParticles = sv('g', { class: 'particles' }); layerLabels = sv('g'); layerNodes = sv('g', { role: 'list' });
      viewport.append(layerEdges, layerParticles, layerMarks, layerReasons, layerLabels, layerNodes);
      svg.append(viewport);
    }

    const posOf = id => state.positions[id] || layoutPos[id] || { x: 0, y: 0 };

    function edgeVisible(e) {
      if (!state.states[e.state]) return false;
      if (state.signals.size && !e.signals.some(s => state.signals.has(s))) return false;
      return true;
    }

    // posKey stores each view's own layout: the whole map, or one journey.
    const posKey = () => 'positions' + (state.journey ? '@' + state.journey.toLowerCase() : '');

    function renderMap() {
      const view = buildView(state.j ? journeySnapshot(state.snap, state.j) : state.snap, index, state);
      currentView = view;
      const key = [...view.nodes.keys()].sort().join('|') + '#' + [...view.edges.keys()].sort().join('|');
      if (key !== topologyKey) { layoutPos = layered(view); topologyKey = key; }
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
        g.style.display = edgeVisible(e) ? '' : 'none';
        g.setAttribute('class', `edge s-${e.state} h-${e.health}${e.stubEdge ? ' to-stub' : ''}${state.selected?.id === e.id ? ' sel' : ''}`);
        routeEdge(g, posOf(e.from), posOf(e.to), view.edges.has(e.to + '->' + e.from) ? 7 : 0);
      }
      for (const [id, g] of edgeEls) if (!eSeen.has(id)) { g.remove(); edgeEls.delete(id); }

      const empty = $('#empty');
      empty.hidden = view.nodes.size > 0;
      if (!empty.hidden) {
        empty.firstChild.textContent = state.j
          ? `Nothing takes part in the journey "${state.journey}". Clear it with ✕ above, or search for the label your services carry.`
          : 'No communication discovered yet. The Sources tab shows what teia reads and what failed.';
      }

      renderMarks();
      renderReasons();
      rebuildParticles();
      applyHighlight();
      renderEdgeLabel();
      if (!state.fitted && view.nodes.size) state.fitted = fit();
    }

    function createNode(n) {
      const g = sv('g', { class: 'node enter', tabindex: '0', role: 'button' });
      g.dataset.id = n.id;
      const halo = sv('rect', { class: 'halo', x: -NODE_W / 2, y: -NODE_H / 2, width: NODE_W, height: NODE_H, rx: 15, style: 'transform-box: fill-box; transform-origin: center' });
      const card = sv('rect', { class: 'card', x: -NODE_W / 2, y: -NODE_H / 2, width: NODE_W, height: NODE_H, rx: 15 });
      const tile = sv('rect', { class: 'tile', x: -NODE_W / 2 + 10, y: -20, width: 40, height: 40, rx: 11 });
      const icon = sv('g', { class: 'icon', transform: `translate(${-NODE_W / 2 + 18},-12)` });
      // Health is never colour alone: a triangle for degraded, a cross for failing.
      const warn = sv('g', { class: 'glyph warn', transform: `translate(${-NODE_W / 2 + 44},14)` });
      warn.append(sv('path', { d: 'M0,-6 L6,5 L-6,5 Z' }));
      const fail = sv('g', { class: 'glyph fail', transform: `translate(${-NODE_W / 2 + 44},14)` });
      fail.append(sv('path', { d: 'M-5,-3 L-3,-5 L0,-2 L3,-5 L5,-3 L2,0 L5,3 L3,5 L0,2 L-3,5 L-5,3 L-2,0 Z' }));
      const title = sv('text', { class: 'title', x: -NODE_W / 2 + 60, y: -3 });
      const sub = sv('text', { class: 'sub num', x: -NODE_W / 2 + 60, y: 14 });
      const badges = sv('g', { class: 'badges' });
      const alert = sv('g', { class: 'alert', transform: `translate(${NODE_W / 2 - 4},${-NODE_H / 2 + 4})` });
      // The worst reason sits on the card's top edge, like a tab.
      const why = sv('g', { transform: `translate(${-NODE_W / 2 + 12},${-NODE_H / 2 - 2})` });
      const tip = sv('title');
      g.append(halo, card, tile, icon, warn, fail, title, sub, badges, alert, why, tip);
      g._parts = { icon, title, sub, badges, alert, why, tip };
      g.addEventListener('pointerdown', ev => startNodeDrag(ev, g));
      g.addEventListener('mouseenter', () => { state.hover = g.dataset.id; applyHighlight(); });
      g.addEventListener('mouseleave', () => { state.hover = null; applyHighlight(); });
      g.addEventListener('keydown', ev => {
        if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); ev.stopPropagation(); select({ type: 'node', id: g.dataset.id }); }
      });
      later(() => g.classList.remove('enter'), 600);
      return g;
    }

    function updateNode(g, n) {
      const p = g._parts;
      const sel = state.selected?.id === n.id;
      g.setAttribute('class', `node kind-${n.kind} h-${n.health} vis-${n.visibility}${n.stub ? ' stub' : ''}${sel ? ' sel' : ''}${g.classList.contains('enter') ? ' enter' : ''}`);
      const cat = categoryOf(n);
      if (p.cat !== cat) {
        p.icon.replaceChildren(iconSVG(iconOf(n)));
        p.icon.firstChild.setAttribute('width', 24); p.icon.firstChild.setAttribute('height', 24);
        p.cat = cat;
      }
      p.title.textContent = n.title.length > 17 ? n.title.slice(0, 16) + '…' : n.title;
      p.sub.textContent = cardSubtitle(n);
      p.badges.replaceChildren(...n.signals.slice(0, 6).map((s, i) => {
        const b = sv('g', { class: 'badge', transform: `translate(${-NODE_W / 2 + 70 + i * 21},${NODE_H / 2})` });
        b.append(sv('circle', { r: 9 }));
        const ic = iconSVG(SIGNAL[s].icon);
        ic.setAttribute('x', -6); ic.setAttribute('y', -6); ic.setAttribute('width', 12); ic.setAttribute('height', 12);
        const t = sv('title'); t.textContent = 'seen by ' + SIGNAL[s].label;
        b.append(ic, t);
        return b;
      }));
      const fs = state.snap.findings.filter(f => n.group ? n.members.some(m => m.id === f.subject) : f.subject === n.id);
      p.alert.replaceChildren();
      if (fs.length) {
        p.alert.setAttribute('class', 'alert' + (fs.some(f => f.severity === 'critical') ? ' critical' : ''));
        p.alert.append(sv('circle', { r: 9 }));
        const t = sv('text', { y: 3.6 }); t.textContent = fs.length; p.alert.append(t);
      }
      const traffic = n.requestRate > 0 ? `, ${fmtRate(n.requestRate)} requests per second${n.latencyP95Ms > 0 ? `, p95 ${fmtMs(n.latencyP95Ms)}` : ''}, ${n.errorsUnknown ? 'failures not measured' : pct(n.errorRate) + ' errors'}` : '';
      drawWhy(p.why, n.reasons);
      const because = n.reasons.length ? `, because of ${n.reasons.map(r => r.summary || reasonTag(r)).join('; ')}` : '';
      const label = `${n.title}, ${n.group ? 'group' : CATEGORY[cat]?.label || n.kind}${n.tech ? ' (' + techLabel(n) + ')' : ''}, ${healthText(n)}${because}${traffic}, ${n.visibility} visibility${fs.length ? `, ${fs.length} next step${fs.length > 1 ? 's' : ''}` : ''}`;
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
      g.insertBefore(sv('rect', { x: 0, y: -9, width: Math.ceil(w) + 28, height: 18, rx: 9 }), text);
    }

    function createEdge() {
      const g = sv('g', { class: 'edge' });
      const line = sv('path', { class: 'line' });
      const hit = sv('path', { class: 'hit' });
      g.append(line, hit);
      g._line = line; g._hit = hit;
      hit.addEventListener('click', ev => { ev.stopPropagation(); select({ type: 'edge', id: g._edge.id }); });
      hit.addEventListener('mouseenter', () => { state.hoverEdge = g._edge.id; renderEdgeLabel(); });
      hit.addEventListener('mouseleave', () => { state.hoverEdge = null; renderEdgeLabel(); });
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
      const w = e.requestRate > 0 ? 1.6 + Math.min(3, Math.log10(1 + e.requestRate * 4) * 1.5) : 1.5;
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
        const t = sv('text', { y: 3.5 }); t.textContent = '!';
        const tt = sv('title'); tt.textContent = 'Observed but not declared in any flows file';
        m.append(sv('circle', { r: 8 }), t, tt);
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
      const obstacles = [...nodeEls.values()].map(n => ({
        x0: n._pos.x - NODE_W / 2 - 6, x1: n._pos.x + NODE_W / 2 + 6,
        y0: n._pos.y - NODE_H / 2 - 14, y1: n._pos.y + NODE_H / 2 + 12, // the card's own tag and badges
      }));
      const shown = [...edgeEls.values()].filter(g => g.style.display !== 'none');
      const spotsOf = g => { const pts = samples(g); return [24, 20, 28, 16, 32, 12, 36, 8, 40, 5, 43].map(i => ({ x: pts[i][0], y: pts[i][1] })); };
      const pin = (tag, g, w, at) => {
        tag.setAttribute('transform', `translate(${at.x - w / 2},${at.y})`);
        tag.addEventListener('click', ev => { ev.stopPropagation(); select({ type: 'edge', id: g._edge.id }); });
      };
      // Reasons first: they matter most and take the best spots.
      for (const g of shown) {
        const e = g._edge;
        if (!e.reasons.length) continue;
        const tag = sv('g');
        tag._edge = e;
        layerReasons.append(tag);
        drawWhy(tag, e.reasons);
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
        if (e.reasons.length || e.stubEdge || !text) continue;
        const tag = sv('g', { class: 'value-tag' });
        tag._edge = e;
        const label = sv('text', { x: 7, y: 3.5 });
        label.textContent = text;
        tag.append(label);
        layerReasons.append(tag);
        const w = Math.ceil(label.getComputedTextLength ? label.getComputedTextLength() : text.length * 5.6) + 14;
        tag.insertBefore(sv('rect', { x: 0, y: -8, width: w, height: 16, rx: 8 }), label);
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
      if (e.latencyP95Ms > 0) parts.push(fmtMs(e.latencyP95Ms));
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
      if (e.latencyP95Ms > 0) parts.push(`p95 ${fmtMs(e.latencyP95Ms)}`);
      if (e.errorsUnknown && e.requestRate > 0) parts.push('failures not measured');
      if (e.errorRate > 0.0005) parts.push(`${pct(e.errorRate)} errors`);
      if (state.snap.drift && e.state !== 'ok') parts.push(STATE_LABEL[e.state]);
      const p = mid(g);
      const lbl = sv('g', { class: 'elabel', transform: `translate(${p.x},${p.y - 18})` });
      const text = sv('text', { 'text-anchor': 'middle', y: 4 }); text.textContent = parts.join(' · ');
      lbl.append(text);
      layerLabels.append(lbl);
      const bb = text.getBBox();
      lbl.insertBefore(sv('rect', { x: bb.x - 8, y: bb.y - 4, width: bb.width + 16, height: bb.height + 8, rx: 7 }), text);
    }

    // ---------------------------------------------------------------- particles
    let particles = [];
    function rebuildParticles() {
      const old = new Map(particles.map(p => [p.key, p]));
      particles = [];
      layerParticles.replaceChildren();
      const visible = [...edgeEls.values()].filter(g => g.style.display !== 'none' && !g._edge.stubEdge);
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
      p.el.setAttribute('class', (p.err ? 'err' : p.edge.state === 'ghost' && state.snap.drift ? 'ghost' : '') + (p.edge.requestRate > 0 ? '' : ' faint'));
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
          const errLap = Math.random() < Math.min(0.9, (p.edge.errorRate || 0) * 4);
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
      const padTop = page ? 70 : 56, pad = page ? 60 : 24;
      const k = Math.max(0.05, Math.min(1.25, (r.width - pad * 2) / (x1 - x0), (r.height - padTop - pad) / (y1 - y0)));
      state.view = { k, x: (r.width - (x1 - x0) * k) / 2 - x0 * k, y: padTop + (r.height - padTop - pad - (y1 - y0) * k) / 2 - y0 * k };
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
      if (ev.target.closest('.node, .edge, .mark')) return;
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
      const focus = state.hover || (state.selected?.type === 'node' ? state.selected.id : null);
      const match = matcher(state.query);
      const nb = focus && currentView.nodes.has(focus) ? neighborhood(focus) : null;
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
      if (page) return;
      const aside = $('aside');
      aside.classList.toggle('open', open);
      aside.inert = !open;
    }

    function select(sel) {
      state.selected = sel;
      for (const [id, g] of nodeEls) g.classList.toggle('sel', sel?.type === 'node' && sel.id === id);
      for (const [id, g] of edgeEls) g.classList.toggle('sel', sel?.type === 'edge' && sel.id === id);
      applyHighlight();
      renderEdgeLabel();
      renderDetails();
      if (sel) { showTab('details'); setDrawer(true); }
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

    function toggleGroup(id) {
      if (id.startsWith('group:')) state.expanded.add(id.slice(6));
      else {
        const v = index.nodes.get(id)?.labels?.[state.groupBy];
        if (v && state.expanded.has(v)) state.expanded.delete(v); else return;
      }
      renderMap();
      const sel = state.selected;
      if (sel && !currentView.nodes.has(sel.id) && !currentView.edges.has(sel.id)) {
        const g = currentView.groupOf(sel.id);
        select(g && currentView.nodes.has(g) ? { type: 'node', id: g } : null);
      } else renderDetails();
    }

    // --------------------------------------------------------------- top bar
    function renderRings() {
      const c = state.snap.coverage;
      const ring = (label, v, hint) => {
        const r = 15, circ = 2 * Math.PI * r;
        const color = v >= 0.8 ? css('--healthy') : v >= 0.5 ? css('--degraded') : css('--failing');
        const s = sv('svg', { viewBox: '0 0 38 38', 'aria-hidden': 'true' });
        s.append(sv('circle', { class: 'track', cx: 19, cy: 19, r, fill: 'none', 'stroke-width': 4 }));
        s.append(sv('circle', { class: 'value', cx: 19, cy: 19, r, fill: 'none', stroke: color, 'stroke-width': 4, 'stroke-linecap': 'round', 'stroke-dasharray': `${circ * v} ${circ}` }));
        return el('div', { class: 'ring', title: hint }, s, el('div', {}, el('b', { class: 'num' }, pct(v)), el('span', {}, label)));
      };
      const rings = [ring('visibility', c.visibility, 'Share of internal nodes teia sees first-hand')];
      if (state.snap.drift) {
        rings.push(ring('declared', c.declared, 'Share of observed communication declared in flows files'));
        rings.push(ring('observed', c.observed, 'Share of declared communication actually observed'));
      }
      $('#rings').replaceChildren(...rings);
    }

    function renderLive() {
      const l = $('#live');
      if (state.error) { l.className = 'live error'; l.querySelector('span').textContent = state.error; return; }
      l.className = 'live ' + (!state.live ? 'paused' : state.connected ? 'on' : '');
      const since = opts.api && state.snap ? new Date(state.snap.generatedAt).getTime() : state.lastData;
      const ago = since ? Math.max(0, Math.round((Date.now() - since) / 1000)) : 0;
      l.querySelector('span').textContent = !state.live ? 'paused'
        : !state.connected ? (opts.api ? 'reconnecting…' : 'waiting for data…')
          : opts.api ? `live · round ${state.snap?.round ?? '–'} · ${ago}s ago` : `updated ${ago}s ago`;
    }

    function renderFilters() {
      const edges = state.snap.edges;
      const present = new Map();
      for (const e of edges) for (const s of e.signals) present.set(s, (present.get(s) || 0) + 1);
      // A saved filter on a signal the data no longer has would hide everything.
      for (const s of [...state.signals]) if (!present.has(s)) state.signals.delete(s);
      const chips = [];
      if (state.j) {
        const why = journeyWhy(state.snap, state.j);
        const parts = [iconSVG('<circle cx="5" cy="12" r="2.4"/><circle cx="19" cy="6" r="2.4"/><circle cx="19" cy="18" r="2.4"/><path d="M7.2 11l9.6-4M7.2 13l9.6 4"/>'),
          el('span', {}, `${state.journey} · ${state.j.roles.size}${state.j.truncated ? '+' : ''}`)];
        if (why.reason) {
          parts.push(el('button', { type: 'button', title: `Show ${why.subject.replace('->', ' → ')}`, onclick: () => locate(why.subject) },
            iconSVG(REASON[why.reason.cause].icon), `${why.subject.replace('->', ' → ')} · ${reasonTag(why.reason, why.more)}`));
        }
        parts.push(el('button', { type: 'button', 'aria-label': 'Show the whole map', title: 'Show the whole map', onclick: () => setJourney('') }, '✕'));
        chips.push(el('div', { class: 'journey-chip' }, ...parts), el('span', { class: 'sep' }));
      }
      if (state.snap.drift) {
        const count = st => edges.filter(e => e.state === st).length;
        const stateChip = (st, label, style) => el('button', {
          type: 'button', class: 'chip' + (state.states[st] ? '' : ' off'), title: STATE_LABEL[st], 'aria-pressed': String(!!state.states[st]),
          onclick: () => { state.states[st] = !state.states[st]; store.set('states', state.states); renderAll(); },
        }, el('i', { class: 'ln', style }), label, el('b', { class: 'num' }, count(st)));
        chips.push(stateChip('ok', 'declared', `border-color:${css('--healthy')}`));
        chips.push(stateChip('ghost', 'undeclared', `border-color:${css('--ghost')};border-top-style:dashed`));
        chips.push(stateChip('stale', 'not seen', `border-color:${css('--stale')};border-top-style:dotted`));
        chips.push(el('span', { class: 'sep' }));
      }
      for (const [s, n] of [...present.entries()].sort()) {
        if (!has(SIGNAL, s)) continue;
        const on = !state.signals.size || state.signals.has(s);
        chips.push(el('button', {
          type: 'button', class: 'chip' + (on ? '' : ' off'), 'aria-pressed': String(state.signals.has(s)),
          title: `Only communication seen by ${SIGNAL[s].label.toLowerCase()} (click again to clear)`,
          onclick: () => {
            if (state.signals.has(s) && state.signals.size === 1) state.signals.clear();
            else { state.signals.clear(); state.signals.add(s); }
            store.set('signals', [...state.signals]); renderAll();
          },
        }, iconSVG(SIGNAL[s].icon), SIGNAL[s].label, el('b', { class: 'num' }, n)));
      }
      $('#filters').replaceChildren(...chips);
      $('#filters').hidden = chips.length === 0;
    }

    function renderLegend() {
      const sw = (color, label) => el('div', { class: 'lg' }, el('i', { class: 'sw', style: `background:${color}` }), label);
      const ln = (style, label) => el('div', { class: 'lg' }, el('i', { class: 'ln', style }), label);
      const bx = (style, label) => el('div', { class: 'lg' }, el('i', { class: 'bx', style }), label);
      const parts = [
        el('h4', {}, 'health'), el('h4', {}, 'communication'), el('h4', {}, 'visibility'),
        sw(css('--healthy'), 'healthy'), ln('', 'declared + seen'), bx('', 'first-hand'),
        sw(css('--degraded'), '▲ degraded'), ln('border-top-style:dashed', 'not declared'), bx('border-style:dashed', 'via peers'),
        sw(css('--failing'), '✕ failing'), ln('border-top-style:dotted', 'not seen'), bx('border-style:dotted', 'no traffic'),
        el('h4', { class: 'span3' }, 'why: the tag on the card or line'),
        el('div', { class: 'reasons' }, ...Object.values(REASON).map(r => el('span', { class: 'lg' }, iconSVG(r.icon), r.label))),
      ];
      if (state.legendIcons) {
        parts.push(el('h4', { class: 'span3' }, 'components'));
        for (const [id, c] of Object.entries(CATEGORY)) {
          const examples = Object.values(TECH).filter(t => t.cat === id && !['Microservice', 'External', 'Frontend', 'Job', 'Function'].includes(t.label)).map(t => t.label).slice(0, 3).join(', ');
          parts.push(el('div', { class: 'lg icon', title: examples }, iconSVG(c.icon), c.label));
        }
      }
      parts.push(el('button', { type: 'button', class: 'legend-toggle', 'aria-expanded': String(state.legendIcons), onclick: () => { state.legendIcons = !state.legendIcons; renderLegend(); } },
        state.legendIcons ? 'hide icon library' : 'icon library ▸'));
      $('#legend').replaceChildren(...parts);
    }

    // setJourney shows one journey, or the whole map for "". It keeps the
    // view's own layout, says so to screen readers and, on teia's page, in
    // the address bar so the view can be shared.
    function setJourney(spec) {
      spec = String(spec || '').trim();
      if (spec === state.journey) return;
      state.journey = spec;
      state.positions = stored(posKey(), {});
      topologyKey = '';
      state.fitted = false;
      state.j = spec && state.snap ? journeyOf(state.snap, spec) : null;
      if (state.selected && state.j && !state.j.roles.has(state.selected.id)) state.selected = null;
      if (page && typeof history !== 'undefined' && typeof location !== 'undefined') {
        const url = new URL(location.href);
        if (spec) url.searchParams.set('journey', spec); else url.searchParams.delete('journey');
        history.replaceState(null, '', url);
      }
      if (!state.snap) return;
      renderAll();
      state.fitted = fit();
      const j = state.j;
      $('#announce').textContent = !j ? 'Whole map' : `Journey ${spec}: ${j.roles.size} components${journeyLine(j)}`;
    }

    // journeyLine is the journey's headline: why it is amber or red.
    function journeyLine(j) {
      const why = journeyWhy(state.snap, j);
      return why.reason ? `, ${why.health} because of ${why.subject.replace('->', ' → ')}: ${why.reason.summary || reasonTag(why.reason)}` : '';
    }

    let journeyKeys = null;
    function renderJourneys() {
      const sel = $('#journey');
      const names = journeyNames(state.snap);
      const keys = names.join('|') + '#' + state.journey;
      sel.hidden = names.length === 0 && !state.journey;
      if (journeyKeys !== keys) { // rebuilding would close an open dropdown on every update
        journeyKeys = keys;
        const custom = state.journey && !names.includes(state.journey) ? [el('option', { value: state.journey }, 'Journey ' + state.journey)] : [];
        sel.replaceChildren(el('option', { value: '' }, 'Whole map'), ...names.map(n => el('option', { value: n }, 'Journey ' + n)), ...custom);
      }
      sel.value = state.journey;
    }

    let groupKeys = null;
    function renderGroupBy() {
      const sel = $('#groupBy');
      const keys = labelKeys(state.snap);
      if (state.groupBy && !keys.includes(state.groupBy)) { state.groupBy = ''; state.expanded.clear(); }
      if (groupKeys !== keys.join('|')) { // rebuilding would close an open dropdown on every update
        groupKeys = keys.join('|');
        sel.replaceChildren(el('option', { value: '' }, 'No grouping'), ...keys.map(k => el('option', { value: k }, 'Group by ' + k)));
      }
      sel.value = state.groupBy;
    }

    function renderButtons() {
      const flow = $('#flowBtn');
      flow.replaceChildren(iconSVG(state.flow ? '<rect x="6" y="5" width="4" height="14" rx="1"/><rect x="14" y="5" width="4" height="14" rx="1"/>' : '<path d="M7 4.5v15l12-7.5z"/>'), 'Flow');
      flow.classList.toggle('on', state.flow);
      flow.setAttribute('aria-pressed', String(state.flow));
      svg.classList.toggle('flowing', state.flow);
      const live = $('#liveBtn');
      live.replaceChildren(iconSVG('<circle cx="12" cy="12" r="3"/><path d="M5.6 5.6a9 9 0 000 12.8M18.4 5.6a9 9 0 010 12.8M8.5 8.5a5 5 0 000 7M15.5 8.5a5 5 0 010 7"/>'), 'Live');
      live.classList.toggle('on', state.live);
      live.setAttribute('aria-pressed', String(state.live));
      const values = $('#valuesBtn');
      values.replaceChildren(iconSVG('<path d="M3 17l6-6 4 4 8-8"/><path d="M14 7h7v7"/>'), 'Values');
      values.classList.toggle('on', state.values);
      values.setAttribute('aria-pressed', String(state.values));
    }

    // ------------------------------------------------------------- side panel
    function showTab(tab) {
      state.tab = tab;
      app.querySelectorAll('.tab').forEach(t => { const on = t.dataset.tab === tab; t.classList.toggle('on', on); t.setAttribute('aria-selected', String(on)); });
      app.querySelectorAll('.pane').forEach(p => p.classList.toggle('on', p.id === 'pane-' + tab));
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

    function findingCard(f, open = false) {
      const head = el('button', { type: 'button', class: 'card-head', title: 'Show it on the map', onclick: () => locate(f.subject) },
        el('span', { class: 'meta' }, `${f.severity} · ${f.kind || f.rule || ''} · ${f.subject}`),
        el('span', { class: 'h4' }, f.title));
      const card = el('div', { class: 'card-f ' + f.severity }, head);
      if (f.detail) card.append(el('p', {}, f.detail));
      if (f.fix) {
        card.append(el('p', { class: 'next' }, '→ ' + f.fix.summary));
        const s = snippet(f.fix);
        if (s) card.append(open ? s : el('details', {}, el('summary', {}, 'show ' + (f.fix.language || 'snippet')), s));
      }
      return card;
    }

    // locate shows a subject (node, edge or "a->b") on the map, through its
    // group when it is folded into one. move: false selects it in place.
    function locate(subject, { move = true } = {}) {
      if (!subject || !currentView) return;
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
      if (!fs.length) { pane.replaceChildren(el('p', { class: 'muted' }, 'Nothing to do: the picture is complete for the configured sources.')); return; }
      pane.replaceChildren(el('h3', {}, 'What teia needs to see more'), ...fs.map(f => findingCard(f)));
    }

    // measureText reads a measure out: "12 of 97 (12%)", "3, held by legacy-reports".
    const measureText = m => (m.unit === 'share' || !(m.max > 0) ? fmtValue(m.value, m.unit) : `${fmtValue(m.value, m.unit)} of ${fmtValue(m.max, m.unit)} (${pct(m.value / m.max)})`) + (m.via ? `, held by ${m.via}` : '');

    // whyBlock explains an amber or red node or line, worst reason first,
    // with a way to the dependency it most likely comes from.
    function whyBlock(list) {
      if (!list.length) return [];
      const red = list.some(r => r.health === 'failing');
      return [el('h3', {}, red ? 'Why it is red' : 'Why it is amber'), ...list.map(r => el('div', { class: 'why ' + r.health },
        iconSVG(REASON[r.cause].icon),
        el('span', {}, (r.summary || reasonTag(r)) + (r.subject ? ` (${r.subject})` : '')),
        r.via ? el('button', { type: 'button', class: 'btn', title: `Show ${r.via} on the map`, onclick: () => locate(r.via) }, 'go to ' + r.via) : el('span'),
      ))];
    }

    function rel(label, sub, health, onclick) {
      return el('button', { type: 'button', class: 'rel', onclick }, el('i', { class: 'dot', style: `background:${css('--' + health)}` }), label, el('span', {}, sub));
    }

    function renderDetails() {
      const pane = $('#pane-details');
      const sel = state.selected;
      if (!sel || !currentView) { pane.replaceChildren(el('p', { class: 'muted' }, 'Click a service, a datastore or a communication on the map.')); return; }
      if (sel.type === 'node') {
        const n = currentView.nodes.get(sel.id);
        if (!n) { pane.replaceChildren(el('p', { class: 'muted' }, 'That node is no longer in the graph.')); return; }
        const tile = el('div', { class: 'tile', style: `background:${css('--' + n.health)}` }, iconSVG(iconOf(n)));
        const parts = [el('div', { class: 'hero' }, tile, el('div', {}, el('h2', {}, n.title),
          el('span', { class: 'pill' }, el('i', { class: 'dot', style: `background:${css('--' + n.health)}` }), healthText(n)), ' ',
          el('span', { class: 'pill' }, n.kind), ' ', el('span', { class: 'pill' }, n.visibility + ' visibility'))), ...whyBlock(n.reasons)];
        if (n.group) {
          const inside = n.internal ? `, talking to each other through ${n.internal} communication${n.internal === 1 ? '' : 's'}` : '';
          parts.push(el('p', { class: 'explain' }, `${n.members.length} workload${n.members.length === 1 ? '' : 's'} with ${n.labelKey}=${n.title}${inside}. Double-click the card to expand it.`));
          parts.push(el('button', { type: 'button', class: 'btn', onclick: () => toggleGroup(n.id) }, 'Expand group'));
          parts.push(el('h3', {}, 'Members'));
          parts.push(...n.members.map(m => rel(m.id, m.kind, m.health, () => { state.expanded.add(n.title); renderMap(); focusNode(m.id); })));
        } else if (n.stubOf) {
          parts.push(el('p', { class: 'explain' }, `${n.stubOf.length} more callers outside the journey: ${n.stubOf.join(', ')}.`));
        } else {
          if (n.stub) {
            parts.push(el('p', { class: 'explain' }, `Outside the journey ${state.journey}: it calls into it, or the journey leaves to it.`));
            parts.push(el('button', { type: 'button', class: 'btn', onclick: () => { setJourney(''); locate(n.id); } }, 'Show on the whole map'));
          } else if (n.journeyRole) {
            parts.push(el('p', { class: 'explain' }, n.journeyRole === 'member'
              ? `Part of the journey ${state.journey}.`
              : `Used by the journey ${state.journey}${(state.snap.edges.some(e => e.to === n.id && !state.j.roles.has(e.from))) ? ', and by others outside it: its health includes their traffic' : ''}.`));
          }
          parts.push(el('p', { class: 'explain' }, VIS_EXPLAIN[n.visibility]));
          const kv = el('dl', { class: 'kv' });
          const row = (k, v) => kv.append(el('dt', {}, k), el('dd', {}, v));
          if (n.requestRate > 0) row('traffic in', `${fmtRate(n.requestRate)} req/s · ${n.errorsUnknown ? 'failures not measured' : pct(n.errorRate) + ' errors'}`);
          if (n.latencyP95Ms > 0) row('latency', `p50 ${fmtMs(n.latencyP50Ms)} · p95 ${fmtMs(n.latencyP95Ms)} (what its callers wait for)`);
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
        const out = [...currentView.edges.values()].filter(e => e.from === n.id);
        const inn = [...currentView.edges.values()].filter(e => e.to === n.id);
        const relEdge = (e, other) => rel(other, `${e.requestRate > 0 ? fmtRate(e.requestRate) + '/s · ' : ''}${state.snap.drift ? STATE_LABEL[e.state] : e.protocol || ''}`, e.state === 'stale' ? 'unknown' : e.health, () => select({ type: 'edge', id: e.id }));
        if (out.length) { parts.push(el('h3', {}, `Calls (${out.length})`)); parts.push(...out.map(e => relEdge(e, e.to))); }
        if (inn.length) { parts.push(el('h3', {}, `Called by (${inn.length})`)); parts.push(...inn.map(e => relEdge(e, e.from))); }
        const fs = state.snap.findings.filter(f => n.group ? n.members.some(m => m.id === f.subject) : f.subject === n.id || f.subject.startsWith(n.id + '->'));
        if (fs.length) { parts.push(el('h3', {}, 'Next steps')); parts.push(...fs.map(f => findingCard(f, true))); }
        pane.replaceChildren(...parts);
        return;
      }
      const e = currentView.edges.get(sel.id);
      if (!e) { pane.replaceChildren(el('p', { class: 'muted' }, 'That communication is no longer in the graph.')); return; }
      const color = e.state === 'stale' ? 'unknown' : e.health;
      const pills = [el('span', { class: 'pill' }, el('i', { class: 'dot', style: `background:${css('--' + color)}` }), healthText(e))];
      if (state.snap.drift) pills.push(' ', el('span', { class: 'pill' }, STATE_LABEL[e.state]));
      const parts = [el('div', { class: 'hero' },
        el('div', { class: 'tile', style: `background:${css('--' + color)}` }, iconSVG('<path d="M4 12h14M13 6l6 6-6 6"/>')),
        el('div', {}, el('h2', {}, `${e.from} → ${e.to}`), ...pills)), ...whyBlock(e.reasons)];
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
        row('errors', e.errorsUnknown ? 'not measured: the source counts requests, not failures (see Next steps)' : pct(e.errorRate));
      }
      if (e.latencyP95Ms > 0) row('latency', `p50 ${fmtMs(e.latencyP50Ms)} · p95 ${fmtMs(e.latencyP95Ms)}`);
      for (const m of e.measures) row(m.what, measureText(m));
      if (state.snap.drift) row('reason', e.reason || (e.declared ? '—' : 'not declared'));
      row('seen by', e.signals.map(s => has(SIGNAL, s) ? SIGNAL[s].label : s).join(', ') || (e.observed ? '—' : 'nobody yet'));
      if (e.sources.length) row('sources', e.sources.join(', '));
      if (e.members.length > 1) row('aggregates', `${e.members.length} communications`);
      parts.push(kv);
      const fs = state.snap.findings.filter(f => e.members.some(m => f.subject === m.from + '->' + m.to) || (e.state === 'ghost' && f.subject === e.from && f.rule === 'undeclared-flow'));
      if (fs.length) { parts.push(el('h3', {}, 'Next steps')); parts.push(...fs.map(f => findingCard(f, true))); }
      pane.replaceChildren(...parts);
    }

    function renderSources() {
      const pane = $('#pane-sources');
      if (!state.snap.sources.length) { pane.replaceChildren(el('p', { class: 'muted' }, 'No source details in this data.')); return; }
      pane.replaceChildren(el('h3', {}, 'Signals teia reads'), ...state.snap.sources.map(s => el('div', { class: 'src' },
        el('div', {}, s.name, el('small', {}, `${s.type} · ${s.message || `${s.nodes} nodes, ${s.edges} edges`}`)),
        el('span', { class: 'state ' + s.state }, s.state))));
    }

    const CHANGE_ICON = {
      'edge.added': '<path d="M4 12h12M12 6l6 6-6 6"/><path d="M19 4v4M17 6h4"/>',
      'edge.removed': '<path d="M4 12h12M12 6l6 6-6 6"/><path d="M17 5h4"/>',
      'edge.state': '<path d="M4 7h13l-3-3M20 17H7l3 3"/>',
      'edge.health': '<circle cx="12" cy="12" r="7"/>',
      'node.added': '<path d="M12 5v14M5 12h14"/>',
      'node.removed': '<path d="M5 12h14"/>',
      'node.health': '<circle cx="12" cy="12" r="7"/>',
      'node.visibility': '<path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
      'finding.opened': '<circle cx="11" cy="11" r="7"/><path d="M20 20l-4-4"/>',
      'finding.resolved': '<path d="M5 12.5l4.5 4.5L19 7"/>',
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
      st.textContent = `svg{${vars.map(v => `${v}:${css(v)}`).join(';')};font-family:Outfit,system-ui,sans-serif}` + st.textContent;
      clone.insertBefore(sv('rect', { width: '100%', height: '100%', fill: css('--bg') }), clone.firstChild.nextSibling);
      const text = new XMLSerializer().serializeToString(clone);
      const name = `teia-map-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}`;
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

    // ----------------------------------------------------------------- data
    function renderAll() {
      index = indexSnapshot(state.snap);
      state.j = state.journey ? journeyOf(state.snap, state.journey) : null;
      renderRings(); renderFilters(); renderGroupBy(); renderJourneys(); renderButtons();
      renderMap(); renderSteps(); renderSources(); renderDetails(); renderLive();
    }

    function apply(raw, fresh) {
      if (destroyed) return;
      const snap = sanitize(raw);
      state.connected = true;
      state.error = '';
      if (!state.live && state.snap) { state.pending = snap; renderLive(); return; }
      const sig = signature(snap);
      const changed = sig !== state.sig;
      const isNew = !state.snap || snap.round !== state.snap.round;
      state.snap = snap;
      state.lastData = Date.now();
      if (changed) { state.sig = sig; renderAll(); } else renderLive();
      if (fresh && isNew) pushActivity(snap.changes, true);
    }

    async function load(signal) {
      const get = path => fetch(opts.api + path, { signal, headers: { Accept: 'application/json' } }).then(r => { if (!r.ok) throw new Error('HTTP ' + r.status); return r.json(); });
      const [snap, changes] = await Promise.all([get('snapshot'), get('changes').catch(() => [])]);
      if (destroyed) return;
      if (snap.generatedAt && !String(snap.generatedAt).startsWith('0001')) apply(snap, false);
      pushActivity(changes, false);
    }

    function connect() {
      if (destroyed || es || !opts.api || opts.stream === false) return;
      try { es = new EventSource(opts.api + 'stream'); } catch { return; }
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
      const t = ev.composedPath()[0];
      if (ev.ctrlKey || ev.metaKey || ev.altKey) return;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'SELECT' || t.tagName === 'TEXTAREA')) { if (ev.key === 'Escape') t.blur(); return; }
      const onControl = t && (t.tagName === 'BUTTON' || t.tagName === 'A' || t.tagName === 'SUMMARY');
      if (ev.key === 'Escape') {
        const open = !page && $('aside').classList.contains('open');
        if (state.selected || open) { ev.stopPropagation(); select(null); setDrawer(false); }
        return;
      }
      if (onControl) return;
      if (ev.key === 'f') fit();
      else if (ev.key === 'j' && !$('#journey').hidden) { ev.preventDefault(); $('#journey').focus(); }
      else if (ev.key === 'v') $('#valuesBtn').click();
      else if (ev.key === ' ') { ev.preventDefault(); $('#flowBtn').click(); }
      else if (ev.key === '/') { ev.preventDefault(); $('#search').focus(); }
    }

    let searchTimer = 0;
    app.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => showTab(t.dataset.tab)));
    $('#search').addEventListener('input', ev => {
      $('#isolateBtn').hidden = !ev.target.value.trim();
      clearTimeout(searchTimer);
      searchTimer = setTimeout(() => { state.query = ev.target.value; applyHighlight(); }, 120);
    });
    $('#search').addEventListener('keydown', ev => {
      if (ev.key !== 'Enter') return;
      if (ev.shiftKey) { ev.preventDefault(); isolate(); return; }
      state.query = ev.target.value; applyHighlight();
      const m = matcher(state.query);
      const hit = m && currentView && [...currentView.nodes.values()].find(m);
      if (hit) focusNode(hit.id);
    });
    $('#groupBy').addEventListener('change', ev => {
      state.groupBy = ev.target.value; state.expanded.clear(); store.set('groupBy', state.groupBy);
      state.selected = null; renderMap(); renderDetails(); fit();
    });
    $('#flowBtn').addEventListener('click', () => { state.flow = !state.flow; store.set('flow', state.flow); renderButtons(); wake(); });
    $('#valuesBtn').addEventListener('click', () => { state.values = !state.values; store.set('values', state.values); renderButtons(); renderReasons(); applyHighlight(); });
    $('#journey').addEventListener('change', ev => setJourney(ev.target.value));
    // isolate turns what the search names into the journey shown.
    const isolate = () => {
      let q = $('#search').value.trim();
      if (!q) return;
      // One word that names a node, not a journey: what that node depends on.
      if (/^[^\s=:]+$/.test(q) && state.snap && !journeyNames(state.snap).includes(q)) {
        const n = state.snap.nodes.find(x => x.id.toLowerCase() === q.toLowerCase());
        if (n) q = 'from:' + n.id;
      }
      $('#search').value = ''; state.query = ''; $('#isolateBtn').hidden = true;
      setJourney(q);
    };
    $('#isolateBtn').addEventListener('click', isolate);
    $('#liveBtn').addEventListener('click', () => {
      state.live = !state.live; renderButtons(); renderLive();
      if (!state.live) return;
      if (state.pending) { const p = state.pending; state.pending = null; apply(p, true); }
      connect();
    });
    $('#fitBtn').addEventListener('click', fit);
    $('#zoomIn').addEventListener('click', () => { const r = stage.getBoundingClientRect(); zoomAt(1.2, r.width / 2, r.height / 2); });
    $('#zoomOut').addEventListener('click', () => { const r = stage.getBoundingClientRect(); zoomAt(1 / 1.2, r.width / 2, r.height / 2); });
    $('#resetBtn').addEventListener('click', () => { state.positions = {}; store.set(posKey(), {}); topologyKey = ''; renderMap(); fit(); });
    const setMenu = open => { $('#exportMenu').classList.toggle('open', open); $('#exportBtn').setAttribute('aria-expanded', String(open)); };
    $('#exportBtn').addEventListener('click', ev => { ev.stopPropagation(); setMenu(!$('#exportMenu').classList.contains('open')); });
    app.addEventListener('click', () => setMenu(false));
    app.querySelectorAll('[data-export]').forEach(b => b.addEventListener('click', () => exportMap(b.dataset.export)));
    svg.addEventListener('wheel', onWheel, { passive: false });
    svg.addEventListener('pointerdown', onPointerDown);
    svg.addEventListener('pointermove', onPointerMove);
    svg.addEventListener('pointerup', () => endDrag(true));
    svg.addEventListener('pointercancel', () => endDrag(false));
    svg.addEventListener('lostpointercapture', () => endDrag(false));

    const keyTarget = page ? document : host;
    keyTarget.addEventListener('keydown', onKey);
    // Resizing refits the map until the viewer pans or zooms it themselves.
    const resizeObs = new ResizeObserver(() => { if (state.snap && !state.userMoved) state.fitted = fit() || state.fitted; });
    resizeObs.observe(host);
    const seenObs = typeof IntersectionObserver !== 'undefined'
      ? new IntersectionObserver(entries => { state.visible = entries.some(e => e.isIntersecting); wake(); }) : null;
    seenObs?.observe(host);
    const onVisibility = () => wake();
    document.addEventListener('visibilitychange', onVisibility);
    const schemeMQ = matchMedia('(prefers-color-scheme: dark)');
    const onScheme = () => { if (state.snap) { renderLegend(); renderAll(); renderActivity(); } };
    schemeMQ.addEventListener('change', onScheme);
    const onMotion = () => { if (store.get('flow', null) == null) { state.flow = !motionMQ.matches; renderButtons(); wake(); } };
    motionMQ.addEventListener('change', onMotion);

    // Shareable views (page mode): ?journey=checkout&group=team&q=kind:database&theme=light&select=orders-db&legend=icons&flow=1&values=0&live=0
    if (page && typeof location !== 'undefined') {
      const params = new URLSearchParams(location.search);
      if (params.has('group')) state.groupBy = params.get('group');
      if (params.has('q')) { state.query = params.get('q'); $('#search').value = state.query; }
      if (params.has('theme')) host.dataset.theme = params.get('theme');
      if (params.get('live') === '0') opts.stream = false;
      if (params.has('select')) opts.select = params.get('select');
      if (params.get('legend') === 'icons') state.legendIcons = true;
      if (params.has('flow')) state.flow = params.get('flow') !== '0';
      if (params.has('journey')) state.journey = params.get('journey').trim();
      if (params.has('values')) state.values = params.get('values') !== '0';
    }
    if (opts.journey) state.journey = String(opts.journey).trim();
    state.positions = stored(posKey(), {});
    if (opts.theme) host.dataset.theme = opts.theme;

    // API links only make sense when teia's API is reachable from the browser.
    app.querySelectorAll('[data-api]').forEach(a => { if (opts.api) a.setAttribute('href', opts.api + a.dataset.api); else a.remove(); });
    if (!page) {
      $('#stepsBtn').addEventListener('click', () => { const open = !$('aside').classList.contains('open'); setDrawer(open); if (open) showTab('steps'); });
      $('#drawerClose').addEventListener('click', () => setDrawer(false));
      setDrawer(false);
      host.tabIndex = 0;
    }

    initSVG();
    renderLegend();
    renderButtons();
    renderLive();
    const ticker = setInterval(renderLive, 1000);
    if (opts.api) {
      load(aborter.signal)
        .then(() => { if (!destroyed && opts.select) locate(opts.select, { move: false }); })
        .catch(() => { /* the stream will bring it */ })
        .finally(() => { if (destroyed) return; if (opts.stream !== false) connect(); else { state.live = false; renderButtons(); renderLive(); } });
    }

    return {
      // update feeds a snapshot from outside (the Grafana panel).
      update(snap, changes) { apply(snap, true); if (changes) pushActivity(changes, state.activity.length > 0); },
      // setStatus reports the host's own query state (the panel's data.state).
      setStatus(error) { state.error = error || ''; renderLive(); },
      setOptions({ groupBy, flow, journey, values } = {}) {
        // A journey from the host (a dashboard variable) is the viewer's choice.
        if (journey != null) setJourney(journey);
        if (values != null && store.get('values', null) == null && values !== state.values) { state.values = values; renderButtons(); if (state.snap) renderReasons(); }
        if (groupBy != null && store.get('groupBy', null) == null && groupBy !== state.groupBy) { state.groupBy = groupBy; state.expanded.clear(); if (state.snap) { renderGroupBy(); renderMap(); fit(); } }
        if (flow != null && store.get('flow', null) == null && flow !== state.flow) { state.flow = flow; renderButtons(); wake(); }
      },
      setTheme(theme) { host.dataset.theme = theme; renderLegend(); if (state.snap) { renderAll(); renderActivity(); } },
      destroy() {
        if (destroyed) return;
        destroyed = true;
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
