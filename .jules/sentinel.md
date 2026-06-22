## 2026-06-22 - Fix standard library vulnerabilities in govulncheck
**Vulnerability:** The CI pipeline failed due to outdated go version causing `govulncheck` to report standard library vulnerabilities such as `GO-2026-5039` and `GO-2026-5037` in `go1.26.3`. It also failed on `golang.org/x/net` module vulnerabilities.
**Learning:** Hardcoded toolchain and dependency versions can lead to security vulnerabilities when standard libraries are patched. `go.mod` and github action configurations must be updated in tandem when bumping minor runtime versions.
**Prevention:** Avoid statically pinning older versions of standard libraries or core network packages, instead keeping CI versions synchronized with minor updates and regularly checking the vulnerability reports.
