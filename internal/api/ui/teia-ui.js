/* teia UI: the communication map as one dependency-free component.
 *
 * The same file runs the page teia serves and the Grafana panel
 * (plugins/grafana). It renders into a shadow root, so its styles never leak
 * into the host page and the host's never leak in.
 *
 *   const ui = TeiaUI.mount(element, { mode: 'page', api: 'api/v1/' });   // fetch + live stream
 *   const ui = TeiaUI.mount(element, { mode: 'panel' }); ui.update(snap); // data pushed in
 */
(function (global) {
  'use strict';

  const STYLE = `:host {
  --bg: #050b1a; --bg-2: #081227; --panel: #0b1630; --panel-2: #0f1d3d; --line: #1b2b52; --line-2: #26396a;
  --ink: #e8eefb; --ink-2: #a9b6d3; --muted: #6f7fa3; --accent: #7ee8fa; --accent-2: #1148ac; --accent-ink: #04122b;
  --healthy: #22c55e; --degraded: #f5a524; --failing: #ef4444; --unknown: #64748b;
  --ghost: #f5a524; --stale: #64748b; --edge: #3b5b94; --particle: #7ee8fa;
  --card: #0e1a36; --card-hi: #13244a; --grid: rgba(126, 232, 250, .06);
  --shadow: 0 8px 28px rgba(0, 0, 0, .45);
  --critical: #ef4444; --warning: #f5a524; --info: #60a5fa;
  --radius: 12px;
  color-scheme: dark;
}
@media (prefers-color-scheme: light) {
  :host(:not([data-theme="dark"])) {
    --bg: #f3f6fb; --bg-2: #eaf0f8; --panel: #ffffff; --panel-2: #f6f8fc; --line: #dde4f0; --line-2: #c9d4e6;
    --ink: #0d1730; --ink-2: #3c4b6b; --muted: #6b7a99; --accent: #1148ac; --accent-2: #1148ac; --accent-ink: #ffffff;
    --edge: #9fb0cf; --particle: #1148ac; --card: #ffffff; --card-hi: #f2f6ff; --grid: rgba(17, 72, 172, .07);
    --shadow: 0 6px 22px rgba(15, 30, 70, .12);
    color-scheme: light;
  }
}
:host([data-theme="light"]) {
  --bg: #f3f6fb; --bg-2: #eaf0f8; --panel: #ffffff; --panel-2: #f6f8fc; --line: #dde4f0; --line-2: #c9d4e6;
  --ink: #0d1730; --ink-2: #3c4b6b; --muted: #6b7a99; --accent: #1148ac; --accent-2: #1148ac; --accent-ink: #ffffff;
  --edge: #9fb0cf; --particle: #1148ac; --card: #ffffff; --card-hi: #f2f6ff; --grid: rgba(17, 72, 172, .07);
  --shadow: 0 6px 22px rgba(15, 30, 70, .12);
  color-scheme: light;
}

* { box-sizing: border-box; }
:host { display: block; height: 100%; }
.teia {
  height: 100%; position: relative;
  background: var(--bg); color: var(--ink);
  font: 13px/1.45 Inter, ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  display: grid; grid-template-rows: auto 1fr; overflow: hidden;
}
button, input, select { font: inherit; color: inherit; }
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
.live.on i { background: var(--healthy); box-shadow: 0 0 0 0 rgba(34, 197, 94, .6); animation: beat 2s infinite; }
.live.paused i { background: var(--degraded); }
@keyframes beat { 0% { box-shadow: 0 0 0 0 rgba(34, 197, 94, .55); } 70% { box-shadow: 0 0 0 8px rgba(34, 197, 94, 0); } 100% { box-shadow: 0 0 0 0 rgba(34, 197, 94, 0); } }
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
}
.toast.warn { border-left-color: var(--degraded); } .toast.bad { border-left-color: var(--failing); } .toast.good { border-left-color: var(--healthy); }
.toast small { color: var(--muted); display: block; font-size: 11px; }
@keyframes toast-in { from { transform: translateY(-6px); opacity: 0; } to { transform: none; opacity: 1; } }
.empty-state { position: absolute; inset: 0; display: grid; place-items: center; color: var(--muted); }

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
  background: var(--panel-2); cursor: pointer;
}
.card-f:hover { border-color: var(--line-2); }
.card-f.critical { border-left-color: var(--critical); } .card-f.warning { border-left-color: var(--warning); }
.card-f .meta { font-size: 10.5px; color: var(--muted); text-transform: uppercase; letter-spacing: .06em; }
.card-f h4 { margin: 3px 0 5px; font-size: 13.5px; }
.card-f p { margin: 0 0 6px; color: var(--ink-2); }
.card-f .next { color: var(--ink); }
.snippet { position: relative; }
.snippet pre {
  background: var(--bg); border: 1px solid var(--line); border-radius: 8px; padding: 9px 10px; overflow-x: auto;
  font: 11.5px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; margin: 6px 0 0; color: var(--ink);
}
.snippet .copy { position: absolute; right: 6px; top: 6px; height: 24px; font-size: 11px; padding: 0 8px; }
details summary { cursor: pointer; color: var(--accent); font-size: 12px; margin-top: 4px; }
.hero { display: flex; gap: 12px; align-items: center; margin-bottom: 12px; }
.hero .tile { width: 46px; height: 46px; border-radius: 12px; display: grid; place-items: center; flex: none; }
.hero .tile svg { width: 26px; height: 26px; color: #fff; }
.hero h2 { margin: 0; font-size: 18px; word-break: break-all; }
.pill { display: inline-flex; align-items: center; gap: 5px; border-radius: 999px; padding: 1px 8px; font-size: 11px; border: 1px solid var(--line-2); color: var(--ink-2); }
.pill .dot { width: 7px; height: 7px; border-radius: 50%; }
.kv { display: grid; grid-template-columns: 130px 1fr; gap: 6px 10px; }
.kv dt { color: var(--muted); }
.kv dd { margin: 0; word-break: break-word; }
.labels { display: flex; flex-wrap: wrap; gap: 5px; }
.label { font-size: 11px; border: 1px solid var(--line-2); border-radius: 6px; padding: 1px 6px; background: var(--bg); cursor: pointer; }
.label b { color: var(--muted); font-weight: 500; }
.rel { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 8px; cursor: pointer; }
.rel:hover { background: var(--panel-2); }
.rel .dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.rel span { color: var(--muted); margin-left: auto; font-size: 11.5px; }
.explain { color: var(--ink-2); margin: 4px 0 10px; }
.feed .ev { display: grid; grid-template-columns: 58px 18px 1fr; gap: 6px; padding: 7px 0; border-bottom: 1px solid var(--line); align-items: start; }
.feed .ev time { color: var(--muted); font-size: 11.5px; }
.feed .ev svg { width: 15px; height: 15px; margin-top: 2px; }
.feed .ev.new { animation: toast-in .4s ease-out; }
.src { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; padding: 8px 0; border-bottom: 1px solid var(--line); }
.src small { display: block; color: var(--muted); }
.state { font-size: 10.5px; text-transform: uppercase; letter-spacing: .05em; border-radius: 6px; padding: 1px 6px; border: 1px solid var(--line-2); white-space: nowrap; }
.state.ok { color: var(--healthy); } .state.degraded { color: var(--degraded); } .state.error { color: var(--failing); }
.muted { color: var(--muted); }

@media (max-width: 960px) {
  .teia.page { overflow: auto; }
  main { grid-template-columns: 1fr; grid-template-rows: 64vh auto; }
  aside { border-left: 0; border-top: 1px solid var(--line); }
  .legend { display: none; }
  .search input { width: 160px; }
  header { padding: 10px 16px; }
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
.teia.panel .legend.hidden { display: none; }
.stepsBtn { display: none; }
.teia.panel .stepsBtn { display: inline-flex; }
.stepsBtn .count { background: var(--degraded); color: #111; border-radius: 999px; padding: 0 6px; font-size: 11px; font-weight: 700; }
`;

  const MARKUP = `<header>
  <div class="brand">
    <svg viewBox="0 0 32 32" aria-hidden="true"><circle cx="16" cy="16" r="15" fill="#1148ac"/><g fill="none" stroke="#7ee8fa" stroke-width="1.6"><circle cx="16" cy="16" r="5"/><circle cx="16" cy="16" r="10"/><path d="M16 3v26M3 16h26M7 7l18 18M25 7L7 25"/></g></svg>
    <div><b>teia</b><small>service communication map</small></div>
  </div>
  <div class="rings" id="rings"></div>
  <div class="spacer"></div>
  <div class="live" id="live" title="Live updates from the discovery engine"><i></i><span>connecting…</span></div>
  <label class="search">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7"/><path d="M20 20l-4-4"/></svg>
    <input id="search" placeholder="Search…  e.g. team:checkout" autocomplete="off" spellcheck="false">
  </label>
  <div class="ctl">
    <select class="btn" id="groupBy" title="Group nodes by a label"></select>
    <button class="btn" id="flowBtn" title="Animate traffic (space)"></button>
    <button class="btn" id="liveBtn" title="Follow live updates"></button>
    <button class="btn stepsBtn" id="stepsBtn" title="Next steps">Next steps <span class="count" id="stepsBadge">0</span></button>
    <div class="menu" id="exportMenu">
      <button class="btn" id="exportBtn" title="Export">Export ▾</button>
      <div class="menu-list">
        <button data-export="png">Map as PNG</button>
        <button data-export="svg">Map as SVG</button>
        <a data-api="export/matrix.csv" download>Flow matrix (CSV)</a>
        <a data-api="export/matrix.csv?lang=pt-BR" download>Matriz de fluxos (CSV, pt-BR)</a>
        <a data-api="snapshot" target="_blank" rel="noopener">Snapshot (JSON)</a>
      </div>
    </div>
  </div>
</header>
<main>
  <div id="stage">
    <svg id="map" aria-label="Communication map"></svg>
    <div class="floating filters" id="filters"></div>
    <div class="floating legend" id="legend"></div>
    <div class="floating zoom">
      <button class="btn" id="zoomIn" title="Zoom in">+</button>
      <button class="btn" id="zoomOut" title="Zoom out">−</button>
      <button class="btn" id="fitBtn" title="Fit (F)">⤢</button>
      <button class="btn" id="resetBtn" title="Reset layout">↺</button>
    </div>
    <div class="toasts" id="toasts"></div>
  </div>
  <aside>
    <nav class="tabs">
      <button class="tab on" data-tab="steps">Next steps <span class="count" id="stepsCount">0</span></button>
      <button class="tab" data-tab="details">Details</button>
      <button class="tab" data-tab="activity">Live <span class="count" id="activityCount">0</span></button>
      <button class="tab" data-tab="sources">Sources</button>
      <button class="btn icon drawer-close" id="drawerClose" title="Close">✕</button>
    </nav>
    <section class="pane on" id="pane-steps"></section>
    <section class="pane" id="pane-details"><p class="muted">Click a service, a datastore or a communication on the map.</p></section>
    <section class="pane feed" id="pane-activity"><p class="muted">Changes between discovery rounds show up here as they happen.</p></section>
    <section class="pane" id="pane-sources"></section>
  </aside>
</main>`;

  function mount(host, opts = {}) {
    opts = { mode: 'page', ...opts };
    if (opts.api && !opts.api.endsWith('/')) opts.api += '/';
    const root = host.shadowRoot || host.attachShadow({ mode: 'open' });
    root.innerHTML = `<style>${STYLE}</style><div class="teia ${opts.mode}">${MARKUP}</div>`;
    const keyTarget = opts.mode === 'panel' ? host : document;
    const timers = [];
    let raf = 0, destroyed = false, es = null;


    // ---------------------------------------------------------------- constants
    const NODE_W = 196, NODE_H = 62, COL_GAP = 300, ROW_GAP = 112;
    const SVGNS = 'http://www.w3.org/2000/svg';
    const prefix = 'teia.' + (opts.storageKey ? opts.storageKey + '.' : '');
    const store = {
      get(k, d) { try { const v = localStorage.getItem(prefix + k); return v == null ? d : JSON.parse(v); } catch { return d; } },
      set(k, v) { try { localStorage.setItem(prefix + k, JSON.stringify(v)); } catch { /* private mode */ } },
    };
    const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;

    const ICON = {
      service: '<path d="M12 2.8l8 4.6v9.2l-8 4.6-8-4.6V7.4z"/><path d="M12 12l8-4.6M12 12v9.2M12 12L4 7.4"/>',
      database: '<ellipse cx="12" cy="5.5" rx="7.5" ry="2.8"/><path d="M4.5 5.5v13c0 1.6 3.4 2.8 7.5 2.8s7.5-1.2 7.5-2.8v-13"/><path d="M4.5 12c0 1.6 3.4 2.8 7.5 2.8s7.5-1.2 7.5-2.8"/>',
      cache: '<path d="M13.2 2.5L5 13.5h6.2l-1 8 8.3-11h-6.3z"/>',
      queue: '<path d="M12 3l9 4.8-9 4.8-9-4.8z"/><path d="M3 12.2l9 4.8 9-4.8"/><path d="M3 16.4l9 4.8 9-4.8"/>',
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
    const KIND_CATEGORY = { service: 'service', database: 'relational', cache: 'cache', queue: 'queue', external: 'external', unknown: 'unknown', group: 'group' };
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
    const categoryOf = n => n.group ? 'group' : (TECH[n.tech]?.cat || KIND_CATEGORY[n.kind] || 'unknown');
    const iconOf = n => n.group ? ICON.group : (CATEGORY[categoryOf(n)]?.icon || ICON.unknown);
    const techLabel = n => TECH[n.tech]?.label || n.kind;

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
    const HEALTH_LABEL = { healthy: 'healthy', degraded: 'degraded', failing: 'failing', unknown: 'no traffic data' };
    const STATE_LABEL = { ok: 'declared and observed', ghost: 'observed, not declared', stale: 'declared, not observed' };
    const VIS_EXPLAIN = {
      full: 'teia sees it first-hand: its own SDK, sidecar, eBPF probe or database explorer reports it.',
      partial: 'teia only knows it through what its peers report. Whatever it calls, nobody tells teia.',
      none: 'teia knows it exists (inventory or a flows file) but has seen no traffic to or from it.',
    };

    const $ = s => root.querySelector(s);
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
      const s = sv('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.8', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', class: cls });
      s.innerHTML = paths;
      return s;
    };
    const fmtRate = r => r >= 100 ? r.toFixed(0) : r >= 10 ? r.toFixed(1) : r.toFixed(2);
    const pct = v => (v * 100).toFixed(v > 0 && v < 0.1 ? 1 : 0) + '%';
    const css = name => getComputedStyle(host).getPropertyValue(name).trim();
    const timeOf = iso => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });

    // -------------------------------------------------------------------- state
    const state = {
      snap: null, pending: null, connected: false,
      view: { x: 0, y: 0, k: 1 }, fitted: false,
      positions: store.get('positions', {}),
      selected: null, hover: null,
      states: store.get('states', { ok: true, ghost: true, stale: true }),
      signals: new Set(store.get('signals', [])),
      query: '',
      groupBy: store.get('groupBy', ''),
      expanded: new Set(),
      flow: store.get('flow', !reducedMotion),
      live: true,
      tab: 'steps',
      activity: [],
    };

    // ------------------------------------------------------------- derivations
    let index = { nodes: new Map(), sourceType: new Map() };

    function reindex(snap) {
      const nodes = new Map(snap.nodes.map(n => [n.id, n]));
      const sourceType = new Map(snap.sources.map(s => [s.name, s.type]));
      index = { nodes, sourceType };
    }

    function nodeSignals(n) {
      const out = new Set();
      for (const [k, v] of Object.entries(n.labels || {})) if (k.startsWith('teia.signal.') && v === 'true') out.add(k.slice(12));
      for (const s of n.sources || []) {
        const t = index.sourceType.get(s);
        if (t === 'docker' || t === 'kubernetes') out.add('inventory');
        else if (t === 'declared') out.add('declared');
        else if (t === 'postgres' && n.kind === 'database') out.add('database');
      }
      for (const e of state.snap.edges) {
        if ((e.from === n.id || e.to === n.id) && e.observed) {
          for (const sig of e.signals || []) if (sig === 'logs' || sig === 'database') out.add(sig);
        }
      }
      return [...out].filter(s => SIGNAL[s]);
    }

    const HEALTH_RANK = { failing: 3, degraded: 2, healthy: 1, unknown: 0 };
    const userLabel = k => !k.startsWith('teia.') && k !== 'docker.container' && k !== 'backstage.ref';

    function labelKeys(snap) {
      const keys = new Map();
      for (const n of snap.nodes) for (const k of Object.keys(n.labels || {})) if (userLabel(k)) keys.set(k, (keys.get(k) || 0) + 1);
      return [...keys.entries()].filter(([, c]) => c > 1).map(([k]) => k).sort();
    }

    // Builds what is drawn: grouping by a label folds nodes into one card and
    // sums the traffic between groups.
    function buildView(snap) {
      const key = state.groupBy;
      const groupOf = id => {
        if (!key) return null;
        const v = index.nodes.get(id)?.labels?.[key];
        if (!v || state.expanded.has(v)) return null;
        return 'group:' + v;
      };
      const nodes = new Map();
      for (const n of snap.nodes) {
        const g = groupOf(n.id);
        if (!g) { nodes.set(n.id, { ...n, title: n.id, signals: nodeSignals(n) }); continue; }
        let gn = nodes.get(g);
        if (!gn) {
          gn = { id: g, group: true, kind: 'group', title: g.slice(6), labelKey: key, members: [], health: 'unknown', visibility: 'none', requestRate: 0, signals: [], labels: { [key]: g.slice(6) } };
          nodes.set(g, gn);
        }
        gn.members.push(n);
        if (HEALTH_RANK[n.health] > HEALTH_RANK[gn.health]) gn.health = n.health;
        if (n.visibility === 'full' || (n.visibility === 'partial' && gn.visibility === 'none')) gn.visibility = n.visibility;
        gn.signals = [...new Set([...gn.signals, ...nodeSignals(n)])];
      }
      const edges = new Map();
      const internal = new Map();
      for (const e of snap.edges) {
        const from = groupOf(e.from) || e.from, to = groupOf(e.to) || e.to;
        if (from === to) { internal.set(from, (internal.get(from) || 0) + 1); continue; }
        const id = from + '->' + to;
        let ve = edges.get(id);
        if (!ve) {
          ve = { id, from, to, members: [], requestRate: 0, failed: 0, signals: new Set(), states: new Set(), declared: false, observed: false, protocol: e.protocol, reason: e.reason, port: e.port, sources: new Set() };
          edges.set(id, ve);
        }
        ve.members.push(e);
        ve.requestRate += e.requestRate || 0;
        ve.failed += (e.requestRate || 0) * (e.errorRate || 0);
        (e.signals || []).forEach(s => ve.signals.add(s));
        (e.sources || []).forEach(s => ve.sources.add(s));
        ve.states.add(e.state);
        ve.declared ||= e.declared; ve.observed ||= e.observed;
        if (HEALTH_RANK[e.health] > HEALTH_RANK[ve.health || 'unknown']) ve.health = e.health;
      }
      for (const ve of edges.values()) {
        ve.errorRate = ve.requestRate > 0 ? ve.failed / ve.requestRate : 0;
        ve.state = ve.states.has('ghost') ? 'ghost' : ve.states.has('ok') ? 'ok' : 'stale';
        ve.health = ve.health || 'unknown';
        ve.signals = [...ve.signals]; ve.sources = [...ve.sources];
      }
      for (const [g, c] of internal) if (nodes.get(g)) nodes.get(g).internal = c;
      for (const n of nodes.values()) if (n.group) {
        n.requestRate = n.members.reduce((a, m) => a + (m.requestRate || 0), 0);
      }
      return { nodes, edges };
    }

    function edgeVisible(e) {
      if (!state.states[e.state]) return false;
      if (state.signals.size && !e.signals.some(s => state.signals.has(s))) return false;
      return true;
    }

    // Search: free text matches names; key:value matches labels and fields.
    function matcher(q) {
      const terms = q.trim().toLowerCase().split(/\s+/).filter(Boolean);
      if (!terms.length) return null;
      return n => terms.every(t => {
        const i = t.indexOf(':');
        if (i > 0) {
          const k = t.slice(0, i), v = t.slice(i + 1);
          const field = { kind: n.kind, tech: n.tech || '', category: categoryOf(n), health: n.health, visibility: n.visibility, id: n.id }[k];
          if (field != null) return String(field).toLowerCase().includes(v);
          const labels = n.group ? n.members.map(m => m.labels || {}) : [n.labels || {}];
          return labels.some(l => Object.entries(l).some(([lk, lv]) => lk.toLowerCase() === k && String(lv).toLowerCase().includes(v)));
        }
        return n.title.toLowerCase().includes(t) || (n.group && n.members.some(m => m.id.includes(t)));
      });
    }

    // -------------------------------------------------------------------- layout
    // Columns by longest path from callers so every edge points right; nodes
    // nobody calls sit just before their nearest callee; rows ordered by a few
    // barycenter sweeps to keep lines short and crossings rare.
    function layered(view) {
      const ids = [...view.nodes.keys()];
      const edges = [...view.edges.values()];
      const depth = new Map(ids.map(id => [id, 0]));
      for (let i = 0; i < ids.length; i++) {
        let changed = false;
        for (const e of edges) {
          const d = depth.get(e.from) + 1;
          if (d > depth.get(e.to) && d < ids.length) { depth.set(e.to, d); changed = true; }
        }
        if (!changed) break;
      }
      const called = new Set(edges.map(e => e.to));
      for (const id of ids) {
        if (called.has(id)) continue;
        const t = edges.filter(e => e.from === id).map(e => depth.get(e.to));
        if (t.length) depth.set(id, Math.max(0, Math.min(...t) - 1));
      }
      const cols = [];
      for (const id of ids.sort()) (cols[depth.get(id)] ||= []).push(id);
      const row = new Map();
      cols.forEach(c => c && c.forEach((id, i) => row.set(id, i)));
      const bary = (id, dir) => {
        const peers = edges.filter(e => dir > 0 ? e.to === id : e.from === id).map(e => row.get(dir > 0 ? e.from : e.to));
        return peers.length ? peers.reduce((a, b) => a + b, 0) / peers.length : row.get(id);
      };
      for (let sweep = 0; sweep < 4; sweep++) {
        const dir = sweep % 2 ? -1 : 1;
        const order = dir > 0 ? cols : [...cols].reverse();
        for (const c of order) {
          if (!c) continue;
          c.sort((a, b) => bary(a, dir) - bary(b, dir));
          c.forEach((id, i) => row.set(id, i));
        }
      }
      const pos = {};
      cols.forEach((c, x) => c && c.forEach((id, y) => { pos[id] = { x: x * COL_GAP, y: (y - (c.length - 1) / 2) * ROW_GAP }; }));
      return pos;
    }

    // ------------------------------------------------------------------- the map
    const svg = $('#map');
    const stage = $('#stage');
    let viewport, layerEdges, layerMarks, layerParticles, layerLabels, layerNodes;
    const nodeEls = new Map(), edgeEls = new Map();
    let currentView = null, layoutPos = {}, topologyKey = '';

    function initSVG() {
      svg.innerHTML = '';
      const defs = sv('defs');
      for (const h of ['healthy', 'degraded', 'failing', 'unknown', 'ghost', 'accent']) {
        const m = sv('marker', { id: 'arrow-' + h, viewBox: '0 0 10 10', refX: '9', refY: '5', markerWidth: '7', markerHeight: '7', orient: 'auto-start-reverse' });
        m.append(sv('path', { d: 'M0,1 L9,5 L0,9 z', class: 'arrow ' + h }));
        defs.append(m);
      }
      const glow = sv('filter', { id: 'glow', x: '-50%', y: '-50%', width: '200%', height: '200%' });
      glow.innerHTML = '<feGaussianBlur stdDeviation="2.2" result="b"/><feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge>';
      defs.append(glow);
      const style = sv('style');
      style.textContent = SVG_CSS;
      defs.append(style);
      svg.append(defs);
      viewport = sv('g', { id: 'viewport' });
      layerEdges = sv('g'); layerMarks = sv('g'); layerParticles = sv('g', { class: 'particles' }); layerLabels = sv('g'); layerNodes = sv('g');
      viewport.append(layerEdges, layerParticles, layerMarks, layerLabels, layerNodes);
      svg.append(viewport);
    }

    const SVG_CSS = `
      .card { fill: var(--card); stroke: var(--line-2); stroke-width: 1.2; transition: fill .3s, stroke .3s; }
      .node:hover .card, .node.sel .card { fill: var(--card-hi); stroke: var(--accent); }
      .node.vis-partial .card { stroke-dasharray: 6 4; }
      .node.vis-none .card { stroke-dasharray: 2 4; }
      .node.vis-none { opacity: .78; }
      .tile { transition: fill .5s ease; }
      .h-healthy .tile { fill: var(--healthy); } .h-degraded .tile { fill: var(--degraded); }
      .h-failing .tile { fill: var(--failing); } .h-unknown .tile { fill: var(--unknown); }
      .kind-group .card { stroke: var(--accent); stroke-width: 1.4; }
      .node .icon { color: #fff; }
      .node .title { fill: var(--ink); font-weight: 650; font-size: 13.5px; }
      .node .sub { fill: var(--muted); font-size: 11px; }
      .node .badge circle { fill: var(--bg-2); stroke: var(--line-2); }
      .node .badge svg { color: var(--ink-2); }
      .node .alert circle { fill: var(--degraded); stroke: var(--bg); stroke-width: 2; }
      .node .alert.critical circle { fill: var(--failing); }
      .node .alert text { fill: #111; font-size: 10.5px; font-weight: 700; text-anchor: middle; }
      .node .halo { fill: none; stroke: var(--failing); stroke-width: 2; opacity: 0; }
      .flowing .h-failing .halo { animation: halo 1.6s ease-out infinite; }
      @keyframes halo { 0% { opacity: .7; transform: scale(1); } 100% { opacity: 0; transform: scale(1.12); } }
      .node.match .card { stroke: var(--accent); stroke-width: 2.2; }
      .node.enter { animation: enter .5s ease-out; }
      @keyframes enter { from { opacity: 0; } to { opacity: 1; } }
      .edge path.line { fill: none; stroke-linecap: round; transition: stroke .5s, stroke-width .3s; }
      .edge path.hit { fill: none; stroke: transparent; stroke-width: 14; cursor: pointer; }
      .edge.h-healthy path.line { stroke: var(--healthy); } .edge.h-degraded path.line { stroke: var(--degraded); }
      .edge.h-failing path.line { stroke: var(--failing); } .edge.h-unknown path.line { stroke: var(--edge); }
      .edge path.line { stroke-opacity: .7; }
      .edge.s-ghost path.line { stroke-dasharray: 8 6; }
      .edge.s-stale path.line { stroke-dasharray: 2 6; stroke: var(--stale); stroke-opacity: .8; }
      .edge.sel path.line, .edge:hover path.line { stroke-opacity: 1; }
      .arrow.healthy { fill: var(--healthy); } .arrow.degraded { fill: var(--degraded); } .arrow.failing { fill: var(--failing); }
      .arrow.unknown { fill: var(--edge); } .arrow.ghost { fill: var(--stale); } .arrow.accent { fill: var(--accent); }
      .mark circle { fill: var(--ghost); stroke: var(--bg); stroke-width: 2; }
      .mark text { fill: #111; font-size: 10px; font-weight: 800; text-anchor: middle; }
      .elabel rect { fill: var(--panel); stroke: var(--line-2); }
      .elabel text { fill: var(--ink); font-size: 11px; }
      .particles circle { fill: var(--particle); }
      .particles circle.ghost { fill: var(--ghost); }
      .particles circle.err { fill: var(--failing); }
      .particles circle.faint { opacity: .55; }
      .dim { opacity: .14; transition: opacity .2s; }
    `;

    function renderMap() {
      const snap = state.snap;
      const view = buildView(snap);
      currentView = view;
      const key = [...view.nodes.keys()].sort().join('|') + '#' + [...view.edges.keys()].sort().join('|');
      if (key !== topologyKey) { layoutPos = layered(view); topologyKey = key; }
      const posOf = id => state.positions[id] || layoutPos[id] || { x: 0, y: 0 };
      const match = matcher(state.query);

      // nodes
      const seen = new Set();
      for (const n of view.nodes.values()) {
        seen.add(n.id);
        let g = nodeEls.get(n.id);
        if (!g) { g = createNode(n); nodeEls.set(n.id, g); layerNodes.append(g); }
        updateNode(g, n, match);
        const p = posOf(n.id);
        g.setAttribute('transform', `translate(${p.x},${p.y})`);
        g._pos = p;
      }
      for (const [id, g] of nodeEls) if (!seen.has(id)) { g.remove(); nodeEls.delete(id); }

      // edges
      const eSeen = new Set();
      layerMarks.innerHTML = '';
      for (const e of view.edges.values()) {
        const visible = edgeVisible(e);
        eSeen.add(e.id);
        let g = edgeEls.get(e.id);
        if (!g) { g = createEdge(e); edgeEls.set(e.id, g); layerEdges.append(g); }
        g._edge = e;
        g.style.display = visible ? '' : 'none';
        g.setAttribute('class', `edge s-${e.state} h-${e.health}${state.selected?.id === e.id ? ' sel' : ''}`);
        routeEdge(g, posOf(e.from), posOf(e.to), view.edges.has(e.to + '->' + e.from) ? 7 : 0);
        if (visible && e.state === 'ghost') addGhostMark(g);
      }
      for (const [id, g] of edgeEls) if (!eSeen.has(id)) { g.remove(); edgeEls.delete(id); }

      rebuildParticles();
      applyHighlight();
      renderEdgeLabel();
      if (!state.fitted && view.nodes.size) { fit(); state.fitted = true; }
    }

    function createNode(n) {
      const g = sv('g', { class: 'node enter' });
      g.dataset.id = n.id;
      const halo = sv('rect', { class: 'halo', x: -NODE_W / 2, y: -NODE_H / 2, width: NODE_W, height: NODE_H, rx: 15, style: 'transform-box: fill-box; transform-origin: center' });
      const card = sv('rect', { class: 'card', x: -NODE_W / 2, y: -NODE_H / 2, width: NODE_W, height: NODE_H, rx: 15 });
      const tile = sv('rect', { class: 'tile', x: -NODE_W / 2 + 10, y: -20, width: 40, height: 40, rx: 11 });
      const icon = sv('g', { class: 'icon', transform: `translate(${-NODE_W / 2 + 18},-12)` });
      const title = sv('text', { class: 'title', x: -NODE_W / 2 + 60, y: -3 });
      const sub = sv('text', { class: 'sub num', x: -NODE_W / 2 + 60, y: 14 });
      const badges = sv('g', { class: 'badges' });
      const alert = sv('g', { class: 'alert', transform: `translate(${NODE_W / 2 - 4},${-NODE_H / 2 + 4})` });
      g.append(halo, card, tile, icon, title, sub, badges, alert);
      g._parts = { icon, title, sub, badges, alert };
      g.addEventListener('pointerdown', ev => startNodeDrag(ev, g));
      g.addEventListener('mouseenter', () => { state.hover = g.dataset.id; applyHighlight(); });
      g.addEventListener('mouseleave', () => { state.hover = null; applyHighlight(); });
      g.addEventListener('dblclick', ev => { ev.stopPropagation(); toggleGroup(g.dataset.id); });
      return g;
    }

    function updateNode(g, n, match) {
      const p = g._parts;
      const sel = state.selected?.id === n.id;
      g.setAttribute('class', `node kind-${n.kind} h-${n.health || 'unknown'} vis-${n.visibility}${sel ? ' sel' : ''}${match && match(n) ? ' match' : ''}${g.classList.contains('enter') ? ' enter' : ''}`);
      setTimeout(() => g.classList.remove('enter'), 600);
      const cat = categoryOf(n);
      if (p.cat !== cat) { p.icon.innerHTML = ''; p.icon.append(iconSVG(iconOf(n))); p.icon.firstChild.setAttribute('width', 24); p.icon.firstChild.setAttribute('height', 24); p.cat = cat; }
      const name = n.title.length > 17 ? n.title.slice(0, 16) + '…' : n.title;
      p.title.textContent = name;
      let sub = n.group ? `${n.members.length} workload${n.members.length === 1 ? '' : 's'}` : techLabel(n);
      if (n.requestRate > 0) sub += ` · ${fmtRate(n.requestRate)}/s`;
      if (n.errorRate > 0.0005 && !n.group) sub += ` · ${pct(n.errorRate)} err`;
      if (n.group && n.internal) sub += ` · ${n.internal} inside`;
      p.sub.textContent = sub.length > 30 ? sub.slice(0, 29) + '…' : sub;
      // signal badges along the bottom edge
      p.badges.innerHTML = '';
      n.signals.slice(0, 6).forEach((s, i) => {
        const b = sv('g', { class: 'badge', transform: `translate(${-NODE_W / 2 + 70 + i * 21},${NODE_H / 2})` });
        b.append(sv('circle', { r: 9 }));
        const ic = iconSVG(SIGNAL[s].icon);
        ic.setAttribute('x', -6); ic.setAttribute('y', -6); ic.setAttribute('width', 12); ic.setAttribute('height', 12);
        b.append(ic);
        const t = sv('title'); t.textContent = 'seen by ' + SIGNAL[s].label; b.append(t);
        p.badges.append(b);
      });
      // findings bubble
      const fs = (state.snap.findings || []).filter(f => n.group ? n.members.some(m => m.id === f.subject) : f.subject === n.id);
      p.alert.innerHTML = '';
      if (fs.length) {
        p.alert.setAttribute('class', 'alert' + (fs.some(f => f.severity === 'critical') ? ' critical' : ''));
        p.alert.append(sv('circle', { r: 9 }));
        const t = sv('text', { y: 3.6 }); t.textContent = fs.length; p.alert.append(t);
      }
      let tt = g.querySelector('title');
      if (!tt) { tt = sv('title'); g.append(tt); }
      tt.textContent = `${n.title} · ${n.group ? 'group' : CATEGORY[categoryOf(n)]?.label || n.kind}${n.tech ? ' (' + techLabel(n) + ')' : ''} · ${HEALTH_LABEL[n.health || 'unknown']} · ${n.visibility} visibility`;
    }

    function createEdge(e) {
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
      const w = e.requestRate > 0 ? 1.6 + Math.min(3, Math.log10(1 + e.requestRate * 4) * 1.5) : 1.5;
      g._line.setAttribute('d', d); g._hit.setAttribute('d', d);
      g._line.setAttribute('stroke-width', w.toFixed(2));
      const marker = e.state === 'stale' ? 'ghost' : e.health;
      g._line.setAttribute('marker-end', `url(#arrow-${marker})`);
      g._len = null;
    }

    function addGhostMark(g) {
      const len = g._line.getTotalLength();
      const p = g._line.getPointAtLength(len / 2);
      const m = sv('g', { class: 'mark', transform: `translate(${p.x},${p.y})` });
      m.append(sv('circle', { r: 8 }));
      const t = sv('text', { y: 3.5 }); t.textContent = '!'; m.append(t);
      const tt = sv('title'); tt.textContent = 'Observed but not declared in any flows file'; m.append(tt);
      m.style.cursor = 'pointer';
      m.addEventListener('click', ev => { ev.stopPropagation(); select({ type: 'edge', id: g._edge.id }); });
      layerMarks.append(m);
    }

    function renderEdgeLabel() {
      layerLabels.innerHTML = '';
      const id = state.hoverEdge || (state.selected?.type === 'edge' ? state.selected.id : null);
      const g = id && edgeEls.get(id);
      if (!g || g.style.display === 'none') return;
      const e = g._edge;
      const parts = [e.protocol || (e.signals[0] || 'tcp')];
      if (e.requestRate > 0) parts.push(`${fmtRate(e.requestRate)} req/s`);
      if (e.errorRate > 0.0005) parts.push(`${pct(e.errorRate)} errors`);
      if (e.state !== 'ok') parts.push(STATE_LABEL[e.state]);
      const p = g._line.getPointAtLength(g._line.getTotalLength() / 2);
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
      layerParticles.innerHTML = '';
      for (const g of edgeEls.values()) {
        const e = g._edge;
        if (g.style.display === 'none' || !e.observed) continue;
        const len = g._line.getTotalLength();
        const samples = [];
        for (let i = 0; i <= 64; i++) { const p = g._line.getPointAtLength((i / 64) * len); samples.push([p.x, p.y]); }
        const count = e.requestRate > 0 ? Math.max(1, Math.min(6, Math.round(1 + Math.log2(1 + e.requestRate)))) : 1;
        const speed = (e.requestRate > 0 ? 70 + 45 * Math.log10(1 + e.requestRate * 5) : 38) / len;
        for (let i = 0; i < count; i++) {
          const key = e.id + '#' + i;
          const prev = old.get(key);
          const c = sv('circle', { r: e.requestRate > 0 ? 2.6 : 2.1 });
          layerParticles.append(c);
          const p = { key, edge: e, samples, speed, t: prev ? prev.t : i / count, el: c, err: prev ? prev.err : false };
          paintParticle(p);
          particles.push(p);
        }
      }
      placeParticles();
    }
    function paintParticle(p) {
      const e = p.edge;
      p.el.setAttribute('class', (p.err ? 'err' : e.state === 'ghost' ? 'ghost' : '') + (e.requestRate > 0 ? '' : ' faint'));
    }
    function placeParticles() {
      for (const p of particles) {
        const f = p.t * 64, i = Math.min(63, Math.floor(f)), r = f - i;
        const a = p.samples[i], b = p.samples[i + 1];
        p.el.setAttribute('cx', (a[0] + (b[0] - a[0]) * r).toFixed(1));
        p.el.setAttribute('cy', (a[1] + (b[1] - a[1]) * r).toFixed(1));
      }
    }
    let last = performance.now();
    function frame(now) {
      const dt = Math.min(0.1, (now - last) / 1000);
      last = now;
      if (state.flow && particles.length) {
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
      }
      if (!destroyed) raf = requestAnimationFrame(frame);
    }

    // ------------------------------------------------------------- interaction
    function applyView() {
      const { x, y, k } = state.view;
      viewport.setAttribute('transform', `translate(${x},${y}) scale(${k})`);
      stage.style.setProperty('--grid-x', x + 'px');
      stage.style.setProperty('--grid-y', y + 'px');
      stage.style.setProperty('--grid-size', 24 * k + 'px');
    }

    function fit() {
      if (!currentView || !currentView.nodes.size) return;
      let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
      for (const g of nodeEls.values()) {
        const p = g._pos;
        x0 = Math.min(x0, p.x - NODE_W / 2); x1 = Math.max(x1, p.x + NODE_W / 2);
        y0 = Math.min(y0, p.y - NODE_H / 2); y1 = Math.max(y1, p.y + NODE_H / 2 + 12);
      }
      const r = stage.getBoundingClientRect();
      const padTop = 70, pad = 60;
      const k = Math.min(1.25, (r.width - pad * 2) / (x1 - x0), (r.height - padTop - pad) / (y1 - y0));
      state.view = { k, x: (r.width - (x1 - x0) * k) / 2 - x0 * k, y: padTop + (r.height - padTop - pad - (y1 - y0) * k) / 2 - y0 * k };
      applyView();
    }

    function zoomAt(factor, cx, cy) {
      const v = state.view;
      const k = Math.max(0.2, Math.min(2.6, v.k * factor));
      const f = k / v.k;
      state.view = { k, x: cx - (cx - v.x) * f, y: cy - (cy - v.y) * f };
      applyView();
    }

    svg.addEventListener('wheel', ev => {
      ev.preventDefault();
      const r = svg.getBoundingClientRect();
      zoomAt(Math.exp(-ev.deltaY * 0.0015), ev.clientX - r.left, ev.clientY - r.top);
    }, { passive: false });

    let drag = null;
    svg.addEventListener('pointerdown', ev => {
      if (ev.target.closest('.node') || ev.target.closest('.edge') || ev.target.closest('.mark')) return;
      drag = { type: 'pan', sx: ev.clientX, sy: ev.clientY, vx: state.view.x, vy: state.view.y, moved: false };
      svg.classList.add('panning');
      svg.setPointerCapture(ev.pointerId);
    });
    function startNodeDrag(ev, g) {
      ev.stopPropagation();
      const p = g._pos;
      drag = { type: 'node', g, id: g.dataset.id, sx: ev.clientX, sy: ev.clientY, px: p.x, py: p.y, moved: false };
      svg.setPointerCapture(ev.pointerId);
    }
    svg.addEventListener('pointermove', ev => {
      if (!drag) return;
      const dx = ev.clientX - drag.sx, dy = ev.clientY - drag.sy;
      if (Math.abs(dx) + Math.abs(dy) > 3) drag.moved = true;
      if (drag.type === 'pan') {
        state.view.x = drag.vx + dx; state.view.y = drag.vy + dy; applyView();
      } else if (drag.moved) {
        const p = { x: drag.px + dx / state.view.k, y: drag.py + dy / state.view.k };
        state.positions[drag.id] = p;
        drag.g._pos = p;
        drag.g.setAttribute('transform', `translate(${p.x},${p.y})`);
        rerouteFor(drag.id);
      }
    });
    svg.addEventListener('pointerup', () => {
      if (!drag) return;
      svg.classList.remove('panning');
      if (drag.type === 'node') {
        if (drag.moved) { store.set('positions', state.positions); rebuildParticles(); renderMarks(); }
        else select({ type: 'node', id: drag.id });
      } else if (!drag.moved) select(null);
      drag = null;
    });

    function rerouteFor(id) {
      const posOf = n => nodeEls.get(n)?._pos || { x: 0, y: 0 };
      for (const g of edgeEls.values()) {
        const e = g._edge;
        if (e.from === id || e.to === id) routeEdge(g, posOf(e.from), posOf(e.to), currentView.edges.has(e.to + '->' + e.from) ? 7 : 0);
      }
      layerMarks.innerHTML = '';
      layerParticles.innerHTML = '';
      particles = [];
      renderEdgeLabel();
    }
    function renderMarks() {
      layerMarks.innerHTML = '';
      for (const g of edgeEls.values()) if (g.style.display !== 'none' && g._edge.state === 'ghost') addGhostMark(g);
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
      const focus = state.hover || (state.selected?.type === 'node' ? state.selected.id : null);
      const match = matcher(state.query);
      let nb = null;
      if (focus) nb = neighborhood(focus);
      for (const [id, g] of nodeEls) {
        const n = currentView.nodes.get(id);
        const dim = (nb && !nb.ids.has(id)) || (match && !match(n));
        g.classList.toggle('dim', !!dim);
        g.classList.toggle('match', !!(match && match(n)));
      }
      for (const [id, g] of edgeEls) g.classList.toggle('dim', !!(nb && !nb.edges.has(id)));
      layerParticles.classList.toggle('dim', !!nb);
    }

    function select(sel) {
      state.selected = sel;
      for (const [id, g] of nodeEls) g.classList.toggle('sel', sel?.type === 'node' && sel.id === id);
      for (const [id, g] of edgeEls) g.classList.toggle('sel', sel?.type === 'edge' && sel.id === id);
      applyHighlight();
      renderEdgeLabel();
      renderDetails();
      if (sel) { showTab('details'); if (opts.mode === 'panel') $('aside').classList.add('open'); }
    }

    function focusNode(id) {
      const g = nodeEls.get(id);
      if (!g) return;
      const r = stage.getBoundingClientRect();
      const k = Math.max(state.view.k, 0.9);
      state.view = { k, x: r.width / 2 - g._pos.x * k, y: r.height / 2 - g._pos.y * k };
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
    }

    // --------------------------------------------------------------- top bar
    function renderRings() {
      const c = state.snap.coverage;
      const ring = (label, v, hint) => {
        const r = 15, circ = 2 * Math.PI * r;
        const color = v >= 0.8 ? css('--healthy') : v >= 0.5 ? css('--degraded') : css('--failing');
        const s = sv('svg', { viewBox: '0 0 38 38' });
        s.append(sv('circle', { class: 'track', cx: 19, cy: 19, r, fill: 'none', 'stroke-width': 4 }));
        s.append(sv('circle', { class: 'value', cx: 19, cy: 19, r, fill: 'none', stroke: color, 'stroke-width': 4, 'stroke-linecap': 'round', 'stroke-dasharray': `${circ * v} ${circ}` }));
        return el('div', { class: 'ring', title: hint }, s, el('div', {}, el('b', { class: 'num' }, pct(v)), el('span', {}, label)));
      };
      $('#rings').replaceChildren(
        ring('visibility', c.visibility, 'Share of internal nodes teia sees first-hand'),
        ring('declared', c.declared, 'Share of observed communication declared in flows files'),
        ring('observed', c.observed, 'Share of declared communication actually observed'));
    }

    function renderLive() {
      const l = $('#live');
      l.className = 'live ' + (!state.live ? 'paused' : state.connected ? 'on' : '');
      const ago = state.snap ? Math.max(0, Math.round((Date.now() - new Date(state.snap.generatedAt)) / 1000)) : 0;
      l.querySelector('span').textContent = !state.live ? 'paused' : state.connected
        ? `live · round ${state.snap?.round ?? '–'} · ${ago}s ago` : 'reconnecting…';
    }
    timers.push(setInterval(renderLive, 1000));

    function renderFilters() {
      const edges = state.snap.edges;
      const count = st => edges.filter(e => e.state === st).length;
      const chips = [];
      const stateChip = (st, label, style) => el('button', {
        class: 'chip' + (state.states[st] ? '' : ' off'), title: STATE_LABEL[st],
        onclick: () => { state.states[st] = !state.states[st]; store.set('states', state.states); renderAll(); },
      }, el('i', { class: 'ln', style }), label, el('b', { class: 'num' }, count(st)));
      chips.push(stateChip('ok', 'declared', `border-color:${css('--healthy')}`));
      chips.push(stateChip('ghost', 'undeclared', `border-color:${css('--ghost')};border-top-style:dashed`));
      chips.push(stateChip('stale', 'not seen', `border-color:${css('--stale')};border-top-style:dotted`));
      chips.push(el('span', { class: 'sep' }));
      const sigCount = new Map();
      for (const e of edges) for (const s of e.signals || []) sigCount.set(s, (sigCount.get(s) || 0) + 1);
      for (const [s, n] of [...sigCount.entries()].sort()) {
        if (!SIGNAL[s]) continue;
        const on = !state.signals.size || state.signals.has(s);
        chips.push(el('button', {
          class: 'chip' + (on ? '' : ' off'), title: `Only communication seen by ${SIGNAL[s].label.toLowerCase()} (click again to clear)`,
          onclick: () => {
            if (state.signals.has(s) && state.signals.size === 1) state.signals.clear();
            else { state.signals.clear(); state.signals.add(s); }
            store.set('signals', [...state.signals]); renderAll();
          },
        }, iconSVG(SIGNAL[s].icon), SIGNAL[s].label, el('b', { class: 'num' }, n)));
      }
      $('#filters').replaceChildren(...chips);
    }

    function renderLegend() {
      const sw = (color, label) => el('div', { class: 'lg' }, el('i', { class: 'sw', style: `background:${color}` }), label);
      const ln = (style, label) => el('div', { class: 'lg' }, el('i', { class: 'ln', style }), label);
      const bx = (style, label) => el('div', { class: 'lg' }, el('i', { class: 'bx', style }), label);
      const showIcons = state.legendIcons;
      const toggle = el('button', { class: 'legend-toggle', onclick: () => { state.legendIcons = !state.legendIcons; renderLegend(); } },
        showIcons ? 'hide icon library' : 'icon library ▸');
      const parts = [
        el('h4', {}, 'health'), el('h4', {}, 'communication'), el('h4', {}, 'visibility'),
        sw(css('--healthy'), 'healthy'), ln('', 'declared + seen'), bx('', 'first-hand'),
        sw(css('--degraded'), '≥ 1% errors'), ln('border-top-style:dashed', 'not declared'), bx('border-style:dashed', 'via peers'),
        sw(css('--failing'), '≥ 5% errors'), ln('border-top-style:dotted', 'not seen'), bx('border-style:dotted', 'no traffic'),
      ];
      if (showIcons) {
        parts.push(el('h4', { class: 'span3' }, 'components'));
        for (const [id, c] of Object.entries(CATEGORY)) {
          const examples = Object.values(TECH).filter(t => t.cat === id && !['Microservice', 'External', 'Frontend', 'Job', 'Function'].includes(t.label)).map(t => t.label).slice(0, 3).join(', ');
          parts.push(el('div', { class: 'lg icon', title: examples }, iconSVG(c.icon), c.label));
        }
      }
      parts.push(toggle);
      $('#legend').replaceChildren(...parts);
    }

    let groupKeys = null;
    function renderGroupBy() {
      const sel = $('#groupBy');
      const keys = labelKeys(state.snap);
      if (groupKeys === keys.join('|')) return; // keep an open dropdown open on live updates
      groupKeys = keys.join('|');
      const opts = [el('option', { value: '' }, 'No grouping')];
      for (const k of keys) opts.push(el('option', { value: k }, 'Group by ' + k));
      sel.replaceChildren(...opts);
      sel.value = keys.includes(state.groupBy) ? state.groupBy : '';
    }

    function renderButtons() {
      const flow = $('#flowBtn');
      flow.replaceChildren(iconSVG(state.flow ? '<rect x="6" y="5" width="4" height="14" rx="1"/><rect x="14" y="5" width="4" height="14" rx="1"/>' : '<path d="M7 4.5v15l12-7.5z"/>'), state.flow ? 'Flow' : 'Flow');
      flow.classList.toggle('on', state.flow);
      svg.classList.toggle('flowing', state.flow);
      const live = $('#liveBtn');
      live.replaceChildren(iconSVG('<circle cx="12" cy="12" r="3"/><path d="M5.6 5.6a9 9 0 000 12.8M18.4 5.6a9 9 0 010 12.8M8.5 8.5a5 5 0 000 7M15.5 8.5a5 5 0 010 7"/>'), 'Live');
      live.classList.toggle('on', state.live);
    }

    // ------------------------------------------------------------- side panel
    function showTab(tab) {
      state.tab = tab;
      root.querySelectorAll('.tab').forEach(t => t.classList.toggle('on', t.dataset.tab === tab));
      root.querySelectorAll('.pane').forEach(p => p.classList.toggle('on', p.id === 'pane-' + tab));
      if (tab === 'activity') $('#activityCount').textContent = '0';
    }

    function snippet(fix) {
      if (!fix?.snippet) return null;
      const pre = el('pre', {}, fix.snippet);
      const btn = el('button', { class: 'btn copy', onclick: ev => { ev.stopPropagation(); navigator.clipboard?.writeText(fix.snippet); btn.textContent = 'copied'; setTimeout(() => (btn.textContent = 'copy'), 1200); } }, 'copy');
      return el('div', { class: 'snippet' }, btn, pre);
    }

    function findingCard(f, open = false) {
      const card = el('div', { class: 'card-f ' + f.severity, onclick: () => locate(f.subject) },
        el('div', { class: 'meta' }, `${f.severity} · ${f.kind} · ${f.subject}`),
        el('h4', {}, f.title));
      if (f.detail) card.append(el('p', {}, f.detail));
      if (f.fix) {
        card.append(el('p', { class: 'next' }, '→ ' + f.fix.summary));
        const s = snippet(f.fix);
        if (s) card.append(open ? s : el('details', {}, el('summary', {}, 'show ' + (f.fix.language || 'snippet')), s));
      }
      return card;
    }

    function locate(subject) {
      if (!subject) return;
      if (subject.includes('->')) {
        const [a, b] = subject.split('->');
        const g = edgeEls.get(a + '->' + b);
        if (g) { select({ type: 'edge', id: a + '->' + b }); return; }
        subject = a;
      }
      if (nodeEls.has(subject)) focusNode(subject);
    }

    function renderSteps() {
      const fs = state.snap.findings || [];
      $('#stepsCount').textContent = fs.length;
      $('#stepsBadge').textContent = fs.length;
      const pane = $('#pane-steps');
      if (!fs.length) { pane.replaceChildren(el('p', { class: 'muted' }, 'Nothing to do: the picture is complete for the configured sources.')); return; }
      pane.replaceChildren(el('h3', {}, 'What teia needs to see more'), ...fs.map(f => findingCard(f)));
    }

    function rel(label, sub, health, onclick) {
      return el('div', { class: 'rel', onclick }, el('i', { class: 'dot', style: `background:${css('--' + (health || 'unknown'))}` }), label, el('span', {}, sub));
    }

    function renderDetails() {
      const pane = $('#pane-details');
      const sel = state.selected;
      if (!sel || !currentView) { pane.replaceChildren(el('p', { class: 'muted' }, 'Click a service, a datastore or a communication on the map.')); return; }
      if (sel.type === 'node') {
        const n = currentView.nodes.get(sel.id);
        if (!n) { pane.replaceChildren(el('p', { class: 'muted' }, 'That node is no longer in the graph.')); return; }
        const tile = el('div', { class: 'tile', style: `background:${css('--' + (n.health || 'unknown'))}` }, iconSVG(iconOf(n)));
        const parts = [el('div', { class: 'hero' }, tile, el('div', {}, el('h2', {}, n.title),
          el('span', { class: 'pill' }, el('i', { class: 'dot', style: `background:${css('--' + (n.health || 'unknown'))}` }), HEALTH_LABEL[n.health || 'unknown']), ' ',
          el('span', { class: 'pill' }, n.kind), ' ', el('span', { class: 'pill' }, n.visibility + ' visibility')))];
        if (n.group) {
          parts.push(el('p', { class: 'explain' }, `${n.members.length} workloads with ${n.labelKey}=${n.title}. Double-click the card to expand it.`));
          parts.push(el('button', { class: 'btn', onclick: () => toggleGroup(n.id) }, 'Expand group'));
          parts.push(el('h3', {}, 'Members'));
          parts.push(...n.members.map(m => rel(m.id, m.kind, m.health, () => { state.expanded.add(n.title); renderMap(); focusNode(m.id); })));
        } else {
          parts.push(el('p', { class: 'explain' }, VIS_EXPLAIN[n.visibility]));
          const kv = el('dl', { class: 'kv' });
          const row = (k, v) => kv.append(el('dt', {}, k), el('dd', {}, v));
          if (n.requestRate > 0) row('traffic in', `${fmtRate(n.requestRate)} req/s · ${pct(n.errorRate || 0)} errors`);
          row('component', `${CATEGORY[categoryOf(n)]?.label || n.kind}${n.tech ? ' · ' + techLabel(n) : ''}`);
          row('known from', (n.sources || []).join(', ') || '—');
          if (n.addresses?.length) row('addresses', n.addresses.join(', '));
          parts.push(kv);
          if (n.signals.length) {
            parts.push(el('h3', {}, 'Seen by'));
            parts.push(el('div', { class: 'labels' }, ...n.signals.map(s => el('span', { class: 'label' }, SIGNAL[s].label))));
          }
          const labels = Object.entries(n.labels || {}).filter(([k]) => userLabel(k));
          if (labels.length) {
            parts.push(el('h3', {}, 'Labels'));
            parts.push(el('div', { class: 'labels' }, ...labels.map(([k, v]) => el('span', {
              class: 'label', title: 'Search for this label', onclick: () => { $('#search').value = `${k}:${v}`; state.query = `${k}:${v}`; renderMap(); },
            }, el('b', {}, k + '='), v))));
          }
        }
        const out = [...currentView.edges.values()].filter(e => e.from === n.id);
        const inn = [...currentView.edges.values()].filter(e => e.to === n.id);
        const relEdge = (e, other) => rel(other, `${e.requestRate > 0 ? fmtRate(e.requestRate) + '/s · ' : ''}${STATE_LABEL[e.state]}`, e.state === 'stale' ? 'unknown' : e.health, () => select({ type: 'edge', id: e.id }));
        if (out.length) { parts.push(el('h3', {}, `Calls (${out.length})`)); parts.push(...out.map(e => relEdge(e, e.to))); }
        if (inn.length) { parts.push(el('h3', {}, `Called by (${inn.length})`)); parts.push(...inn.map(e => relEdge(e, e.from))); }
        const fs = (state.snap.findings || []).filter(f => n.group ? n.members.some(m => m.id === f.subject) : f.subject === n.id || f.subject.startsWith(n.id + '->'));
        if (fs.length) { parts.push(el('h3', {}, 'Next steps')); parts.push(...fs.map(f => findingCard(f, true))); }
        pane.replaceChildren(...parts);
        return;
      }
      const e = currentView.edges.get(sel.id);
      if (!e) { pane.replaceChildren(el('p', { class: 'muted' }, 'That communication is no longer in the graph.')); return; }
      const color = e.state === 'stale' ? 'unknown' : e.health;
      const parts = [
        el('div', { class: 'hero' },
          el('div', { class: 'tile', style: `background:${css('--' + color)}` }, iconSVG('<path d="M4 12h14M13 6l6 6-6 6"/>')),
          el('div', {}, el('h2', {}, `${e.from} → ${e.to}`),
            el('span', { class: 'pill' }, el('i', { class: 'dot', style: `background:${css('--' + color)}` }), HEALTH_LABEL[e.health]), ' ',
            el('span', { class: 'pill' }, STATE_LABEL[e.state]))),
      ];
      const explain = {
        ok: 'Declared in a flows file and observed in traffic: intent and reality agree.',
        ghost: 'Observed in traffic but no flows file declares it. Either the declaration is out of date, or this call was never meant to exist.',
        stale: 'Declared, but no traffic was observed while the caller was visible. Remove it if the dependency is gone; keep it if it is rare.',
      }[e.state];
      parts.push(el('p', { class: 'explain' }, explain));
      const kv = el('dl', { class: 'kv' });
      const row = (k, v) => kv.append(el('dt', {}, k), el('dd', {}, v));
      row('protocol', [e.protocol || '—', e.port ? ':' + e.port : ''].join(''));
      if (e.requestRate > 0) row('traffic', `${fmtRate(e.requestRate)} req/s`);
      if (e.requestRate > 0) row('errors', pct(e.errorRate));
      row('reason', e.reason || (e.declared ? '—' : 'not declared'));
      row('seen by', e.signals.map(s => SIGNAL[s]?.label || s).join(', ') || (e.observed ? '—' : 'nobody yet'));
      row('sources', e.sources.join(', '));
      if (e.members.length > 1) row('aggregates', `${e.members.length} communications`);
      parts.push(kv);
      const fs = (state.snap.findings || []).filter(f => f.subject === e.id || (e.state === 'ghost' && f.subject === e.from && f.rule === 'undeclared-flow'));
      if (fs.length) { parts.push(el('h3', {}, 'Next steps')); parts.push(...fs.map(f => findingCard(f, true))); }
      pane.replaceChildren(...parts);
    }

    function renderSources() {
      const pane = $('#pane-sources');
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
        : c.type === 'edge.added' && c.to === 'ghost' || c.to === 'degraded' ? 'warn' : '';

    function pushActivity(changes, fresh) {
      if (!changes?.length) return;
      state.activity = [...changes.slice().reverse(), ...state.activity].slice(0, 300);
      const pane = $('#pane-activity');
      const rows = state.activity.map((c, i) => {
        const tone = changeTone(c);
        const ic = iconSVG(CHANGE_ICON[c.type] || CHANGE_ICON['edge.state']);
        ic.style.color = tone === 'bad' ? css('--failing') : tone === 'good' ? css('--healthy') : tone === 'warn' ? css('--degraded') : css('--accent');
        return el('div', { class: 'ev' + (fresh && i < changes.length ? ' new' : ''), onclick: () => locate(c.subject), style: 'cursor:pointer' },
          el('time', { class: 'num' }, timeOf(c.at)), ic, el('div', {}, c.message));
      });
      pane.replaceChildren(el('h3', {}, 'What changed, round by round'), ...rows);
      if (fresh && state.tab !== 'activity') {
        const n = Number($('#activityCount').textContent) + changes.length;
        $('#activityCount').textContent = n;
      }
      if (fresh) toast(changes);
    }

    function toast(changes) {
      const important = changes.filter(c => changeTone(c) !== '' || c.type === 'edge.added').slice(0, 3);
      for (const c of important) {
        const t = el('div', { class: 'toast ' + changeTone(c) }, el('small', {}, `${c.type} · ${timeOf(c.at)}`), c.message);
        t.addEventListener('click', () => locate(c.subject));
        $('#toasts').prepend(t);
        setTimeout(() => t.remove(), 6000);
      }
    }

    // ---------------------------------------------------------------- export
    async function exportMap(kind) {
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
      const root = vars.map(v => `${v}:${css(v)}`).join(';');
      const style = clone.querySelector('style');
      style.textContent = `svg{${root};font-family:system-ui,sans-serif}` + style.textContent;
      clone.insertBefore(sv('rect', { width: '100%', height: '100%', fill: css('--bg') }), clone.firstChild.nextSibling);
      const text = new XMLSerializer().serializeToString(clone);
      const name = `teia-map-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}`;
      if (kind === 'svg') return download(new Blob([text], { type: 'image/svg+xml' }), name + '.svg');
      const img = new Image();
      img.onload = () => {
        const scale = 2, c = document.createElement('canvas');
        c.width = w * scale; c.height = h * scale;
        const ctx = c.getContext('2d'); ctx.scale(scale, scale); ctx.drawImage(img, 0, 0);
        c.toBlob(b => download(b, name + '.png'), 'image/png');
      };
      img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(text);
    }
    function download(blob, name) {
      const a = el('a', { href: URL.createObjectURL(blob), download: name });
      document.body.append(a); a.click(); a.remove();
      setTimeout(() => URL.revokeObjectURL(a.href), 2000);
    }

    // ----------------------------------------------------------------- data
    function renderAll() {
      reindex(state.snap);
      renderRings(); renderFilters(); renderGroupBy(); renderButtons();
      renderMap(); renderSteps(); renderSources(); renderDetails(); renderLive();
    }

    function apply(snap, fresh) {
      snap.nodes ||= []; snap.edges ||= []; snap.findings ||= []; snap.sources ||= [];
      if (!opts.api) state.connected = true;
      if (!state.live && state.snap) { state.pending = snap; return; }
      const isNew = !state.snap || snap.round !== state.snap.round;
      state.snap = snap;
      renderAll();
      if (fresh && isNew) pushActivity(snap.changes, true);
    }

    function connect() {
      try { es = new EventSource(opts.api + 'stream'); } catch { poll(); return; }
      es.addEventListener('snapshot', ev => { state.connected = true; apply(JSON.parse(ev.data), true); });
      es.onopen = () => { state.connected = true; renderLive(); };
      es.onerror = () => { state.connected = false; renderLive(); };
    }
    function poll() {
      timers.push(setInterval(async () => {
        try { apply(await (await fetch(opts.api + 'snapshot')).json(), true); state.connected = true; } catch { state.connected = false; }
      }, 10000));
    }

    // ------------------------------------------------------------------ wiring
    root.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => showTab(t.dataset.tab)));
    $('#search').addEventListener('input', ev => { state.query = ev.target.value; renderMap(); });
    $('#search').addEventListener('keydown', ev => {
      if (ev.key === 'Enter') {
        const m = matcher(state.query);
        const hit = m && [...currentView.nodes.values()].find(m);
        if (hit) focusNode(hit.id);
      }
    });
    $('#groupBy').addEventListener('change', ev => {
      state.groupBy = ev.target.value; state.expanded.clear(); store.set('groupBy', state.groupBy);
      state.selected = null; renderMap(); setTimeout(fit, 30);
    });
    $('#flowBtn').addEventListener('click', () => { state.flow = !state.flow; store.set('flow', state.flow); renderButtons(); });
    $('#liveBtn').addEventListener('click', () => {
      state.live = !state.live; renderButtons(); renderLive();
      if (state.live && state.pending) { const p = state.pending; state.pending = null; apply(p, true); }
    });
    $('#fitBtn').addEventListener('click', fit);
    $('#zoomIn').addEventListener('click', () => { const r = stage.getBoundingClientRect(); zoomAt(1.2, r.width / 2, r.height / 2); });
    $('#zoomOut').addEventListener('click', () => { const r = stage.getBoundingClientRect(); zoomAt(1 / 1.2, r.width / 2, r.height / 2); });
    $('#resetBtn').addEventListener('click', () => { state.positions = {}; store.set('positions', {}); topologyKey = ''; renderMap(); fit(); });
    $('#exportBtn').addEventListener('click', ev => { ev.stopPropagation(); $('#exportMenu').classList.toggle('open'); });
    root.addEventListener('click', () => $('#exportMenu').classList.remove('open'));
    root.querySelectorAll('[data-export]').forEach(b => b.addEventListener('click', () => exportMap(b.dataset.export)));
    keyTarget.addEventListener('keydown', onKey);
    function onKey(ev) {
      const t = ev.composedPath()[0];
      if (t.tagName === 'INPUT' || t.tagName === 'SELECT') { if (ev.key === 'Escape') t.blur(); return; }
      if (ev.key === 'f') fit();
      else if (ev.key === ' ') { ev.preventDefault(); $('#flowBtn').click(); }
      else if (ev.key === '/') { ev.preventDefault(); $('#search').focus(); }
      else if (ev.key === 'Escape') select(null);
    }
    const resizeObs = new ResizeObserver(() => { if (state.snap) fit(); });
    resizeObs.observe(host);
    const schemeMQ = matchMedia('(prefers-color-scheme: dark)');
    const onScheme = () => state.snap && renderAll();
    schemeMQ.addEventListener('change', onScheme);

    // Shareable views (page mode): ?group=team&q=kind:database&theme=light
    if (opts.mode === 'page') {
      const params = new URLSearchParams(location.search);
      if (params.has('group')) state.groupBy = params.get('group');
      if (params.has('q')) { state.query = params.get('q'); $('#search').value = state.query; }
      if (params.has('theme')) host.dataset.theme = params.get('theme');
      if (params.get('live') === '0') opts.stream = false;
    }
    if (opts.theme) host.dataset.theme = opts.theme;
    if (opts.groupBy != null && !store.get('groupBy', null)) state.groupBy = opts.groupBy;
    if (opts.flow === false && store.get('flow', null) == null) state.flow = false;

    // API links only make sense when teia's API is reachable from the browser.
    root.querySelectorAll('[data-api]').forEach(a => {
      if (opts.api) a.setAttribute('href', opts.api + a.dataset.api); else a.remove();
    });
    if (opts.mode === 'panel') {
      const aside = $('aside');
      $('#stepsBtn').addEventListener('click', () => { aside.classList.toggle('open'); showTab('steps'); });
      $('#drawerClose').addEventListener('click', () => aside.classList.remove('open'));
      host.tabIndex = 0;
    }

    initSVG();
    renderLegend();
    raf = requestAnimationFrame(frame);
    if (opts.api) {
      (async () => {
        try {
          const [snap, changes] = await Promise.all([
            fetch(opts.api + 'snapshot').then(r => r.json()),
            fetch(opts.api + 'changes').then(r => r.json()),
          ]);
          if (snap.generatedAt && !snap.generatedAt.startsWith('0001')) apply(snap, false);
          pushActivity(changes || [], false);
        } catch { /* the stream will bring it */ }
        if (opts.stream !== false) connect(); else { state.live = false; renderButtons(); }
      })();
    }

    return {
      // update feeds a snapshot from outside (the Grafana panel).
      update(snap, changes) {
        apply(snap, true);
        if (changes) {
          const known = new Set(state.activity.map(c => c.at + c.type + c.subject));
          const fresh = changes.filter(c => !known.has(c.at + c.type + c.subject));
          if (fresh.length) pushActivity(fresh, state.activity.length > 0);
        }
      },
      setTheme(theme) { host.dataset.theme = theme; if (state.snap) renderAll(); },
      destroy() {
        destroyed = true;
        cancelAnimationFrame(raf);
        timers.forEach(clearInterval);
        resizeObs.disconnect();
        schemeMQ.removeEventListener('change', onScheme);
        keyTarget.removeEventListener('keydown', onKey);
        if (es) es.close();
        root.innerHTML = '';
      },
    };
  }

  const TeiaUI = { mount };
  if (typeof module === 'object' && module.exports) module.exports = TeiaUI;
  else global.TeiaUI = TeiaUI;
})(typeof window !== 'undefined' ? window : globalThis);
