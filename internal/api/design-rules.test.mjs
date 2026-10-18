// The rules in docs/design.md that the component's stylesheets can break,
// checked on the stylesheets themselves.
//
//   node --test internal/api/design-rules.test.mjs
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { describe, it } from 'node:test';

const source = readFileSync(new URL('./ui/teia-ui.js', import.meta.url), 'utf8');
const { core } = createRequire(import.meta.url)('./ui/teia-ui.js');

// The component's stylesheets as written (none interpolates), split into rules:
// the page's, the investigation workspace's and the map's. Rules inside @media
// come out with their own selector, which is all these checks need.
const sheet = opening => {
  const start = source.indexOf(opening);
  assert.ok(start >= 0, `no stylesheet at ${opening}`);
  return source.slice(start + opening.length, source.indexOf('`;', start));
};
const SHEETS = ['const STYLE = `', 'style.textContent = STYLE + `', 'const SVG_CSS = `'].map(sheet);
const rulesOf = css => [...css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g)]
  .map(([, selector, body]) => ({ selector: selector.trim().replace(/\s+/g, ' '), body }));
const RULES = SHEETS.flatMap(rulesOf);
const selectors = r => r.selector.replace(/^@[^{]*$/, '').split(/,(?![^(]*\))/).map(s => s.trim());
const rule = (selector, has = '') => {
  const found = RULES.find(r => r.selector === selector && r.body.includes(has));
  assert.ok(found, `no rule ${selector}`);
  return found;
};
const tokensOf = selector => Object.fromEntries([...rule(selector, '--').body.matchAll(/(--[\w-]+):\s*([^;]+)/g)].map(([, k, v]) => [k, v.trim()]));

const dark = tokensOf(':host'), light = tokensOf(':host([data-theme="light"])');
const THEMES = {
  dark, light,
  'Grafana dark': { ...dark, ...tokensOf('.teia.panel') },
  'Grafana light': { ...light, ...tokensOf('.teia.panel'), ...tokensOf(':host([data-theme="light"]) .teia.panel') },
};

// WCAG 2.2 relative luminance and contrast ratio, for #rrggbb colours.
const rgb = hex => { assert.match(hex, /^#[0-9a-f]{6}$/i); return [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16)); };
const luminance = c => c.map(v => v / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4)
  .reduce((sum, v, i) => sum + v * [.2126, .7152, .0722][i], 0);
const contrast = (a, b) => { const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x); return (hi + .05) / (lo + .05); };
// What a rule strokes with, resolved in a theme and laid over a surface.
const stroke = (theme, selector, surface) => {
  const { body } = rule(selector, 'stroke:');
  const token = body.match(/(?:^|;)\s*stroke:\s*var\((--[\w-]+)\)/)?.[1];
  assert.ok(token, `${selector} strokes with a token`);
  const opacity = Number(body.match(/stroke-opacity:\s*([\d.]+)/)?.[1] ?? 1);
  const under = rgb(theme[surface]);
  return { token, color: rgb(theme[token]).map((v, i) => v * opacity + under[i] * (1 - opacity)), under };
};

describe('design rules (docs/design.md)', () => {
  // Health is a component's health, teia's own (its connection, a failed
  // request) or a finding's severity. A rule that paints with one of these
  // colours has to say which health it shows.
  it('keeps green, amber and red for health', () => {
    const HEALTH_COLOUR = /var\(--(?:healthy|degraded|failing|ok-text|warn-text|err-text|warning|critical)\)/;
    const HEALTH_STATE = /\.(?:h-)?(?:healthy|degraded|failing|critical|warning|warn|error|err|fail|bad|good|ok)\b|\.live\.on\b/;
    const misuse = RULES.filter(r => HEALTH_COLOUR.test(r.body)).flatMap(selectors).filter(s => !HEALTH_STATE.test(s));
    assert.deepEqual(misuse, []);
  });

  it('colours a change in the activity feed only by health', () => {
    assert.equal(core.changeTone({ type: 'edge.added', to: 'ghost' }), '', 'a new undeclared call is news, not a warning');
    assert.equal(core.changeTone({ type: 'edge.changed', to: 'stale' }), '');
    assert.equal(core.changeTone({ type: 'node.changed', to: 'degraded' }), 'warn');
    assert.equal(core.changeTone({ type: 'edge.changed', to: 'failing' }), 'bad');
    assert.equal(core.changeTone({ type: 'finding.resolved' }), 'good');
  });

  it('keeps the dash for what is only declared', () => {
    const ONLY_DECLARED = /\.vis-none\b|\.s-stale\b|\.stale\b/;
    const misuse = RULES.filter(r => /stroke-dasharray|\bdashed\b|\bdotted\b/.test(r.body)).flatMap(selectors).filter(s => !ONLY_DECLARED.test(s));
    assert.deepEqual(misuse, []);
  });

  it('draws none of the decoration the rules exclude', () => {
    const css = SHEETS.join('\n');
    for (const [what, pattern] of [['uppercase labels', /text-transform:\s*uppercase/], ['letter-spaced labels', /letter-spacing:(?!\s*(?:0|normal)\s*[;}])[^;}]*/],
      ['blur', /blur\(|backdrop-filter/], ['gradients', /-gradient\(/], ['endless motion', /\binfinite\b/], ['glow', /text-shadow|drop-shadow/]]) {
      assert.equal(css.match(pattern)?.[0], undefined, what);
    }
    assert.equal(source.match(/feGaussianBlur|feDropShadow/)?.[0], undefined, 'glow in SVG');
    // Floating surfaces (menus, dialogs, the drawer) share one shadow, and it falls below them: no halo.
    for (const r of RULES.filter(r => /box-shadow/.test(r.body))) assert.match(r.body, /box-shadow:\s*(?:var\(--shadow\)|none)/, r.selector);
    for (const [name, theme] of Object.entries(THEMES)) {
      for (const layer of theme['--shadow'].split(/,(?![^(]*\))/)) assert.match(layer.trim(), /^0 [1-9]\d*px /, `${name} shadow: ${layer}`);
    }
  });

  // WCAG 2.2, 1.4.11: a graphic needed to understand the content keeps 3:1
  // against what it sits on. On the map that is the hatch (a blind spot), the
  // dashed card (no traffic seen) and the dashed line (declared, not seen).
  it('gives the marks that carry the grammar 3:1 against the surface under them', () => {
    const marks = [['.blind-line', ['--card', '--card-hi']], ['.vis-none .card', ['--card', '--bg']], ['.edge.s-stale path.line', ['--bg']]];
    for (const [name, theme] of Object.entries(THEMES)) {
      for (const [selector, surfaces] of marks) {
        for (const surface of surfaces) {
          const { token, color, under } = stroke(theme, selector, surface);
          const ratio = contrast(color, under);
          assert.ok(ratio >= 3, `${name}: ${selector} (${token}) on ${surface} is ${ratio.toFixed(2)}:1`);
        }
      }
    }
  });

  it('declares the light theme the same way for the system setting and the theme switch', () => {
    assert.deepEqual(tokensOf(':host(:not([data-theme="dark"]))'), light);
  });
});
