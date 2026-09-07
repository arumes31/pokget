# Pokget Measure

Open **Scan**, then switch to **Measure**. The existing scanner and the new measurement workspace share the route; switching to Measure releases the scanner camera. A measurement stays available while switching between these modes, until the page or fragment is left.

1. Choose a JPG, PNG or WebP photo, drop a file, use the mobile camera picker, or import a public HTTPS image URL. Files are limited to 15 MB and images to 100–12,000 pixels per side. Processing downsizes images to at most 2,000 pixels on the longer side. URL imports require the image host to allow CORS.
2. Use image tools to rotate, crop to the outer guides, or flatten a tilted photo with four ordered corners. Perspective correction resets the inner guides for review. Restore original returns to the imported image.
3. Position four **Card edges** and four **Design borders**. Select the active group to avoid overlapping handles. **Suggest borders** uses local contrast peaks; inspect its suggestions before confirming. Drag, type a percentage, use the slider, or nudge with arrow keys (0.1%; Shift: 0.01%). Pinch/scroll to zoom up to 15× and drag the photo to pan.
4. Confirm the initial guides. Deliberate manual adjustments then update the comparison live. Automatically suggested/reset guides and rotated or corrected photos require another confirmation. Crop preserves the existing ratios.
5. Add a back photo for the combined comparison. Both sides must be confirmed and meet the same grade's published front/back limits. The selected-side view remains available separately.
6. Save a card with a name and tags; reopen, update, filter or delete it in **Saved**. Export the selected side as a PNG report, a measurement as JSON, or the saved history as JSON. PNG always labels and reports the selected side. JSON includes image and guide data; there is currently no JSON import.

## Measurement and grading

Left percentage = left border width ÷ (left + right border widths) × 100; top/bottom use the same formula. Border widths are distances between the card edge and printed design, independent of the photograph's surrounding background. The more uneven axis governs the centering comparison. The display rounds to one decimal place; comparisons use unrounded measurements.

The **Standards** tab contains the implemented thresholds, exceptions and links to the official sources, checked 7 September 2026. Some organizations do not publish distinct back thresholds or all numeric grade boundaries; the UI shows those gaps and does not interpolate missing grades. CGC and TAG values here are for TCG cards. This compares centering only, not corners, edges, surface, authenticity or a guaranteed final grade.

A clear, flat photo is still necessary. Contrast suggestions can select the wrong edges, and a projective transform cannot undo curvature or lens distortion. Decimal resolution is not photographic accuracy. The browser camera uses the device picker; native macro control, sensor guidance and automatic shutter features are not implemented.

## Storage and verification

Photos are processed locally and are not uploaded to Pokget by Measure. Explicit saves use IndexedDB (`pokget-measure`, `cards`) in this browser; clearing browser storage removes them. They do not sync to the cloud collection. Download exports before moving devices or clearing storage.

Run `npm run test:static`, `npm run build:static`, `npm run check:static` and `go test ./cmd/ui_scan_test -run TestMeasure -count=1`. The browser tests exercise real file upload, guide adjustment, local save/reload, PNG/JSON contents, crop invariance, perspective correction, responsive controls and touch pan without unwanted refresh.
