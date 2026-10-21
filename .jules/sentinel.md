## 2024-10-21 - Prevent Timing Attacks in Login Handler
**Vulnerability:** The Login handler returns early on database misses (sql.ErrNoRows) for users, bypassing the expensive bcrypt password check. This allows attackers to enumerate users by measuring response times.
**Learning:** Returning early on "not found" errors in authentication flows creates a distinguishable timing difference compared to a valid lookup followed by a failed password check.
**Prevention:** Always perform a dummy password hash check using the application's actual hashing function when a user is not found to equalize the response time. Avoid comparing against a hardcoded dummy hash to prevent mismatched cost factors.
## 2024-10-21 - Update Go Version to fix standard library CVE
**Vulnerability:** govulncheck flagged GO-2026-6809 (Distrust compromised intermediate certificates in crypto/x509) in Go 1.27.2.
**Learning:** Standard library CVEs require a go-version bump in both go.mod and CI workflow files.
**Prevention:** Monitor Go releases and keep the toolchain updated.
