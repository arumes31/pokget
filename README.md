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
*   **Precision OCR**: Integrated Tesseract engine with intelligent pre-processing (Grayscale, High Contrast, Sharpening) to extract card names even from blurry photos.
*   **Perceptual Hashing (pHash)**: Uses `goimagehash` to match card images against a reference database, providing "fuzzy" visual matching that ignores minor lighting differences.
*   **LLM Correction**: Integrated LLM fallback to resolve OCR ambiguities and correct misspelled card names using context-aware matching.

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
*   **Encryption**: Secure card metadata and private notes using AES-GCM 256-bit encryption.
*   **Brute-Force Protection**: Token-bucket rate limiting applied per IP.
*   **Audit Logging**: Every sensitive action (Login, Register, Add Card) is immutable logged to the `audit_logs` table.
*   **Session Integrity**: 32-byte secure session keys with HttpOnly/Secure cookie standards.
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
Build the bundled browser OCR assets with `npm ci --ignore-scripts` followed by
`npm run build:static`.

Export the required `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, and
`SESSION_KEY` variables before `go run ./cmd/pokget`. **Native Go does not read
`.env` automatically.** Adapt the template's Compose URLs to localhost and your
actual database port; see [the native configuration guide](CONFIGURATION.md#native-processes).
Logs use readable `key=value` text by default; use `LOG_FORMAT=json` for a
structured-log collector. Catalog image processing reports queue progress and ETA.

### Phone-first scanning with local Ollama

The web/PWA scanner defaults to **Read text on this device**. The phone handles
orientation, guide cropping, resizing, and Tesseract WebAssembly OCR in workers,
including enlarged name/collector-number regions. Only the selected language is
loaded; language data is cached and the OCR session is reused. Workers are
terminated on cancellation, navigation/backgrounding, or after one idle minute.
Local OCR has a 20-second budget, including initialization.

Pokget receives text, scopes the active catalog by TCG/language, and validates
all matches. Clear identities and same-name printing ambiguity skip the model;
other ambiguous identities can use local Ollama within a five-second budget.
Request-local short IDs reduce generation work and are resolved to real catalog
IDs on the server. Device-text results **always require printing confirmation**.
No model credentials, card catalog, or LLM weights are sent to the phone.

Weak/failed device OCR, unsupported browsers, Auto detect, or no catalog candidate
use the existing image pipeline. An ambiguous result does not automatically
upload the photo: use **Use server image scan** to request image analysis.
This is browser CPU/WASM offloading, not native Android/iOS NPU integration.

For the tested local text-model configuration:

```env
LLM_BASE_URL=
OLLAMA_MODEL=qwen2.5:1.5b
OLLAMA_NUM_THREAD=4
SCAN_VISION_OCR_ENABLED=false
```

```bash
docker compose exec pokget_ollama ollama pull qwen2.5:1.5b
```

Run `npm ci --ignore-scripts` and `npm run build:static` before native development
or deployment. Docker builds do this automatically. Pinned OCR assets and all
seven language packs are bundled from npm and served by Pokget; there are no
runtime OCR CDN requests. Native development serves the generated OCR assets from
`dist/static/vendor/ocr`, and images use the built `static/vendor/ocr` directory.
Selecting Auto detect intentionally avoids loading seven OCR models on a phone.

The model is an optional shortlist selector, not a reliable abstention or
printing-confidence estimator. See the [CPU and browser test report](benchmarks/phone-ocr-20260909.md)
for measured results, corpus limitations, and reproduction commands.

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
