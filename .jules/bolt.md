# Performance Journal - Pokget Vault

## Critical Performance Learnings
- Tesseract OCR is CPU-intensive and requires global locking (via `ocrMu`) to prevent memory exhaustion and race conditions in C-bindings.
- Visual fingerprinting (phash) is significantly faster than OCR and should always be attempted first.
- SQL Trigram matching (`%` operator) provides fast fuzzy search for card names directly in the database.

## Architectural Bottlenecks
- Synchronous OCR in the request-response cycle can lead to high latency and timeouts under load.
