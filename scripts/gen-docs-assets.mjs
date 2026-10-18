// Generates the documentation images from the map component itself, so the
// docs never drift from what the map draws:
//
//   node scripts/gen-docs-assets.mjs
//
// docs/assets/icon-library.svg  one pictogram per kind of component
// docs/assets/map-grammar.svg   how the map draws what it sees and what it does not
import { createRequire } from 'node:module';
import { writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const { core } = createRequire(import.meta.url)(join(root, 'internal/api/ui/teia-ui.js'));
const { CATEGORY, TECH, REASON } = core;

const BG = '#0d1120', CARD = '#151a26', LINE = '#2a3142', INK = '#e7eaf2', INK2 = '#b4bccc', MUTED = '#8790a3';
const HEALTHY = '#3fb27f', DEGRADED = '#e5a63a', FAILING = '#e5534b';
const FONT = "'IBM Plex Sans', 'Segoe UI', Roboto, Helvetica, Arial, sans-serif";
const ICON = 'fill="none" stroke-width="1.5" stroke-linecap="square" stroke-linejoin="miter"';
const esc = s => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;');
const svg = (w, h, title, body) => [
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}" font-family="${FONT}">`,
  `  <title>${title}</title>`,
  `  <rect width="${w}" height="${h}" rx="12" fill="${BG}"/>`,
  ...body, '</svg>', ''].join('\n');

// ---- icon library
{
  const cats = Object.entries(CATEGORY);
  const cols = 6, cw = 186, ch = 132, pad = 28;
  const w = pad * 2 + cols * cw, h = pad * 2 + 64 + Math.ceil(cats.length / cols) * ch;
  const body = [
    `  <text x="${pad}" y="${pad + 24}" fill="${INK}" font-size="22" font-weight="600">teia icon library</text>`,
    `  <text x="${pad}" y="${pad + 48}" fill="${MUTED}" font-size="13.5">One pictogram per kind of component. On the map, its colour is the component's health.</text>`,
  ];
  cats.forEach(([id, c], i) => {
    const x = pad + (i % cols) * cw, y = pad + 64 + Math.floor(i / cols) * ch;
    let examples = '';
    for (const t of Object.values(TECH).filter(t => t.cat === id && !['Microservice', 'External', 'Frontend', 'Job', 'Function'].includes(t.label))) {
      const next = examples ? `${examples}, ${t.label}` : t.label;
      if (next.length > 28) break;
      examples = next;
    }
    body.push(
      `  <g transform="translate(${x} ${y})">`,
      `    <rect x="5" y="5" width="${cw - 10}" height="${ch - 10}" rx="6" fill="${CARD}" stroke="${LINE}"/>`,
      `    <g transform="translate(${cw / 2 - 18} 20) scale(1.8)" color="${INK}" stroke="currentColor" ${ICON}>${c.icon}</g>`,
      `    <text x="${cw / 2}" y="86" fill="${INK}" font-size="13" font-weight="600" text-anchor="middle">${esc(c.label)}</text>`,
      `    <text x="${cw / 2}" y="105" fill="${MUTED}" font-size="11.5" text-anchor="middle">${esc(examples)}</text>`,
      '  </g>');
  });
  writeFileSync(join(root, 'docs/assets/icon-library.svg'), svg(w, h, 'teia icon library', body));
}

// ---- map grammar: three columns, the same marks the map and its legend draw
{
  const w = 1040, h = 330, col = [32, 372, 712], top = 92, gap = 64;
  const hatch = '<pattern id="blind" width="5" height="5" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><path d="M0 0V5" stroke="' + INK + '" stroke-opacity=".16" stroke-width="1.3"/></pattern>';
  const body = [
    `  <defs>${hatch}</defs>`,
    `  <text x="32" y="44" fill="${INK}" font-size="22" font-weight="600">How the map draws what teia sees, and what it does not</text>`,
    `  <text x="32" y="68" fill="${MUTED}" font-size="13.5">A solid stroke is seen. A dashed one is only declared. A hatched card is a blind spot. Colour is health, and never alone.</text>`,
  ];
  const heading = (x, t) => body.push(`  <text x="${x}" y="${top + 6}" fill="${INK2}" font-size="13" font-weight="600">${t}</text>`);
  const label = (x, y, t, sub) => body.push(`  <text x="${x}" y="${y + 5}" fill="${INK}" font-size="13.5">${t}</text>`, `  <text x="${x}" y="${y + 23}" fill="${MUTED}" font-size="11.5">${sub}</text>`);
  heading(col[0], 'Communication');
  const line = (y, extra = '') => `  <path d="M${col[0]} ${y}H${col[0] + 96}" stroke="${INK2}" stroke-width="2"${extra}/>`;
  body.push(line(top + 40));
  label(col[0] + 116, top + 40, 'declared and seen', 'a flows file and the traffic agree');
  body.push(line(top + 40 + gap), `  <rect x="${col[0] + 39}" y="${top + 40 + gap - 9}" width="18" height="18" rx="3" fill="${BG}" stroke="${INK2}" stroke-width="1.5"/>`,
    `  <path d="M${col[0] + 43} ${top + 40 + gap}H${col[0] + 53}M${col[0] + 48} ${top + 35 + gap}V${top + 45 + gap}" stroke="${INK}" stroke-width="1.8"/>`);
  label(col[0] + 116, top + 40 + gap, 'seen, not declared', 'traffic no flows file mentions');
  body.push(line(top + 40 + gap * 2, ' stroke-dasharray="7 5"'));
  label(col[0] + 116, top + 40 + gap * 2, 'declared, not seen', 'no traffic while the caller was visible');

  heading(col[1], 'Visibility');
  const card = (y, extra = '', fill = CARD) => `  <rect x="${col[1]}" y="${y - 16}" width="96" height="32" rx="5" fill="${fill}" stroke="${INK2}" stroke-width="1.5"${extra}/>`;
  body.push(card(top + 40));
  label(col[1] + 116, top + 40, 'seen first-hand', 'its own SDK, sidecar or eBPF probe');
  body.push(card(top + 40 + gap), `  <rect x="${col[1]}" y="${top + 24 + gap}" width="96" height="32" rx="5" fill="url(#blind)"/>`);
  label(col[1] + 116, top + 40 + gap, 'blind spot', 'only its callers report it');
  body.push(card(top + 40 + gap * 2, ' stroke-dasharray="6 4"'));
  label(col[1] + 116, top + 40 + gap * 2, 'no traffic seen', 'known from an inventory or flows file');

  heading(col[2], 'Health');
  const glyph = (y, d, c) => `  <path transform="translate(${col[2] + 12} ${y}) scale(1.3)" d="${d}" fill="${c}"/>`;
  body.push(glyph(top + 40, 'M-5,-5H5V5H-5Z', HEALTHY));
  label(col[2] + 36, top + 40, 'healthy', 'the icon takes the colour; the card stays quiet');
  body.push(glyph(top + 40 + gap, 'M0,-6 L6,5 L-6,5 Z', DEGRADED));
  label(col[2] + 36, top + 40 + gap, 'degraded', `amber outline and a tag with the reason (${Object.keys(REASON).length} kinds)`);
  body.push(glyph(top + 40 + gap * 2, 'M-5,-3 L-3,-5 L0,-2 L3,-5 L5,-3 L2,0 L5,3 L3,5 L0,2 L-3,5 L-5,3 L-2,0 Z', FAILING));
  label(col[2] + 36, top + 40 + gap * 2, 'failing', 'red outline and a tag with the reason');
  writeFileSync(join(root, 'docs/assets/map-grammar.svg'), svg(w, h, 'teia map grammar', body));
}
console.log('docs/assets: icon-library.svg, map-grammar.svg');
