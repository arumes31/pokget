## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.
## 2024-06-26 - User Enumeration via Timing Attack
**Vulnerability:** The login endpoint was vulnerable to user enumeration via timing attack. It returned immediately when an email was not found in the database, bypassing the expensive bcrypt password check.
**Learning:** Early returns on database misses (`sql.ErrNoRows`) in authentication flows allow attackers to measure response times and determine if an account exists.
**Prevention:** Always execute the password hashing function (or a dummy hash with the same work factor) against a structurally valid dummy string to ensure constant response times, even on database misses.
