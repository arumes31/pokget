# <p align="center">✨ POKGET VAULT ✨</p>
<p align="center">
  <code><b>The Prestige Trading Card Management System</b></code>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.27+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version">
  <img src="https://img.shields.io/badge/HTMX-3366CC?style=for-the-badge&logo=htmx&logoColor=white" alt="HTMX">
  <img src="https://img.shields.io/badge/Tailwind-38B2AC?style=for-the-badge&logo=tailwind-css&logoColor=white" alt="Tailwind">
  <img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="Postgres">
  <a href="https://github.com/arumes31/pokget/actions/workflows/pipeline.yml"><img src="https://github.com/arumes31/pokget/actions/workflows/pipeline.yml/badge.svg?branch=main" alt="CI/CD Pipeline"></a>
</p>

---

## 📱 The Vision
**Pokget** is more than a database; it's a high-performance vault for TCG collectors. Built with a "Prestige" aesthetic and a modern tech stack (Go + HTMX), it combines industrial-grade security with a gamified experience to track, value, and share your collection.

```text
       _______________
      |               |
      |   _________   |
      |  |         |  |
      |  |  [PNG]  |  |  Pokget OCR Engine
      |  |_________|  |  Precision Fingerprinting
      |               |  Real-time Market Data
      |   CHARIZARD   |
      |_______________|
```

---

## Display preferences

Settings offers Light, Dark, and System themes (System by default), and English or German interface language (English by default). Choices are saved in this browser and apply across navigation and reloads. System mode follows live device appearance changes. Card names and personal content are not translated.

Run `npm run test:translations` to check translation behavior and coverage. These checks also run in `npm run test:static` and CI. Mark interface text with `data-i18n` and accessibility text with `data-i18n-attrs="aria-label placeholder"`, then add German copy to `static/js/i18n.js`. Keep user content separate; use numbered placeholders for messages containing values. The coverage guard checks templates, authored JavaScript UI messages, and literal handler errors/notifications, and rejects missing translations or markers.

## 🛠️ Core Technology Pillars

### 👁️ Computer Vision & Recognition
*   **Claude AI Vision Engine**: High-accuracy multimodal recognition powered by Anthropic's Claude API (`claude-sonnet-5-5` or `claude-opus-5-5`) with ephemeral prompt caching. Directly identifies card title, set, collector number, and variant from the camera crop, matching against the catalog with zero client OCR overhead and synthetic fallback for uncatalogued cards.
*   **Full-Screen Mobile Matrix HUD**: An edge-to-edge camera viewfinder engineered for single-screen mobile use. The video stream spans the entire viewport behind high-contrast glassmorphic controls and a persistent, accessible bottom navigation bar.
*   **Offline / Legacy Recognition Pipeline**: When Claude is not configured, Pokget falls back to local Tesseract OCR with intelligent preprocessing (grayscale, high contrast, sharpening), perceptual visual hashing (`goimagehash` pHash), and optional local LLM disambiguation (Ollama).

### Card centering measurement

Open **Scan → Measure** to compare the card's outer edges with its printed design. Measure front and back independently or compare both against the listed PSA, BGS, CGC, SGC, TAG and ACE centering thresholds. Photo import, border suggestions, zoom/pan, fine adjustments, rotation, cropping and four-corner perspective correction run in the browser.

Save named/tagged measurements on this device, reopen them from **Saved**, or export PNG results and JSON data. These measurements are separate from the cloud collection. See [the Measure guide](MEASURE.md) for the workflow and limits.

### 📈 Economic Intelligence
*   **Multi-Market Scraping**: Automated `colly` and `chromedp` (headless) scrapers for real-time price extraction from Cardmarket (EUR) and USD conversions.
*   **Dynamic Currencies**: Users can toggle between **Euro (€)** and **US Dollar ($)** in their account settings, with real-time portfolio recalculation.
*   **Price History**: Tracks historical valuations to provide 24h/7d change statistics and portfolio growth metrics.

### 🎮 The Collector's Journey (Gamification)
*   **XP System**: Earn Experience Points for every card added, scan performed, or successful trade.
*   **Rank Progression**: Advance through ranks from `Novice Collector` to `Vault Master`.
*   **Set Progress**: High-impact visual tracking of set completion percentages (e.g., 151, Paldea Evolved).

---

## 🛡️ Security Architecture
The Pokget vault is hardened using industry standards:
*   **Private Notes**: Card notes are never shown on public vault pages. They are stored unencrypted in PostgreSQL, so protect database access and backups.
*   **Brute-Force Protection**: Token-bucket rate limiting per IP (IPv6 per /64), a per-account failed-login limit, and a cap on concurrent password hashing.
*   **Accounts**: New passwords need at least 12 characters; email verification links expire after 24 hours and are stored hashed.
*   **Audit Logging**: Every sensitive action (Login, Register, Add Card) is immutable logged to the `audit_logs` table.
*   **Session Integrity**: Signed and encrypted HttpOnly/Secure session cookies. Logging out or changing the password revokes the account's sessions on every device.
*   **Validation**: Mandatory password confirmation during registration and CSRF protection on all POST methods.

---

## 🏗️ Internal Structure

| Package | Responsibility |
| :--- | :--- |
| `internal/auth` | Middleware, Hashing, Rate Limiting, Session Management. |
| `internal/service` | OCR Engine, pHash matching, LLM integration, Mailer, Crypto. |
| `internal/handlers` | HTMX-driven logic for Dashboard, APIScan, and Sharing. |
| `internal/catalog` | Versioned multi-TCG catalog, reference-image storage, and fingerprints. |
| `internal/worker` | Background catalog, image, and price synchronization. |
| `internal/db` | Interface-based SQL management and automated migrations. |

---

## 📚 Card Reference Catalog

Pokget maintains a local, versioned card catalog without API keys or paid services. Scheduled full and incremental imports cover:

| Game | Primary source |
| :--- | :--- |
| Pokémon | [TCGdex](https://tcgdex.dev/rest) |
| Magic: The Gathering | [Scryfall bulk data](https://scryfall.com/docs/api/bulk-data) |
| One Piece | [Official English card list](https://en.onepiece-cardgame.com/cardlist/) |
| Disney Lorcana | [LorcanaJSON](https://lorcanajson.org/) |
| Weiss Schwarz | [Official English card search](https://en.ws-tcg.com/cardlist/searchresults/) |
| Yu-Gi-Oh! | [YGOPRODeck](https://ygoprodeck.com/api-guide/) |

Imports retain source provenance and sync history. A successful full snapshot deactivates records no longer present; failed, incremental, and unchanged runs never remove active records. Reference images are downloaded through an exact hostname allowlist, content-addressed by SHA-256, and indexed with perceptual hashes for the original and small rotations.

The application performs scheduled updates automatically. Operators can also run:

```bash
go run ./cmd/catalog sync --game all --mode full
go run ./cmd/catalog status
go run ./cmd/catalog verify
go run ./cmd/catalog images
```

`CATALOG_LANGUAGE` accepts a comma-separated list and defaults to
`en,de,ja,fr,zh-cn,zh-tw,ko`: English, German, Japanese, French, both Chinese
scripts, and Korean. Existing installations with `CATALOG_LANGUAGE=en` in their
deployment environment must update that value and recreate the application
container. The next catalog sync imports the selected languages. The catalog CLI
accepts the same list through `--lang`. TCGdex imports all selected languages in
one source snapshot, preserving separate card and printing identities. LorcanaJSON
imports the supported subset (English, German, French, Italian); other sources
retain their upstream language coverage. A missing language never silently changes
the scanner selection; the prepared image remains available to retry after sync.

Scryfall bulk data is downloaded to a bounded temporary file before database
import, so database processing cannot exhaust the HTTP download timeout. The
temporary file is removed after success or failure.

The first full import can take substantial time and disk space because it downloads large catalogs and reference images. Public sources can change or be temporarily unavailable, and no free public source can guarantee every language, promotional printing, or future physical variant. Sync history and verification commands make such gaps visible.

### 🧪 Detection Acceptance Tests

The versioned acceptance pool contains four independently sourced cards for each supported TCG. A seed reproducibly selects one card per game, downloads and hash-verifies its reference image, then produces seven artifacts: source, clean, blur, resize, 3° rotation, brightness, and JPEG degradation. The resulting 42-case matrix requires the exact canonical card ID and name plus an explicit `needs_review: false` response.

```bash
go run ./cmd/detection_fixtures
go run ./cmd/detection_seed
go run ./cmd/detection_matrix --base-url http://localhost:18066
go run ./cmd/ui_scan_test --base-url http://localhost:18066 \
  --fixture <captured-card-image> \
  --expected-id <canonical-card-id> \
  --expected-name <exact-card-name>
```

Use a different `--seed` to select a different card from each game's pool. “100%” refers specifically to a reproducible seeded 42-case acceptance matrix; it is not a claim that every possible camera, blur level, crop, language, or newly released card will always match. Add captured failure cases to the pool before changing thresholds.

The normal Go suite also exercises a fixed 600-card matching cohort: 100
unique printing IDs for each supported TCG. Every card is ranked against only
its 100-card game scope using exact and OCR-normalized text. Same-name
printings remain explicit ties unless set or collector evidence distinguishes
them.

Close visual matches now wait for OCR before selecting a printing. Name-only
OCR keeps same-name printings available for review; a readable printing ID or
set-and-collector pair takes priority, while disagreement with the artwork
still requires review. Collector numbers match complete tokens, and photo
fingerprints honor EXIF orientation and the selected card crop. Confidence is
an evidence score, not a measured probability of correctness.

On a Linux build with Tesseract installed, run the generated-image integration
checks (including same-art printing disambiguation) with:

```bash
go test -tags=ocrintegration -run '^TestTesseract' -timeout=3m ./internal/service
```

### Verification gates

Pull requests run bounded unit and race-detector shards, real Linux Tesseract
OCR, rendered mobile and service-worker lifecycle tests, static asset tests,
and a production-container smoke test. The container gate starts PostgreSQL on
an isolated network, applies every migration, checks the application health and
runtime dependencies, then verifies a complete dump/restore into a second
database. Historical migrations that cannot be reversed without data loss are
listed in `migrations/irreversible.txt` rather than given unsafe down scripts.

Useful local checks:

```bash
go test -timeout=10m -count=1 ./...
go test -race -timeout=8m -count=1 ./internal/service
go test -timeout=3m -count=1 -v ./cmd/ui_scan_test
docker build -t pokget:verification .
bash scripts/container-smoke.sh pokget:verification
```

The Ubuntu 24.04 CI jobs install Tesseract through
`scripts/ci-apt-install.sh`. It uses only the runner's signed Ubuntu sources, so
an unrelated Chrome repository outage cannot block Go checks. Ubuntu index or
package failures still fail the job; signature/hash verification is not disabled.


---

## 🛠️ Quick Start

### 🐳 Using Docker (Recommended)

Copy [example.env](example.env) to `.env`, then fill in a unique `DB_PASSWORD` and
a random `SESSION_KEY` (generate one with `openssl rand -hex 32`). Do not overwrite
an existing `.env`; compare it with the template when upgrading.

```sh
cp example.env .env
# Edit .env before starting. Use SECURE_COOKIES=false only for local HTTP testing.
docker compose up -d --build
docker compose exec pokget_ollama ollama pull qwen2.5:1.5b
```

PowerShell users can use `Copy-Item example.env .env`. Keep
`SECURE_COOKIES=true` for HTTPS deployments. A phone camera/PWA needs HTTPS when
accessing the server over the LAN; plain `http://<server-IP>` is not a secure
camera origin. See [configuration and deployment](CONFIGURATION.md) for native
setup, proxy settings, model choices, persistence, upgrades, and troubleshooting.

*   **App**: `http://localhost:18066`
*   **Database**: Postgres 15
*   **Reference images**: `./data/catalog-images`

For a published image instead of a source build:

```sh
docker compose -f docker-compose.ghcr.yml pull
docker compose -f docker-compose.ghcr.yml up -d
docker compose -f docker-compose.ghcr.yml exec pokget_ollama ollama pull qwen2.5:1.5b
```

Choose a published tag/digest with `POKGET_IMAGE`. A PR does not publish `latest`;
use a source build to test unmerged changes. Use one Compose file consistently:
the source file uses a named Ollama volume, while the GHCR file uses `./data/ollama`.

### 🔨 Manual Setup

Install Go 1.27.1+, Node.js 26, a C/C++ compiler, Tesseract runtime/development
libraries, Leptonica, `pkg-config`, and the seven OCR language packs. PostgreSQL
must be reachable; Chromium is needed for browser tests and headless scraping.
Build the bundled static assets with `npm ci --ignore-scripts` followed by
`npm run build:static`.

Export the required `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, and
`SESSION_KEY` variables before `go run ./cmd/pokget`. **Native Go does not read
`.env` automatically.** Adapt the template's Compose URLs to localhost and your
actual database port; see [the native configuration guide](CONFIGURATION.md#native-processes).
Logs use readable `key=value` text by default; use `LOG_FORMAT=json` for a
structured-log collector. Catalog image processing reports queue progress and ETA.

#### Catalog fingerprint throughput

Reference images are downloaded and hashed by four parallel processors by default
(`CATALOG_IMAGE_CONCURRENCY=4`, range 1–8). Database writes stay serialized.
`CATALOG_IMAGE_BATCH_SIZE=8` is a lease limit, **not** the number of active processors;
it is capped at twice concurrency to avoid a long queue of waiting leases.
The five-second poll interval applies only to idle/error cycles, not queued batches.
Lower concurrency to 1–2 if background work competes with scans for CPU or RAM;
larger images increase memory use per processor. Recreate the app container after
changing these environment settings. No fingerprint algorithm or matching threshold
changes are required, and existing fingerprints remain valid.

`Catalog image batch finished` reports ready/failed/uncommitted counts and elapsed,
lease-query, processing, and database-write timings. `process_total_ms` sums overlapping
jobs and can exceed wall time; `persist_total_ms` measures serialized writes.
The periodic `Catalog image fingerprints reloaded` event also includes its duration.
With `DEBUG=true`, image ID/source and current stage (`download`, `decode`, `hash`,
`store`) are logged, followed by per-stage timings. Processing `outcome=ready` means
the file and hashes are prepared; only the batch's `ready` count confirms database
publication. URLs and image contents are omitted. During imports the queue grows,
so short-window ETA is an estimate, not a stable completion promise.

#### Scan diagnostics

The scanner captures the card within the Matrix HUD viewfinder and uploads the cropped image directly to the server. When Claude Vision is enabled, the request is dispatched immediately to the Claude API with prompt caching, bypassing secondary pHash and OCR passes for maximum speed and accuracy.

On submission, server logs record `input=server_image` along with the unique `scan_id`. `Scan scope selected` logs the normalized game/language and eligible-card count. `Scan stage started` / `Scan stage finished` track the active stage (`claude_vision` or legacy stages), durations, and completion/timeout outcomes. `Scan request finished` records the HTTP status and total processing duration. Set `DEBUG=true` for detailed stage diagnostics; `LOG_FORMAT=json` enables structured log ingestion.

### 🤖 Claude AI Vision Engine (Recommended)

Pokget features an Anthropic Claude Vision detection engine that replaces brittle client-side OCR and secondary fallback chains with direct multimodal card identification:

*   **Zero Client-Side Overhead**: Cards are captured in the camera HUD and uploaded directly to the server. No heavy WebAssembly downloads, no battery-draining client CPU processing, and no client timeouts.
*   **Prompt Caching & Fast Inference**: Uses Anthropic's ephemeral prompt caching (`CLAUDE_PROMPT_CACHING=true`) with `claude-sonnet-5-5` or `claude-opus-5-5` for fast, cost-effective scans.
*   **Catalog Matching & Synthetic Fallback**: Matches the recognized card name, set code, and collector number against the local PostgreSQL catalog to resolve market pricing. If a newly released or promotional card is not yet cataloged, Claude generates a synthetic card record with direct market search links so collectors can track it immediately.
*   **TCG & Language Guidance**: Selecting the game (Pokémon, MTG, One Piece, Lorcana, Weiss Schwarz, Yu-Gi-Oh!) and card language grounds Claude's extraction prompt and ensures accurate pricing resolution.

To enable Claude AI Vision, configure the following in `.env`:

```env
CLAUDE_ENABLED=true
CLAUDE_API_KEY=your_anthropic_api_key
CLAUDE_MODEL=claude-sonnet-5-5
CLAUDE_PROMPT_CACHING=true
CLAUDE_TIMEOUT_SECONDS=30
```

When `CLAUDE_ENABLED=true` is active, secondary fallback stages (pHash and native OCR) are bypassed, providing clean, direct AI recognition without noisy fallbacks.

### 📱 Full-Screen Mobile HUD & PWA Standalone Mode

The scanner is engineered for single-screen mobile ergonomics:

*   **Edge-to-Edge Viewfinder**: The camera feed fills 100% of the mobile viewport (`.scanner-frame`) behind high-contrast glassmorphic controls and alignment guides.
*   **Persistent Bottom Navigation**: The bottom menu bar (`.app-bottom-nav`) remains visible, docked, and interactive above the camera layer, enabling one-tap switching between Vault, Scanner, Centering Measure, and Settings.
*   **Single-Screen Layout**: Viewfinder controls, quick action buttons, and TCG/language selectors float in the thumb zone without vertical scrolling.

#### Installing the PWA (Removing the Browser Bar)

When opening Pokget in a standard mobile browser tab (Safari on iOS or Chrome on Android), mobile browsers display their top URL address bar and bottom navigation bar by default for standard web pages:

*   **iOS (Safari)**: Open Pokget in Safari → Tap the **Share** button (box with upward arrow) → Tap **Add to Home Screen** → Tap **Add**. Launch Pokget from the home screen icon to run in borderless standalone mode without the Safari address bar.
*   **Android (Chrome)**: Open Pokget in Chrome → Tap the three-dot menu (**⋮**) → Tap **Install app** (or **Add to Home Screen**). Launch Pokget from the home screen or app drawer to experience full-screen standalone immersion.

Pokget includes `<meta name="mobile-web-app-capable" content="yes">`, `<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">`, and modern `display_override: ["standalone", "minimal-ui", "window-controls-overlay"]` web manifest declarations to guarantee a native, edge-to-edge application experience.

### Primary LLM with Ollama fallback

Set these variables in `.env` to use an OpenAI-compatible gateway for LLM-assisted
card matching and binder names:

```env
LLM_BASE_URL=https://your-gateway.example/v1
LLM_MODEL=moonshotai/kimi-k3
LLM_API=your-api-key
LLM_MAX_TOKENS=1024
```

The primary model can inspect the cropped card image when a scan needs help.
When shortlisted printings share identical text metadata, it can compare labeled
reference artwork from approved catalog image hosts. References are limited to
eight images in complete groups; incomplete groups are omitted.
Its answer must still identify a card from the evidence-backed catalog shortlist;
uncertain printings require confirmation. Provider failures and invalid answers
are retried up to three total primary attempts, with five seconds between failed
attempts. A running primary completion has no application-imposed time limit;
canceling the scan still cancels the request and any pending retry. After three
failures, the existing `OLLAMA_*` provider receives its own bounded request budget.
A valid abstention is a completed answer and does not trigger retries or fallback.
`SCAN_TIMEOUT_SECONDS` still bounds detector queueing and local image analysis;
it does not terminate an active primary completion. Scan and binder-name requests
remain cancellable when their caller disconnects. Leave `LLM_BASE_URL` empty for
Ollama-only operation. Keep the key in `.env`, never in source or logs.

For Ollama-only scans, optional text disambiguation takes at most five seconds
and at most half the remaining scan budget. If it times out, completed local
matches remain available for review instead of being discarded.

Ollama defaults to 128 output tokens so canonical printing IDs fit in structured
responses. Compose sets `OMP_THREAD_LIMIT=1` for Tesseract; the application already
pools OCR clients for concurrent scans. This bounds native OCR threading and
reduced recognition time in the local card-image tests. The setting is configurable;
see the [Tesseract threading documentation](https://tesseract-ocr.github.io/tessdoc/FAQ.html#can-i-increase-speed-of-ocr).

### Optional CPU card OCR with Qwen3.5 2B

`qwen3.5:2b` replaces the unsuccessful GLM-OCR pilot as the default model for this
separate **image OCR** stage. It does not replace the text-only `OLLAMA_MODEL`
fallback above. The stage remains disabled by default and always requires review
for model-selected cards. It asks for the visible card name and complete collector
number in the original language, not a full transcript or a guessed catalog ID.

The 2026-09-08/09 CPU pilot used Ollama 0.32.3, Q8_0 weights (about 2.7 GB),
four CPU threads and no GPU. With the focused prompt, all three real-card PNGs
contained the correct name and collector number (32–36 seconds per image), as
did all seven synthetic language samples. Some responses included extra text.
Full-transcription prompts timed out; the smaller 0.8B model misread collector
numbers, and Qwen3-VL 2B Q4_K_M failed accuracy/latency checks. These are small
pilot results, not proof of perfect multilingual or end-to-end printing accuracy.

The production Go client also accepted all ten JPEG-quality-95 pilot inputs with
the correct name and number: real cards took 33.7–44.4 seconds (Furret included a
cold model load), and synthetic samples took 17.2–25.6 seconds. Only two synthetic
responses exactly followed the requested two-field format; extra text and field
ordering remain variable. This is a slow fallback, not a replacement for the
local OCR/fingerprint first stage. Real photographs per language/game still need
held-out validation before broad enablement.

For an explicit trial with a build containing this integration, first pull the
model into the Compose Ollama service:

```sh
docker compose up -d pokget_ollama
docker compose exec pokget_ollama ollama pull qwen3.5:2b
```

Then set these variables in `.env` and rebuild/recreate the application:

```env
SCAN_VISION_OCR_ENABLED=true
SCAN_VISION_OCR_URL=http://pokget_ollama:11434
SCAN_VISION_OCR_MODEL=qwen3.5:2b
SCAN_VISION_OCR_TIMEOUT_SECONDS=45
SCAN_VISION_OCR_THREADS=4
```

```sh
docker compose up -d --build pokget_app
```

Native, non-Compose applications default to `http://localhost:11434`. Configure
only a trusted Ollama endpoint: it receives the cropped scan image. Startup does
not download models or change the existing text model. Set
`SCAN_VISION_OCR_ENABLED=false` and recreate the app to turn the stage off.

If upgrading from the GLM experiment, replace any explicit
`SCAN_VISION_OCR_MODEL=glm-ocr:q8_0` override; environment overrides take precedence
over the new default. The old GLM-specific `Text Recognition:` protocol is no
longer used. Updating source defaults does not change an already running app.

The stage uses [Ollama native chat](https://docs.ollama.com/api/chat) with one image
message, `think=false`, normalized JPEG images, `num_gpu=0`, and one in-flight
request per application instance. Sampling follows the
[Qwen3.5 non-thinking vision settings](https://huggingface.co/Qwen/Qwen3.5-2B)
with a fixed seed and a 512-token output limit. Busy instances fail fast; requests are limited
to 45 seconds (configurable downwards and shortened for the scan deadline).
Model loading and host contention consume this same budget: cold requests can
approach the limit, and a shorter remaining scan deadline can cause abstention.
It runs only for scoped scans without strong local printing evidence when local
scores are low or nearby printings are ambiguous. Local OCR/fingerprints remain
the first stage and the fallback on model failure.

Only complete `done=true`, `done_reason=stop` responses are accepted. Truncation,
repetition, invalid/oversized responses, redirects and provider failures produce
a `vision_ocr` stage error, not a guessed card. Text must match an exact catalog
name or localized alias plus strong collector evidence for **one** eligible
printing. Model-selected suggestions are capped below automatic acceptance and
always require review; they never become deterministic printing evidence or get
counted a second time by the text LLM.

Language scope remains the user's selection: English, German, French, Japanese,
Korean, simplified Chinese or traditional Chinese. `any` searches those catalog
languages but does not guess between otherwise identical printings. Matching
requires active catalog records and localized aliases; adding a language also
requires catalog coverage, an installed Tesseract language pack and real-image
validation. Unicode unit tests alone do not establish OCR accuracy.

Regression tests cover all seven language scopes, inactive/wrong-game records,
ambiguous identifiers, fingerprint conflicts, mandatory review, malformed or
repetitive model output, cancellation, concurrency and fallback timeouts:

```sh
go test -race ./internal/visionocr
go test ./internal/service -run TestVisionOCR
go test ./internal/config ./cmd/pokget
```

Service/application tests require the project's CGO/Tesseract build environment.
Before enabling broadly, compare local-only and hybrid scans on a held-out corpus
of real cards **per language and game**, including variants, glare, blur and
rotation. Record exact printing accuracy, false automatic matches, abstentions,
completion failures, review rate and p50/p95 latency. Do not relax completion or
review guards merely to increase the reported match rate.

---

## 📜 License
Distributed under the **MIT License**. Created with 💜 by **arumes31**.
