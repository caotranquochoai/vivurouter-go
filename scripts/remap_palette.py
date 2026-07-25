#!/usr/bin/env python3
"""Mechanical, layout-preserving color remap of the two page-specific
stylesheets from the old neon-cyan/cool-dark palette to the new refined
warm-dark + tamed-cyan/indigo palette. Only colors change; every selector,
dimension, and JS-facing class stays byte-identical otherwise."""
import re, sys

FILES = [
    "web/static/provider-actions.css",
    "web/static/usage-dashboard.css",
]

# ---- alpha-preserving rgba remaps: old rgb triplet -> new rgb triplet ----
RGB_MAP = {
    (110, 231, 249): (92, 192, 234),   # neon cyan accent -> soft cyan
    (67, 232, 249):  (92, 192, 234),   # #43e8f9 variant
    (8, 13, 25):     (20, 22, 29),     # cool dark inset -> warm surface-2
    (18, 26, 47):    (24, 26, 34),     # panel -> surface
    (15, 23, 42):    (24, 26, 34),     # slate-900 -> surface
    (12, 19, 36):    (30, 33, 43),     # hover surface -> surface-3
    (30, 41, 59):    (33, 36, 48),     # slate-800 -> surface-alt
    (41, 54, 83):    (54, 59, 75),     # nested border -> border-strong
    (24, 34, 59):    (33, 36, 48),     # th bg -> surface-alt
    (52, 211, 153):  (70, 201, 138),   # ok green (softer)
    (34, 197, 94):   (70, 201, 138),   # ok green alt
    (16, 185, 129):  (70, 201, 138),
    (239, 68, 68):   (229, 105, 95),   # danger (softer)
    (248, 113, 113): (229, 105, 95),   # danger light
    (127, 29, 29):   (74, 33, 30),     # dark red bg
    (251, 191, 36):  (230, 179, 77),   # warn (softer)
    (120, 80, 10):   (74, 60, 26),     # dark amber bg
    (59, 130, 246):  (106, 116, 240),  # blue -> indigo
    (37, 99, 235):   (106, 116, 240),  # blue-600 -> indigo
    (167, 139, 250): (154, 134, 242),  # violet
    (192, 132, 252): (154, 134, 242),  # purple -> violet
    (124, 58, 237):  (106, 92, 224),   # deep violet
    (34, 211, 238):  (92, 192, 234),   # cyan-400 -> soft cyan
    (148, 163, 184): (140, 148, 166),  # slate-400 neutral (slightly warmer)
    (255, 255, 255): (255, 255, 255),  # keep white tints
}

# ---- solid hex remaps ----
HEX_MAP = {
    "#6ee7f9": "#7dd0f2", "#67e8f9": "#7dd0f2",
    "#b8f7ff": "#bfe6f7", "#dffbff": "#cfeaf8", "#dff9ff": "#cfeaf8",
    "#111a2f": "#181a22", "#111a33": "#121319", "#070b16": "#0e0f14",
    "#08101f": "#0f1117", "#0f172a": "#14161d", "#080d19": "#0f1117",
    "#334155": "#212430", "#e2e8f0": "#e6e9f2",
    "#c084fc": "#9a86f2", "#e9d5ff": "#d9d2f7", "#a78bfa": "#9a86f2",
    "#fecaca": "#f4b0aa", "#fde68a": "#f2d492", "#bbf7d0": "#8fe3ba",
    "#86efac": "#8fe3ba", "#fb7b5b": "#e88a63", "#fb923c": "#e8a05a",
    "#fb7185": "#e5837f", "#22d3ee": "#5cc0ea", "#dbeafe": "#d4def5",
    "#93c5fd": "#a9c0f4", "#bfdbfe": "#c4d2f5", "#dffbff": "#cfeaf8",
    "#059669": "#3f9bd6", "#0891b2": "#4bb2e6", "#2563eb": "#6a74f0",
    "#b91c1c": "#b0463f", "#7c3aed": "#6a5ce0", "#8b5cf6": "#8b93f7",
    "#cfe0ff": "#c9cfe0",
}

def remap_rgba(text):
    pat = re.compile(r'rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*(,\s*[\d.]+)?\)')
    def repl(m):
        r, g, b = int(m.group(1)), int(m.group(2)), int(m.group(3))
        alpha = m.group(4) or ""
        if (r, g, b) in RGB_MAP:
            nr, ng, nb = RGB_MAP[(r, g, b)]
            fn = "rgba" if alpha else "rgb"
            return f"{fn}({nr},{ng},{nb}{alpha})"
        return m.group(0)
    return pat.sub(repl, text)

def remap_hex(text):
    def repl(m):
        h = m.group(0).lower()
        return HEX_MAP.get(h, m.group(0))
    return re.sub(r'#[0-9a-fA-F]{6}\b', repl, text)

for path in FILES:
    with open(path, "r", encoding="utf-8") as f:
        src = f.read()
    out = remap_hex(remap_rgba(src))
    # Neutralize the body background override in usage-dashboard.css so the
    # new warm app.css background is not clobbered.
    out = out.replace(
        "body{background:linear-gradient(120deg,#0e0f14,#121319)}",
        "/* body background handled by app.css */")
    with open(path, "w", encoding="utf-8") as f:
        f.write(out)
    print(f"remapped {path}: {len(src)} -> {len(out)} bytes")
