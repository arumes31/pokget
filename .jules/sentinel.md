## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2024-06-08 - Prevent User Enumeration Timing Attack
**Vulnerability:** The `Login` handler returned immediately on `sql.ErrNoRows`, while valid user logins processed an expensive `bcrypt` comparison.
**Learning:** Returning early on database misses in authentication flows enables attackers to enumerate valid usernames by measuring response times.
**Prevention:** Always perform equivalent cryptographic work (e.g., hashing the password) even when a user is not found, to normalize response times and prevent enumeration.
