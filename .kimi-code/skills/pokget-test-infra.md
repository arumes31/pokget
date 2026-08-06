---
name: pokget-test-infra
description: How to build and run the pokget Go test suite (CGO/tesseract, Docker), and which tests fail on a clean main
metadata:
  type: project
---

pokget is a Go module (`module pokget`, go 1.26). Its tests **cannot run on the Windows host** — they require `CGO_ENABLED=1` plus libtesseract/leptonica (via `github.com/otiai10/gosseract/v2`). Run them in a Linux container instead, e.g. `golang:1.26-bookworm` + `apt-get install libtesseract-dev libleptonica-dev pkg-config tesseract-ocr tesseract-ocr-eng`, mounting the repo at `/app` with persistent volumes for `/go/pkg/mod` and `/root/.cache/go-build`. (From Git Bash, set `MSYS_NO_PATHCONV=1` so `-v`/`-w` container paths aren't mangled.)

CI (`.github/workflows/pipeline.yml`) runs **lint (golangci-lint) + security (govulncheck/gosec)** only — there is **no `go test` job**. So `go test ./...` is a local-only signal.

On a clean `origin/main`, `go test ./...` is **red**: `internal/db` (TestConnect/SeedDatabase/RunMigrations), `internal/handlers` (TestHandlers/Login_Success), and `internal/service` (TestProcessCardScan_Stub/_Full) all fail. These are genuine test/code bugs (handlers & db use **sqlmock**, not a real Postgres — so they ARE runnable), except the two service `ProcessCardScan` tests which also need the OCR stub fix. The `pr-triage` branch (June 2026 bot-PR triage) fixed all of these — see [[pokget-pr-triage-2026-06]].
