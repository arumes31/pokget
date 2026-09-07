"""Regenerate committed UI assets; not required during ordinary application builds.

Requires fonttools[woff]==4.64.0 and Pillow. Run from any working directory.
"""
from pathlib import Path
import re

from fontTools import subset
from fontTools.ttLib import TTFont
from PIL import Image

ROOT = Path(__file__).resolve().parent.parent
FONT_DIR = ROOT / "static/fonts"
font = TTFont(FONT_DIR / "material-symbols-outlined.ttf")
sources = list((ROOT / "templates").glob("*.html"))
sources += [p for p in (ROOT / "static/js").glob("*.js") if not p.name.endswith(".min.js")]
sources += [ROOT / "static/offline.html"]
tokens = set()
for path in sources:
    tokens.update(re.findall(r"[a-z][a-z0-9_]*", path.read_text(encoding="utf-8")))
def ligature_outputs(font):
    outputs = set()
    for lookup in font["GSUB"].table.LookupList.Lookup:
        for table in lookup.SubTable:
            table = getattr(table, "ExtSubTable", table)
            for entries in getattr(table, "ligatures", {}).values():
                outputs.update(entry.LigGlyph for entry in entries)
    return outputs


icons = sorted(tokens.intersection(ligature_outputs(font)))
characters = set("".join(icons))
options = subset.Options()
options.flavor = "woff2"
# Keep only selected output glyphs. Alphabet closure would include nearly every icon.
options.layout_closure = False
options.layout_features = ["*"]
options.glyph_names = True
subsetter = subset.Subsetter(options=options)
subsetter.populate(glyphs=icons, unicodes=[ord(c) for c in characters])
subsetter.subset(font)
font.flavor = "woff2"
destination = FONT_DIR / "material-symbols-ui.woff2"
font.save(destination)

# Assert that every icon still has its named ligature after pruning GSUB.
result = TTFont(destination)
missing = set(icons) - ligature_outputs(result)
if missing:
    raise RuntimeError(f"Missing icon ligatures: {sorted(missing)}")
(FONT_DIR / "ui-icons.txt").write_text("\n".join(icons) + "\n", encoding="utf-8")

with Image.open(ROOT / "static/img/logo.png") as logo:
    logo.resize((128, 128), Image.Resampling.LANCZOS).save(
        ROOT / "static/img/logo-128.webp", format="WEBP", quality=90, method=6
    )
    logo.resize((32, 32), Image.Resampling.LANCZOS).save(
        ROOT / "static/img/favicon-32.png", optimize=True
    )
print(f"Retained {len(icons)} icons; font {destination.stat().st_size:,} bytes")
