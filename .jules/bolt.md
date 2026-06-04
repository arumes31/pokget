# Performance & Engineering Journal - Pokget Vault

## OCR Pipeline Fallback Extraction
- **Problem**: When database trigram matching and LLM refinement both fail, the system returned "Unknown Card", losing potential value from OCR text.
- **Solution**: Implemented `fallbackExtract` in `internal/service/vision.go`.
- **Logic**:
    1. Prioritize sequences of capitalized words (likely card names).
    2. Fallback to the longest word (>3 chars).
    3. Clean OCR noise (punctuation) during extraction.
- **Integration**: Added as Stage 5 in the OCR pipeline, ensuring that even if strict matching fails, the user gets a "best guess" which they can then confirm or edit.
- **Platform Resilience**: Integrated into both Tesseract and Stub implementations of `ProcessCardScan` to ensure consistent behavior across development and production environments.

## Lessons Learned
- **OCR Noise**: Raw OCR text often contains "junk" characters at word boundaries. Using `strings.Trim` with a set of common punctuation significantly improves extraction quality.
- **Context-Aware Stubbing**: When writing stubs for testing, it's important to distinguish between "test control text" (like "OCR Not Available (Stub)") and "actual OCR data" to prevent fallbacks from mangling expected test outputs.
