## 2025-02-14 - Prevent User Enumeration Timing Attack
**Vulnerability:** The `Login` HTTP handler was vulnerable to a timing attack for user enumeration. It would immediately return a 401 error if `sql.ErrNoRows` occurred, skipping the expensive `bcrypt` password verification that takes ~2.6 seconds. This allowed an attacker to determine if an email address was registered by observing the response times.
**Learning:** Returning early on database misses in authentication flows bypasses computationally heavy operations, creating an observable timing difference that leaks whether a user exists in the system.
**Prevention:** Always ensure a constant-time path in authentication flows by executing the expensive hashing operation (e.g., bcrypt check) against a precomputed dummy hash when a user is not found.

## 2025-02-14 - Dependency Vulnerabilities in HTML Parsing
**Vulnerability:** Multiple denial-of-service and logic bugs (e.g. XSS via duplicate attributes) were found in the `golang.org/x/net/html` dependency via `govulncheck`. These parsing bugs affected the `colly` web scraper used for fetching card prices in `internal/service/price.go`.
**Learning:** Even though our code is safe, external dependencies doing parsing can introduce unexpected attack surfaces. The `colly` scraper uses `golang.org/x/net/html` extensively.
**Prevention:** Always run `govulncheck` in the CI pipeline to quickly detect newly disclosed vulnerabilities in external standard and semi-standard library dependencies, and keep `go.mod` up to date.
