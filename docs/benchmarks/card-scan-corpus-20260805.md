# Six-TCG card-scan corpus audit

Date: 2026-08-05  
Endpoint: authenticated Docker test stack at `http://localhost:18066`  
Runtime: final Alpine/Tesseract image, PostgreSQL catalog, CPU-only Ollama 0.32.3

## Method

The fixed cohort contains exactly 600 unique printing IDs: 100 each for Pokémon, Magic, One Piece, Lorcana, Weiss Schwarz, and Yu-Gi-Oh. Each printing is scanned twice, once from its verified catalog source image and once after a Gaussian blur, for 1,200 live API requests.

A strict pass requires HTTP 200, the exact case-sensitive name, the exact printing ID, and `needs_review=false`. The harness authenticates as a real user, submits the selected TCG and language, validates source SHA-256 values, and records response latency and rate-limit retries.

## Results

| TCG | Unique cards | Cases | Strict pass | Exact ID | Exact name |
|---|---:|---:|---:|---:|---:|
| Pokémon | 100 | 200 | 200 (100%) | 200 | 200 |
| Magic | 100 | 200 | 200 (100%) | 200 | 200 |
| One Piece | 100 | 200 | 200 (100%) | 200 | 200 |
| Lorcana | 100 | 200 | 198 (99%) | 200 | 200 |
| Weiss Schwarz | 100 | 200 | 185 (92.5%) | 196 | 200 |
| Yu-Gi-Oh | 100 | 200 | 200 (100%) | 200 | 200 |
| **Total** | **600** | **1,200** | **1,183 (98.58%)** | **1,196 (99.67%)** | **1,200 (100%)** |

The retained pre-upgrade baseline was 1,152/1,200 strict passes (96%). This run adds 31 strict passes and 2.58 percentage points. Source images passed 597/600; blurred images passed 586/600.

## Failure classification

All 17 strict failures were rerun against the final binary:

- 13 returned the correct printing ID and name with `needs_review=true`.
- 4 returned a different printing ID with the same exact card name and `needs_review=true`.
- 0 returned a card from another TCG.
- 0 returned a wrong card name.
- 0 timed out or returned a protocol/server error.
- 0 requests required an HTTP 429 retry.

The 17 final review responses completed in 142–884 ms. They are visually indistinguishable or blur-ambiguous reprints, not silent matches. For example, Lorcana's “Tinker Bell - Tiny Tactician” set 9/189 and set 1/194 use identical artwork and have the same perceptual hash. The API returns both candidates for manual review rather than claiming unsupported printing certainty.

## Evidence

The reproducible raw evidence remains in the local, gitignored QA workspace:

- `artifacts/qa/card_cohort_a/post_upgrade_final_20260805/` — Pokémon, Magic, One Piece; 600/600 strict passes.
- `artifacts/qa/card_cohort_b/post_upgrade_final_20260805/` — Lorcana, Weiss Schwarz, Yu-Gi-Oh; main results plus final all-failure rerun.

The tracked `internal/detectiontest/testdata/card-cohort-600.csv` preserves the six-TCG/100-printing cohort contract in the test suite.
