# Scan Stack Upgrade Design

## Objective

Implement the approved Pokget backlog ranges covering scanner UX, image
preprocessing, OCR, matching, fingerprinting, LLM validation, evaluation,
database lifecycle, workers, selected Go refactors, PWA behavior, and a
non-root runtime container.

The work starts from the green `bench-test` baseline. Existing public entry
points remain available through compatibility wrappers while internal callers
migrate to typed requests and results.

## Architecture

The scan boundary accepts a typed request containing image bytes, selected TCG,
card language, optional guide bounds, and diagnostic preferences. The handler
validates upload size, media type, dimensions, TCG, and language before invoking
the detector. The detector works only with candidates scoped to the selected
TCG and language.

Preprocessing produces reusable normalized images and TCG-specific regions.
OCR runs bounded, independently scored passes. Matching returns printing IDs,
ranked candidates, and evidence rather than a free-form name. Fingerprint and
OCR candidates form the only shortlist an LLM may inspect. The LLM must return
one supplied ID or abstain; deterministic validation remains authoritative.

Database access is injected rather than read from the `db` package global.
Workers receive explicit source targets and bounded retry policies. Runtime
resources have explicit lifecycle methods.

## Stages

1. Add characterization tests and benchmark fixtures.
2. Split large files inside their current packages without changing behavior.
3. Add typed scan options/results and compatibility wrappers.
4. Implement upload validation, preprocessing, OCR, matching, and LLM changes.
5. Remove global database dependencies and harden database startup.
6. Implement multi-TCG worker scheduling, retry, backoff, and failure tracking.
7. Consolidate scanner JavaScript, repair PWA scope/offline behavior, and add
   manifest shortcuts.
8. Run unit, race, Tesseract/Docker, browser, detection-matrix, and CPU-model
   benchmarks.
9. Run the application as a non-root container and verify Compose health.

## CPU model evaluation

The benchmark set is TinyLlama, SmolLM2 360M/1.7B, Qwen2.5 0.5B/1.5B,
Llama 3.2 1B/3B, and Gemma 3 270M/1B. Models are evaluated at temperature zero
against identical candidate-ID prompts. Reports include exact-ID accuracy,
unknown-card abstention, invalid-output rate, false-positive rate, cold/warm
latency, and throughput. Model selection is configurable; no benchmark result
silently changes the production default.

## Error handling and security

Invalid or oversized images fail before full decoding. OCR-unavailable and
all-pass-failed conditions are typed errors. Caches and pools are bounded.
Untrusted OCR text is data, never prompt instructions. API errors distinguish
validation, overload, cancellation, timeout, no-match, and internal failures
without exposing raw OCR or provider responses.

## Verification

Each structural stage must preserve the green baseline before behavior changes
begin. Behavioral stages add focused table tests, fuzz targets for untrusted
image/text boundaries, race checks for caches and indexes, and representative
real-image matrix runs. The full model and OCR suites remain separate from fast
pull-request tests because the current service suite takes several minutes.

