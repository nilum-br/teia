#!/usr/bin/env python3
"""Generates docs/assets/icon-library.svg from the icon definitions in
internal/api/ui/teia-ui.js, so the documentation never drifts from the map.

    python scripts/gen-docs-assets.py
"""
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
UI = (ROOT / "internal/api/ui/teia-ui.js").read_text(encoding="utf-8")

NAVY_900, NAVY_800, SURFACE, LINE = "#020a33", "#020f59", "#0a155f", "rgba(255,255,255,.10)"
PAPER, MUTED, LIME, HEALTHY = "#f7f8ff", "#9aa6d6", "#a9f549", "#46d18a"
FONT = "Outfit, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"


def js_string(expr: str, env: dict) -> str:
    """Evaluates the tiny subset used for icons: 'literal', NAME, NAME.key and +."""
    out = ""
    for part in re.findall(r"'[^']*'|[A-Za-z_][\w.]*", expr):
        if part.startswith("'"):
            out += part[1:-1]
        elif "." in part:
            obj, key = part.split(".", 1)
            out += env[obj][key]
        else:
            out += env[part]
    return out


env = {"ICON": {}}
icon_block = UI[UI.index("const ICON = {"):]
icon_block = icon_block[: icon_block.index("};")]
for key, val in re.findall(r"^\s*(\w+): ('[^\n]*'),\s*$", icon_block, flags=re.M):
    env["ICON"][key] = js_string(val, env)
env["CYL"] = js_string(re.search(r"const CYL = ([^;]+);", UI).group(1), env)

cat_block = UI[UI.index("const CATEGORY = {"):]
cat_block = cat_block[: cat_block.index("};")]
categories = []
for key, label, icon in re.findall(r"^\s*(\w+): \{ label: '([^']+)', icon: (.+?) \},\s*$", cat_block, flags=re.M):
    categories.append((key, label, js_string(icon, env)))

techs = {}
for cat, pairs in re.findall(r"techs\('(\w+)', '([^']+)'\);", UI):
    techs[cat] = [p.split("=")[1] for p in pairs.split(",")]

cols, cell_w, cell_h, pad = 6, 190, 150, 28
rows = (len(categories) + cols - 1) // cols
width, height = pad * 2 + cols * cell_w, pad * 2 + 70 + rows * cell_h
parts = [
    f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {height}" font-family="{FONT}">',
    "  <title>teia icon library</title>",
    f'  <rect width="{width}" height="{height}" rx="22" fill="{NAVY_900}"/>',
    f'  <text x="{pad}" y="{pad + 30}" fill="{PAPER}" font-size="24" font-weight="600">teia icon library</text>',
    f'  <text x="{pad}" y="{pad + 54}" fill="{MUTED}" font-size="14">One pictogram per kind of component; the tile colour is its health.</text>',
]
for i, (key, label, icon) in enumerate(categories):
    x = pad + (i % cols) * cell_w
    y = pad + 70 + (i // cols) * cell_h
    examples = ""
    for t in techs.get(key, []):
        if t in ("Microservice", "External", "Frontend", "Job", "Function"):
            continue
        candidate = f"{examples}, {t}" if examples else t
        if len(candidate) > 30:
            break
        examples = candidate
    parts += [
        f'  <g transform="translate({x} {y})">',
        f'    <rect x="6" y="6" width="{cell_w - 12}" height="{cell_h - 12}" rx="14" fill="{SURFACE}" stroke="{LINE}"/>',
        f'    <rect x="{cell_w / 2 - 24}" y="22" width="48" height="48" rx="13" fill="{HEALTHY}"/>',
        f'    <g transform="translate({cell_w / 2 - 15} 31) scale(1.25)" fill="none" stroke="{NAVY_800}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">{icon}</g>',
        f'    <text x="{cell_w / 2}" y="96" fill="{PAPER}" font-size="13.5" font-weight="600" text-anchor="middle">{label}</text>',
        f'    <text x="{cell_w / 2}" y="116" fill="{MUTED}" font-size="11.5" text-anchor="middle">{examples}</text>',
        "  </g>",
    ]
parts.append("</svg>\n")
(ROOT / "docs/assets/icon-library.svg").write_text("\n".join(parts), encoding="utf-8", newline="\n")
print(f"icon-library.svg: {len(categories)} categories")
