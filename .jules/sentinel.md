## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2024-08-03 - Fix User Enumeration Timing Attack in Login
**Vulnerability:** The Login handler returned early on database misses (`sql.ErrNoRows`) without performing a password hash, creating a timing difference between valid and invalid usernames. This could allow attackers to enumerate valid email addresses.
**Learning:** Early returns in authentication flows leak state through timing differences. The application's actual hashing function must be used to ensure the timing matches the current configuration.
**Prevention:** Ensure that the computational cost of authentication failures matches successful attempts, for example by executing the hashing function even when a user is not found.
