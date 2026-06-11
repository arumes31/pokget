
## 2024-05-24 - Rate Limiter IP Spoofing via Middleware Ordering
**Vulnerability:** The `RateLimitMiddleware` was placed before `ProxyMiddleware` in the HTTP router (`cmd/pokget/main.go`). This means the rate limiter evaluated the IP address of the reverse proxy, not the actual client IP. An attacker could bypass rate limits by making requests through different proxies or simply overwhelm the application by exhausting the rate limit for the proxy IP, denying service to legitimate users.
**Learning:** Middleware execution order is critical for security controls. Components that rely on accurate client identity (like rate limiters, audit logs, and IP bans) must be placed *after* middleware that resolves the correct client IP.
**Prevention:** Always place proxy/IP resolution middleware at the very beginning of the middleware chain, immediately after logging, to ensure all subsequent security controls operate on the correct client context.
