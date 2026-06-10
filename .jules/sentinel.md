## 2024-06-10 - Added SecurityHeadersMiddleware
**Vulnerability:** Missing standard HTTP security headers (X-Content-Type-Options, X-Frame-Options, X-XSS-Protection). This could lead to MIME-sniffing attacks, clickjacking, and XSS.
**Learning:** Even internal/personal applications should have basic defense in depth. Go's standard library doesn't add these by default, so a custom middleware is necessary.
**Prevention:** Always implement a security headers middleware early in project development to ensure baseline protection against common web vulnerabilities.
