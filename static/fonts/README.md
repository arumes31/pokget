# Bundled interface icons

`material-symbols-outlined.ttf` is Google's Material Symbols Outlined, regular weight, downloaded from Google Fonts on 2026-09-07. It is served locally so icons remain available when Google Fonts is blocked or unavailable.

Source: https://fonts.gstatic.com/s/materialsymbolsoutlined/v369/kJF1BvYX7BgnkSrUwT8OhrdQw4oELdPIeeII9v6oDMzByHX9rA6RzaxHMPdY43zj-jCxv3fzvRNU22ZXGJpEpjC_1v-p_4MrImHCIJIZrDCvHOem.ttf

Apache 2.0 license from https://github.com/google/material-design-icons/blob/master/LICENSE is included in this directory.

The UI serves `material-symbols-ui.woff2`, a 174-icon subset (16,324 bytes), instead of the 964,228-byte source TTF. `ui-icons.txt` records the retained glyph names. The original font remains here as the regeneration source and is not requested or precached by the app.

To regenerate after introducing icons, install `fonttools[woff]==4.64.0` and Pillow, then run `python scripts/optimize-ui-assets.py` from the repository. This developer-only script preserves required ligature shaping, verifies every retained icon, and also encodes the existing logo as a 128px WebP and a separate 32px PNG favicon. Ordinary builds use the committed outputs and do not require Python. Subsetting reference: https://fonttools.readthedocs.io/en/latest/subset/index.html
