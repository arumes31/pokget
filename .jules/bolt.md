# Bolt Performance Journal

## 2026-06-04: Image Processing Optimization

### Context
The application uses edge detection (`DetectCardEdges`) for auto-snapping guides in the card scanner. This involves decoding the image, converting to grayscale, and applying a Sobel operator.

### Bottleneck
Sobel edge detection complexity is O(N), where N is the number of pixels. Processing high-resolution images (e.g., 4K from modern phone cameras) can be slow and memory-intensive, especially on lower-end devices or containers.

### Optimization
Resizing the image to a maximum dimension of 500px before applying edge detection.
- For a 4000x3000 image, this reduces the pixel count from 12,000,000 to ~187,500 (a ~98.4% reduction).
- 500px is still more than enough resolution to accurately find the high-contrast edges of a trading card.

### Impact
- Significant reduction in CPU time for `effect.Sobel`.
- Significant reduction in peak memory usage.
- Improved latency for the user-facing edge detection feature.
