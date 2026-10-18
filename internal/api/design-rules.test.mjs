// The rules in docs/design.md that the component's stylesheets can break,
// checked on the stylesheets themselves.
//
//   node --test internal/api/design-rules.test.mjs
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { describe, it } from 'node:test';

const source = readFileSync(new URL('./ui/teia-ui.js', import.meta.url), 'utf8');

// The two stylesheets as written (neither interpolates), split into rules.
// Rules inside @media come out with their own selector, which is all these checks need.
const sheet = name => {
  const start = source.indexOf(`const ${name} = \``) + `const ${name} = \``.length;
  return source.slice(start, source.indexOf('`;', start));
};
const rulesOf = css => [...css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g)]
  .map(([, selector, body]) => ({ selector: selector.trim().replace(/\s+/g, ' '), body }));
const RULES = [...rulesOf(sheet('STYLE')), ...rulesOf(sheet('SVG_CSS'))];
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
