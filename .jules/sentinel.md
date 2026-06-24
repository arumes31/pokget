## 2026-06-24 - Vulnerabilities in golang.org/x/net
**Vulnerability:** Known CVEs in golang.org/x/net@v0.54.0 affecting html and idna handling (GO-2026-5030, GO-2026-5029, GO-2026-5028, GO-2026-5027, GO-2026-5026, GO-2026-5025).
**Learning:** Outdated dependencies expose the application to XSS and DoS vulnerabilities when processing untrusted HTML.
**Prevention:** Regularly scan dependencies with govulncheck and update them to patched versions (e.g., v0.55.0).
