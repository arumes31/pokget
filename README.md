# <p align="center">✨ POKGET VAULT ✨</p>
<p align="center">
  <code><b>The Prestige Trading Card Management System</b></code>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version">
  <img src="https://img.shields.io/badge/HTMX-3366CC?style=for-the-badge&logo=htmx&logoColor=white" alt="HTMX">
  <img src="https://img.shields.io/badge/Tailwind-38B2AC?style=for-the-badge&logo=tailwind-css&logoColor=white" alt="Tailwind">
  <img src="https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white" alt="Postgres">
  <img src="https://img.shields.io/badge/Coverage-90%2B%25-brightgreen?style=for-the-badge" alt="Coverage">
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
| `internal/worker` | Background tasks for periodic price synchronization. |
| `internal/db` | Interface-based SQL management and automated migrations. |

---

## 🚀 Reaching 100% Coverage
The project maintains a rigorous testing standard:
*   **Mocks Everywhere**: Custom `MockMailer`, `MockLLMClient`, and `redismock` ensure tests run without external dependencies.
*   **Fuzzing**: Algorithmic components (Levenshtein distance, XP calculation) are verified for all edge cases.
*   **Static Analysis**: Zero warnings from `golangci-lint`, `govulncheck`, and `gosec`.

### 🧪 Running Comprehensive Tests
We have a comprehensive test suite that validates the integration of all system components (OCR, downloading, fingerprinting, and indexing):
1. **OCR & Indexing**: Picks 5 random cards from the local `test_cards/` cache, runs them through the Tesseract OCR pre-processing and extraction pipeline, and verifies that the `dont fingerprint test_cards` constraint is respected (i.e. they are not registered in the database).
2. **Download**: Fetches cards from the TCGdex API and downloads high-resolution card images.
3. **Fingerprinting & Indexing**: Calculates the perceptual hash (pHash) of the downloaded card, inserts it into the database, rebuilds the BK-tree, and asserts that a pHash-based BK-tree search successfully retrieves and matches the card with zero distance.

To run the comprehensive test suite inside Docker:
```bash
# 1. Build the test container
docker build -t pokget_test -f Dockerfile.test .

# 2. Start the database container
docker compose up -d pokget_db

# 3. Run the comprehensive test suite
docker run --rm --network pokget_default -e DB_HOST=pokget_db -e DB_PORT=5432 -e DB_USER=pokget_user -e DB_PASSWORD=pokget_pass -e DB_NAME=pokget_db pokget_test go run cmd/comprehensive_test/main.go
```


---

## 🛠️ Quick Start

### 🐳 Using Docker (Recommended)
```powershell
docker-compose up --build
```
*   **App**: `http://localhost:8080`
*   **Database**: Postgres 15
*   **Cache**: Redis 7

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
    go run ./cmd/pokget/main.go
    ```

---

## 📜 License
Distributed under the **MIT License**. Created with 💜 by **arumes31**.
