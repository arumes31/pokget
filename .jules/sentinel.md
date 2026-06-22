## 2026-06-22 - Fix unhandled error in worker database closure
**Vulnerability:** The CI pipeline failed on a `G104` (CWE-703) security issue where an error from `rows.Close()` was unhandled in `internal/service/worker.go`.
**Learning:** `gosec` strictly enforces that all cleanup methods that return errors, including `Close()`, must have their errors explicitly handled or ignored. Ignoring cleanup errors without `_ =` causes security lint failures.
**Prevention:** Always explicitly check or ignore cleanup method returns (e.g., `_ = rows.Close()`) to adhere to strict security linting rules.
