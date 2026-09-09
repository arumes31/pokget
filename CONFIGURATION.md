# Configuration and deployment

Start with [example.env](example.env). It contains non-secret defaults, empty
credential fields, and all variables interpolated by both Compose files. Keep
the populated `.env` private. Existing installations should copy individual
settings, not replace their working configuration or rotate their session key
accidentally. The session key also derives the application's encryption key.

## Choose the scan path

| Stage | Runs on | Default / activation | Data sent to server/provider |
| --- | --- | --- | --- |
| Device text OCR | Browser CPU, Tesseract WASM | Scanner's “Read text on this device” option | OCR text to Pokget when successful |
| Image OCR and fingerprints | Pokget CPU, native Tesseract | Image fallback / “Use server image scan” | Prepared image to Pokget |
| Local text selector | Ollama CPU, `qwen2.5:1.5b` | `OLLAMA_MODEL`; optional ambiguity resolution | OCR text and bounded shortlist to Ollama |
| Optional image OCR | Ollama CPU, `qwen3.5:2b` Q8_0 | `SCAN_VISION_OCR_ENABLED=true`; default false | Prepared image to the configured OCR endpoint |

Device OCR has a 20-second budget and loads one selected language. Auto detect,
unsupported browsers, weak OCR or no catalog candidate can fall back to uploading
the image. Ambiguous device results instead offer an explicit server-image retry.
Device-text and model-selected image results require review. No language/model
setting removes that requirement or guarantees an exact physical printing.

An optional primary provider configured by `LLM_BASE_URL`, `LLM_MODEL`, `LLM_API`,
and `LLM_MAX_TOKENS` can receive scan images and approved reference artwork.
Leave `LLM_BASE_URL` empty for local-only model use. The local device-text selector
does not call this primary provider. See the README's provider section for retry
and cancellation behavior.

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
An empty `DB_PASSWORD` currently selects Compose's known `pokget_pass` fallback;
the template does not enforce secure credentials. Set a unique password explicitly.
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
for routine upgrades. No database reset is needed to switch OCR models.

### Local-only text model

Keep `LLM_BASE_URL=` and `SCAN_VISION_OCR_ENABLED=false`, then run:

```sh
docker compose up -d pokget_ollama
docker compose exec pokget_ollama ollama pull qwen2.5:1.5b
docker compose up -d --build pokget_app
```

The app also attempts background setup of its configured text model, but an
explicit pull makes download errors visible before scanning. Image OCR models
are not automatically downloaded. With the GHCR stack, add
`-f docker-compose.ghcr.yml` to every command and use `pull`/`up -d` instead of
`--build` when updating the app.

### Enable Qwen image OCR or migrate from GLM

Pull `qwen3.5:2b` in the same Ollama service. Set these values in `.env`:

```env
SCAN_VISION_OCR_ENABLED=true
SCAN_VISION_OCR_URL=http://pokget_ollama:11434
SCAN_VISION_OCR_MODEL=qwen3.5:2b
SCAN_VISION_OCR_TIMEOUT_SECONDS=45
SCAN_VISION_OCR_THREADS=4
```

Rebuild/recreate the application afterward. Replace an explicit old
`glm-ocr:q8_0` override; changing the source default cannot override `.env`.
Disable with `SCAN_VISION_OCR_ENABLED=false` and recreate the app; no model cache
deletion is required. The text model remains `qwen2.5:1.5b` independently.

Four threads and one in-flight request are the tested CPU baseline, not a host
performance guarantee. The image pilot took roughly 34–44 seconds per real card.
Cold loading can consume nearly the entire deadline. `OLLAMA_MAX_LOADED_MODELS=1`
limits residency but switching text/image models can incur reloads. Increasing
that setting uses more memory; benchmark before changing it. Request-level
keep-alive settings can override the Ollama server's `OLLAMA_KEEP_ALIVE` default.

## Important controls

| Variables | Default | Purpose / constraint |
| --- | --- | --- |
| `SCAN_TIMEOUT_SECONDS` | 75 | Detector queueing and local image-analysis budget |
| `SCAN_OCR_POOL_SIZE` | 3 | Native OCR pool and scan concurrency limit |
| `OMP_THREAD_LIMIT` | 1 in Compose | Native Tesseract OpenMP threads per operation |
| `SCAN_PHASH_HIGH_CONF`, `SCAN_PHASH_POTENTIAL` | 5, 10 | Hash-distance thresholds; do not tune to hide bad matches |
| `SCAN_VISION_OCR_TIMEOUT_SECONDS` | 45 | Valid 1–45; reduced by remaining scan budget |
| `SCAN_VISION_OCR_THREADS` | 4 | Positive CPU thread count for image inference |
| `OLLAMA_NUM_THREAD`, `OLLAMA_NUM_CTX`, `OLLAMA_NUM_PREDICT` | 4, 2048, 128 | Local text-model inference limits |
| `OLLAMA_MAX_CANDIDATES`, `OLLAMA_MIN_EVIDENCE`, `OLLAMA_MIN_CONFIDENCE` | 20, 180, 0.55 | Text shortlist eligibility and response checks, not accuracy probabilities |
| `OLLAMA_NUM_PARALLEL`, `OLLAMA_MAX_LOADED_MODELS` | 1, 1 | Ollama server concurrency/residency |
| `SECURE_COOKIES` | true | Keep enabled behind HTTPS; false only for local HTTP development |
| `WRITE_TIMEOUT` | 120 | General HTTP write timeout; long primary scan completions have separate cancellation behavior |
| `TRUST_PROXY`, `TRUST_CLOUDFLARE` | false, false | Enable only for an intentionally trusted proxy topology |
| `CATALOG_LANGUAGE` | en,de,ja,fr,zh-cn,zh-tw,ko | Requested import languages; upstream coverage still varies by game |
| `CATALOG_WEISS_MAX_PAGES` | 0 | Weiss source pagination cap; zero means no cap |

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
`OLLAMA_HOST=http://localhost:11434` and
`SCAN_VISION_OCR_URL=http://localhost:11434` for native local Ollama. Docker service
names such as `pokget_ollama` do not resolve outside the Compose network.
`MIGRATIONS_PATH=migrations` is relative to the working directory.

## Troubleshooting and privacy

- **Login fails on local HTTP:** secure cookies are intentionally enabled. Use
  HTTPS, or explicitly disable secure cookies only in your local test environment.
- **Phone camera unavailable:** use HTTPS on a trusted origin and grant camera
  permission. Device OCR uses browser CPU/WASM, not native phone NPU acceleration.
- **Browser OCR assets missing:** run `npm ci --ignore-scripts` and
  `npm run build:static` for native development. The Dockerfile builds self-hosted
  assets; no runtime OCR CDN is required.
- **Model unavailable / timeout:** check the configured endpoint and
  `docker compose exec pokget_ollama ollama list`. Pull the exact configured tag.
  A timeout preserves local review candidates; it is not a successful model match.
- **Wrong or missing language/printing:** check catalog sync coverage and active
  localized records. UI English/German settings do not change the card language.
- **Permission denied on cache/images:** fix ownership of the two writable bind
  mounts. Do not disable read-only filesystem or privilege restrictions.
- **Database/Ollama exposure:** current Compose files publish PostgreSQL on 5433
  and Ollama on 11434 on host interfaces. Restrict them using your firewall or
  loopback-only port bindings; Ollama is not an authenticated public API.
- **Private scans:** successful device-text submissions omit the photo, but image
  fallback uploads it. A configured remote primary or vision endpoint receives
  its request data. Choose only trusted endpoints and keep API keys server-side.

Run `docker compose --env-file example.env config --quiet` (and the GHCR variant)
to validate syntax without printing resolved secrets. This does not validate
credentials or start services. See [SECURITY.md](SECURITY.md) for reporting issues
and [the phone OCR report](benchmarks/phone-ocr-20260909.md) for measured limitations.
