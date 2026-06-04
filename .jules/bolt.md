# Performance Journal

## 2026-06-04: Mailer Initialization Optimization
- **Observation**: The `Mailer` service was being re-initialized on every `Register` and `ResendVerification` request.
- **Pattern**: Redundant environment variable lookups and object creation for each auth operation.
- **Optimization**: Initialized `Mailer` once in `cmd/pokget/main.go` and stored it in the `Handler` struct.
- **Impact**: Reduced CPU and memory overhead per request, and centralized service management.

## 2026-06-04: Handler Logic Extraction
- **Observation**: `ResendVerification` handler contained complex rate-limiting and database logic, making it difficult to maintain.
- **Optimization**: Extracted core logic into `performResendVerification` helper.
- **Impact**: Improved readability and testability.
