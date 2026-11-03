# Configuration and deployment

Start with [example.env](example.env). It contains non-secret defaults, empty
credential fields, and all variables interpolated by both Compose files. Keep
the populated `.env` private. Existing installations should copy individual
settings, not replace their working configuration or rotate their session key
accidentally. The session key also derives the application's encryption key.

## Choose the scan path

| Stage | Runs on | Default / activation | Data sent to server/provider |
| --- | --- | --- | --- |
| **Claude AI Vision Detector** | Anthropic Claude API (`claude-sonnet-5-5` / `claude-opus-5-5` / `claude-haiku-5-5`) | `CLAUDE_ENABLED=true` (Requires `CLAUDE_API_KEY`; default `false`); uses Anthropic Prompt Caching | Card image to Anthropic API |
| Local text selector | Ollama CPU, `qwen2.5:1.5b` | `OLLAMA_MODEL`; card-name suggestions only | Text and bounded shortlist to Ollama |

The phone never runs OCR: the cropped image is always uploaded, and only the
server performs recognition. Card recognition requires the Claude detector; a
server without it answers scans with a clear configuration error instead of
guessing. A model-selected image result requires review. No
language/model setting removes that requirement or guarantees an exact physical
printing.

An optional primary provider configured by `LLM_BASE_URL`, `LLM_MODEL`, `LLM_API`,
and `LLM_MAX_TOKENS` receives card-name suggestion prompts only. Leave
`LLM_BASE_URL` empty for local-only text suggestions. See the README's provider
section for retry and cancellation behavior.

### Claude AI Card Detection & Prompt Caching

Pokget integrates the Anthropic Claude API for high-precision, zero-hallucination card detection across 6 supported TCGs (Pokémon, Magic: The Gathering, Yu-Gi-Oh!, One Piece, Disney Lorcana, Weiß Schwarz).

- **Default Model:** `claude-sonnet-5-5` (Recommended).
  - *Cost Efficiency:* ~5x cheaper than Opus ($3.00/Mtok input, $0.30/Mtok cache read vs $15.00/Mtok input, $1.50/Mtok cache read).
  - *Latency:* ~2.8s-3.6s end-to-end response time.
  - *Accuracy:* Exact identification of card name, collector number fraction (e.g. `16/110`, `4/102`), expansion/set, language (e.g. `deu`, `eng`, `jpn`), and variant/rarity (Delta Species, Holo, Reverse, etc.).
- **Alternative Model:** `claude-opus-5-5`.
  - Configurable via `CLAUDE_MODEL=claude-opus-5-5`.
  - Employs thinking reasoning before outputting structured JSON.
- **Anthropic Prompt Caching:**
  - Automatically enabled via `CLAUDE_PROMPT_CACHING=true` with `anthropic-beta: prompt-caching-2024-07-31`.
  - The comprehensive card recognition system instructions (>1,024 tokens) are cached with ephemeral cache control.
  - Subsequent card scans read the entire system prompt from Anthropic's cache with a **90% discount** and lower latency.
- **Configuration Variables:**
  - `CLAUDE_ENABLED=true` (Default: `false` unless explicitly enabled)
  - `CLAUDE_API_KEY=<your-anthropic-api-key>`
  - `CLAUDE_MODEL=claude-sonnet-5-5`
  - `CLAUDE_TIMEOUT_SECONDS=30`
  - `CLAUDE_PROMPT_CACHING=true`

## Docker Compose

The source stack is `docker-compose.yml`; the published-image stack is
`docker-compose.ghcr.yml`. Do not start both simultaneously: container names and
ports overlap. Set credentials before starting; example blanks are not safe
production credentials. Shell environment variables can override `.env` values.
Compose uses `.env` for interpolation; it does not automatically forward every
entry into the application. Only entries in `environment:` are forwarded.

Both files expose the same app settings. Internal database routing stays fixed
at `pokget_db:5432` with `DB_SSLMODE=disable`; this applies only to the bundled
database. The native-only database settings in the template do not override it.
There is no default database password: Compose refuses to start while
`DB_PASSWORD` is blank. Set a unique password explicitly.
`APP_PORT` sets both sides of the published application port. `POKGET_IMAGE` is
used only by the GHCR file; use a published tag or digest for reproducible updates.
PR builds verify images but do not publish them to `latest`.

The application runs as UID/GID 10001 with a read-only root filesystem. Ensure
`data/cache` and `data/catalog-images` exist and are writable by that user on
Linux bind mounts. Do not solve permission errors by running the app privileged.

| Data | Source Compose | GHCR Compose |
| --- | --- | --- |
| PostgreSQL | `./data/postgres` | `./data/postgres` |
| Catalog images | `./data/catalog-images` | `./data/catalog-images` |
| App cache/failure log | `./data/cache` | `./data/cache` |
| Ollama model cache | Compose-managed `ollama_data` volume | `./data/ollama` |

Switching Compose files does not migrate the model cache. Keep database backups,
the session key, and any required stored assets together. `docker compose down -v`
removes named volumes, including the source stack's downloaded models; avoid it
for routine upgrades. No database reset is needed to change detection settings.

### Local-only text model

Keep `LLM_BASE_URL=` empty, then run:

```sh
docker compose up -d pokget_ollama
docker compose exec pokget_ollama ollama pull qwen2.5:1.5b
docker compose up -d --build pokget_app
```

The app also attempts background setup of its configured text model, but an
explicit pull makes download errors visible before scanning. The text model only
suggests card names; card recognition itself runs through the Claude detector.
With the GHCR stack, add `-f docker-compose.ghcr.yml` to every command and use
`pull`/`up -d` instead of `--build` when updating the app.

## Important controls

| Variables | Default | Purpose / constraint |
| --- | --- | --- |
| `SCAN_TIMEOUT_SECONDS` | 75 | Detector queueing budget |
| `SCAN_MAX_CONCURRENT` | 3 | Concurrent scan requests per instance |
| `SCAN_PHASH_HIGH_CONF`, `SCAN_PHASH_POTENTIAL` | 5, 10 | Hash-distance thresholds; do not tune to hide bad matches |
| `OLLAMA_NUM_THREAD`, `OLLAMA_NUM_CTX`, `OLLAMA_NUM_PREDICT` | 4, 2048, 128 | Local text-model inference limits |
| `OLLAMA_MAX_CANDIDATES`, `OLLAMA_MIN_EVIDENCE`, `OLLAMA_MIN_CONFIDENCE` | 20, 180, 0.55 | Text shortlist eligibility and response checks, not accuracy probabilities |
| `OLLAMA_NUM_PARALLEL`, `OLLAMA_MAX_LOADED_MODELS` | 1, 1 | Ollama server concurrency/residency |
| `SECURE_COOKIES` | true | Keep enabled behind HTTPS; false only for local HTTP development |
| `WRITE_TIMEOUT` | 120 | General HTTP write timeout; long primary scan completions have separate cancellation behavior |
| `TRUST_PROXY`, `TRUST_CLOUDFLARE` | false, false | Enable only for an intentionally trusted proxy topology |
| `TRUSTED_PROXIES` | loopback + private ranges | Comma-separated CIDRs/IPs whose `X-Forwarded-For`/`X-Real-IP` headers are honoured when `TRUST_PROXY=true`; the right-most untrusted `X-Forwarded-For` hop is used |
| `CATALOG_LANGUAGE` | en,de,ja,fr,zh-cn,zh-tw,ko | Requested import languages; upstream coverage still varies by game |
| `CATALOG_WEISS_MAX_PAGES` | 0 | Weiss source pagination cap; zero means no cap |
| `CATALOG_IMAGE_CONCURRENCY` | 4 | Parallel image downloads/hashing, 1–8; database writes remain serialized |
| `CATALOG_IMAGE_BATCH_SIZE` | 8 | Maximum leased batch, capped at twice concurrency to limit waiting leases |
| `CATALOG_IMAGE_POLL_INTERVAL_MS` | 5000 | Delay after idle/error cycles; no delay while successfully draining queued work |
| `PRICE_EUR_FALLBACK` | true | Derive EUR from the USD market price with the daily ECB rate; converted rows carry `price_eur_estimated` |
| `PRICE_BACKFILL_BATCH` | 25000 | Cards per hourly per-card price backfill pass; `0` disables it, negative values are rejected at startup |
| `PRICE_BACKFILL_REQUESTS_PER_SECOND` | 6 | Request rate of the backfill, separate from `WORKER_REQUESTS_PER_SECOND`; `0` falls back to 2 |
| `PRICE_TRACKED_LIMIT` | 100 | Cards per hourly per-card refresh for portfolio/wantlist/alerts; the budget is split across the collectors that track cards |
| `PRICE_BULK_INTERVAL_HOURS` | 24 | Refresh cadence of the free bulk exports (Scryfall, YGOPRODeck, TCGCSV); they also run once at startup |
| `PORTFOLIO_PHOTO_STORE` | `data/card-photos` | Directory that receives the scanned card photos; Compose mounts `/app/data/card-photos` |

Catalog scheduling, image batches, worker retry/rate/circuit settings and price
retention defaults are annotated in the template. Compose fixes image storage
and worker failure paths to writable mounts. The native config otherwise defaults
to `data/catalog-images` and `data/worker-failures.jsonl`. Keep legacy metadata
sync disabled unless intentionally maintaining that older import path.

## Native processes

`go run ./cmd/pokget` reads process environment variables, not a dotenv file.
Use your process manager or explicitly export values. If you source a file in a
shell, it must be a trusted, shell-compatible file; quote values containing shell
metacharacters. Set all required database values plus `SESSION_KEY`.

For a native app connecting to the bundled Compose database, use
`DB_HOST=localhost`, `DB_PORT=5433`; for a separately installed database use its
actual port (often 5432). Use `DB_SSLMODE` appropriate to that server. Set
`OLLAMA_HOST=http://localhost:11434` for native local Ollama. Docker service
names such as `pokget_ollama` do not resolve outside the Compose network.
`MIGRATIONS_PATH=migrations` is relative to the working directory.

## Troubleshooting and privacy

- **Login fails on local HTTP:** secure cookies are intentionally enabled. Use
  HTTPS, or explicitly disable secure cookies only in your local test environment.
- **Phone camera unavailable:** use HTTPS on a trusted origin and grant camera
  permission. The phone only captures and crops; all recognition runs on the server.
- **Missing detector:** set `CLAUDE_ENABLED=true` with a valid `CLAUDE_API_KEY`.
  Without it scans return "Card recognition is not configured on this server"
  instead of guessing a card.
- **Model unavailable / timeout:** check the configured endpoint and
  `docker compose exec pokget_ollama ollama list`. Pull the exact configured tag.
  A timeout preserves local review candidates; it is not a successful model match.
- **Wrong or missing language/printing:** check catalog sync coverage and active
  localized records. UI English/German settings do not change the card language.
- **Permission denied on cache/images:** fix ownership of the two writable bind
  mounts. Do not disable read-only filesystem or privilege restrictions.
- **Database/Ollama exposure:** the Compose files publish PostgreSQL (5433) and
  Ollama (11434) on `127.0.0.1` only, for native tools on the same host. Do not
  widen these bindings; Ollama is not an authenticated API.
- **Private scans:** every scan uploads the cropped photo; nothing is recognized
  on the device. A configured remote primary endpoint or the Claude detector
  receives its request data. Choose only trusted endpoints and keep API keys
  server-side.

Run `DB_PASSWORD=placeholder docker compose --env-file example.env config --quiet`
(and the GHCR variant) to validate syntax without printing resolved secrets. This does not validate
credentials or start services. See [SECURITY.md](SECURITY.md) for reporting issues
and [the phone scan report](benchmarks/phone-ocr-20260909.md) for historical
server-side limitations.
