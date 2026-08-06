# CPU LLM card-resolution benchmark

Date: 2026-08-05  
Host: Intel Xeon Gold 6126, 8 physical cores / 16 threads, 64 GB RAM  
Runtime: Ollama 0.32.3, CPU-only, `num_ctx=2048`, `num_predict=32`, `num_thread=8`, temperature 0, seed 42

The matrix uses the same 14 deterministic prompts for every model: 12 noisy known-card cases spanning Pokémon, Magic, One Piece, Lorcana, Weiss Schwarz, and Yu-Gi-Oh, plus two unrelated-text abstention cases. JSON Schema constrains output to a supplied printing ID or `NONE`.

## Full nine-model matrix

| Model | Known cards | Abstentions | False positives | Invalid | Warm latency | Tokens/s |
|---|---:|---:|---:|---:|---:|---:|
| tinyllama | 10/12 | 0/2 | 2 | 0 | 11.348 s | 8.8 |
| smollm2:360m | 11/12 | 0/2 | 2 | 0 | 3.345 s | 13.5 |
| smollm2:1.7b | 11/12 | 0/2 | 2 | 0 | 16.401 s | 6.8 |
| qwen2.5:0.5b | 9/12 | 0/2 | 2 | 0 | 2.016 s | 21.9 |
| qwen2.5:1.5b | 11/12 | 0/2 | 2 | 0 | 3.649 s | 11.8 |
| llama3.2:1b | 12/12 | 0/2 | 2 | 0 | 5.751 s | 8.6 |
| llama3.2:3b | 12/12 | 0/2 | 2 | 0 | 10.112 s | 4.9 |
| gemma3:270m | 12/12 | 0/2 | 2 | 0 | 3.971 s | 19.4 |
| gemma3:1b | 6/12 | 0/2 | 2 | 0 | 6.224 s | 10.9 |

The full matrix overlapped with parts of the Go regression run, so its latency values are useful as screening measurements, not final comparisons.

## Isolated finalist rerun

| Model | Known cards | Cold latency | Warm latency | Tokens/s |
|---|---:|---:|---:|---:|
| gemma3:270m | 12/12 | 1.519 s | 2.052 s | 26.4 |
| llama3.2:1b | 12/12 | 7.754 s | 3.464 s | 13.8 |
| llama3.2:3b | 12/12 | 57.159 s | 19.698 s | 4.0 |

## Decision

`gemma3:270m` is the default: it retained 12/12 known-card accuracy and was the fastest finalist by a clear margin. A separate production-shaped request using the final one-field response schema selected the correct printing in 1.566 seconds.

No tested small model safely abstained when it was deliberately given only irrelevant text and otherwise valid candidates. The application therefore does not treat the LLM as a safety boundary: deterministic TCG/language/activity scoping and local evidence thresholds decide whether the model is called, the schema restricts it to shortlisted IDs, and response confidence is derived from deterministic evidence rather than a model self-report.

Raw results:

- `llm-cpu-models.json` — all nine models and per-case measurements
- `llm-cpu-finalists.json` — isolated finalist rerun
