#!/usr/bin/env python3
"""Recover page-specific CSS classes that lived only in the original app.css
(before the redesign rewrite) and were dropped. Pull them from the pristine
client/web mirror, remap the old palette to new tokens, and emit a CSS chunk
to append into components.css.

Segments recovered:
  - line 0 (base): only the provider hero/card/summary/meta rules we dropped
  - lines 3-4: combo page layout
  - line 6: request-log card layout
  - line 7: settings page alignment
"""
import re

SRC = r"E:\AI\9router-master\9router-master\vivurouter-go\client\web\static\app.css"
OUT = r"E:\AI\9router-master\9router-master\vivurouter-go\web\static\_recovered.css"

lines = open(SRC, encoding="utf-8").read().split("\n")
base   = lines[0]
combo  = lines[3] + lines[4]
reqlog = lines[6]
settings = lines[7]

# --- Surgically pull provider-base rules out of the base line 0 ---
# We want the run that starts at `.providers-hero{` and continues through the
# provider card / summary / meta / model-chip / empty-state system, up to the
# first @media block in base (which we already re-authored in app.css).
start = base.find(".providers-hero{")
end = base.find("@media(max-width:900px)")
provider_base = base[start:end] if start != -1 and end != -1 else ""

combined = "\n".join([
    "/* ==== Recovered provider hero/card/summary system ==== */",
    provider_base,
    "/* ==== Recovered combo page layout ==== */",
    combo,
    "/* ==== Recovered request-log card layout ==== */",
    reqlog,
    "/* ==== Recovered settings alignment ==== */",
    settings,
])

# ---- palette remap (same maps as remap_palette.py) ----
RGB_MAP = {
    (110,231,249):(92,192,234),(67,232,249):(92,192,234),(8,13,25):(20,22,29),
    (18,26,47):(24,26,34),(15,23,42):(24,26,34),(12,19,36):(30,33,43),
    (30,41,59):(33,36,48),(41,54,83):(54,59,75),(24,34,59):(33,36,48),
    (52,211,153):(70,201,138),(34,197,94):(70,201,138),(16,185,129):(70,201,138),
    (239,68,68):(229,105,95),(248,113,113):(229,105,95),(127,29,29):(74,33,30),
    (251,191,36):(230,179,77),(120,80,10):(74,60,26),(59,130,246):(106,116,240),
    (37,99,235):(106,116,240),(167,139,250):(154,134,242),(192,132,252):(154,134,242),
    (124,58,237):(106,92,224),(34,211,238):(92,192,234),(148,163,184):(140,148,166),
    (255,255,255):(255,255,255),
}
HEX_MAP = {
    "#6ee7f9":"#7dd0f2","#67e8f9":"#7dd0f2","#b8f7ff":"#bfe6f7","#dffbff":"#cfeaf8",
    "#dff9ff":"#cfeaf8","#111a2f":"#181a22","#111a33":"#121319","#070b16":"#0e0f14",
    "#08101f":"#0f1117","#0f172a":"#14161d","#080d19":"#0f1117","#334155":"#212430",
    "#e2e8f0":"#e6e9f2","#c084fc":"#9a86f2","#e9d5ff":"#d9d2f7","#a78bfa":"#9a86f2",
    "#fecaca":"#f4b0aa","#fde68a":"#f2d492","#bbf7d0":"#8fe3ba","#86efac":"#8fe3ba",
    "#fb7b5b":"#e88a63","#fb923c":"#e8a05a","#fb7185":"#e5837f","#22d3ee":"#5cc0ea",
    "#dbeafe":"#d4def5","#93c5fd":"#a9c0f4","#bfdbfe":"#c4d2f5","#059669":"#3f9bd6",
    "#0891b2":"#4bb2e6","#2563eb":"#6a74f0","#b91c1c":"#b0463f","#7c3aed":"#6a5ce0",
    "#8b5cf6":"#8b93f7","#cfe0ff":"#c9cfe0","#e8eefc":"#e9ebf2","#9fb0d0":"#a2aabd",
}
def remap_rgba(t):
    def r(m):
        rr,gg,bb=int(m[1]),int(m[2]),int(m[3]); a=m[4] or ""
        if (rr,gg,bb) in RGB_MAP:
            nr,ng,nb=RGB_MAP[(rr,gg,bb)]; return f"{'rgba' if a else 'rgb'}({nr},{ng},{nb}{a})"
        return m[0]
    return re.sub(r'rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*(,\s*[\d.]+)?\)', r, t)
def remap_hex(t):
    return re.sub(r'#[0-9a-fA-F]{6}\b', lambda m: HEX_MAP.get(m[0].lower(), m[0]), t)

combined = remap_hex(remap_rgba(combined))
open(OUT, "w", encoding="utf-8").write(combined)
print(f"wrote {OUT}: {len(combined)} bytes")
print("provider_base found:", bool(provider_base), "len", len(provider_base))
