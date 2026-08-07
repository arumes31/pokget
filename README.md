# <p align="center">✨ POKGET VAULT ✨</p>
<p align="center">
  <code><b>The Prestige Trading Card Management System</b></code>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version">
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

## 🛠️ Core Technology Pillars

### 👁️ Computer Vision & Recognition
*   **Precision OCR**: Integrated Tesseract engine with intelligent pre-processing (Grayscale, High Contrast, Sharpening) to extract card names even from blurry photos.
*   **Perceptual Hashing (pHash)**: Uses `goimagehash` to match card images against a reference database, providing "fuzzy" visual matching that ignores minor lighting differences.
*   **LLM Correction**: Integrated LLM fallback to resolve OCR ambiguities and correct misspelled card names using context-aware matching.

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


---

## 🛠️ Quick Start

### 🐳 Using Docker (Recommended)
```powershell
docker-compose up --build
```
*   **App**: `http://localhost:18066`
*   **Database**: Postgres 15
*   **Reference images**: `./data/catalog-images`

### 🔨 Manual Setup
1.  **Dependencies**: Install `tesseract-ocr`.
2.  **Environment**: Create a `.env` file:
    ```env
    DB_HOST=localhost
    DB_PORT=5432
    SESSION_KEY=your-32-character-secure-key-here
    SMTP_HOST=smtp.gmail.com
    ```
3.  **Run**:
    ```bash
    go run ./cmd/pokget
    ```

---

## 📜 License
Distributed under the **MIT License**. Created with 💜 by **arumes31**.
