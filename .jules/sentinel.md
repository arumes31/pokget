## 2024-10-21 - Prevent Timing Attacks in Login Handler
**Vulnerability:** The Login handler returns early on database misses (sql.ErrNoRows) for users, bypassing the expensive bcrypt password check. This allows attackers to enumerate users by measuring response times.
**Learning:** Returning early on "not found" errors in authentication flows creates a distinguishable timing difference compared to a valid lookup followed by a failed password check.
**Prevention:** Always perform a dummy password hash check using the application's actual hashing function when a user is not found to equalize the response time. Avoid comparing against a hardcoded dummy hash to prevent mismatched cost factors.
## 2024-10-21 - Update Go Version to fix standard library CVE
**Vulnerability:** govulncheck flagged GO-2026-6809 (Distrust compromised intermediate certificates in crypto/x509) in Go 1.27.2.
**Learning:** Standard library CVEs require a go-version bump in both go.mod and CI workflow files.
**Prevention:** Monitor Go releases and keep the toolchain updated.
## 2024-10-21 - Fix Docker Build Failure Due to Ubuntu 26 OverlayFS Bug
**Vulnerability:** Docker build `container-smoke` and `docker` jobs failed on `ubuntu-latest` (migrated to Ubuntu 26) due to an OverlayFS `invalid argument` error during `apk add` in the Alpine container.
**Learning:** `ubuntu-latest` is migrating to Ubuntu 26.04 which has known incompatibilities with current Docker/containerd OverlayFS implementations.
**Prevention:** Pin runner images to `ubuntu-24.04` for stability in Docker build jobs instead of relying on `ubuntu-latest` which may silently upgrade and break builds.
