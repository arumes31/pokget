## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2024-05-18 - Proxy Middleware Ordering Vulnerability
**Vulnerability:** The rate limiting middleware was placed before the proxy middleware in the request pipeline.
**Learning:** Because the rate limiter ran first, it applied rate limits based on the reverse proxy's IP address (e.g., a Cloudflare node or load balancer) rather than the actual client IP. This allowed attackers to bypass the rate limit if they routed their requests through multiple proxy nodes, or conversely, could block legitimate users if they shared a proxy IP with an attacker.
**Prevention:** Always ensure that `auth.ProxyMiddleware` is applied *before* `auth.RateLimitMiddleware` in the HTTP router configuration so that the rate limiter evaluates the real client IP.
