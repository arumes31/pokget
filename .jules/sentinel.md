## 2026-06-12 - Fix ProxyMiddleware Order for Rate Limiting
**Vulnerability:** Rate limiting bypass/denial of service. `auth.RateLimitMiddleware` was applied before `auth.ProxyMiddleware`.
**Learning:** In Go HTTP server setups behind reverse proxies, rate limiting middleware must execute *after* proxy middleware extracts the real client IP (e.g. from `X-Forwarded-For`). Executing it first means the rate limiter acts on the proxy's IP, leading to either a DoS for all users (if the proxy hits the limit) or a complete bypass if users can cycle their IPs.
**Prevention:** Always verify the order of middleware registration. Security components like rate limiters and audit loggers that depend on the client's identity/IP should be registered after middleware that resolves those properties.
