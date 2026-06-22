This PR addresses the CI check suite failures in both the lint and security jobs.

**Lint Fixes:**
- Handled ignored error returns for `os.Mkdir` in `internal/db/db_test.go` and `rows.Close()` in `internal/service/worker.go` by explicitly assigning to the blank identifier (`_`) to satisfy `errcheck` and `gosec` (G104).
- Renamed unused function parameters (`card`, `condition`, `multipliers`) to `_` in `internal/service/worker_bench_test.go` to satisfy `revive`.

**Security Fixes:**
- Updated the minimum Go version to `1.26.4` in `go.mod`, `.github/workflows/pipeline.yml`, and `.github/workflows/license.yml` to resolve upstream vulnerabilities in standard libraries like `net/textproto` and `crypto/x509`.
- Updated the `golang.org/x/net` dependency to `v0.55.0` to address known vulnerabilities identified by `govulncheck`.
