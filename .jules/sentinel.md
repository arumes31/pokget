## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2024-07-22 - Prevent Timing Attack in Login User Enumeration
**Vulnerability:** The login endpoint returns early when a user is not found, skipping the expensive bcrypt password hashing. This allows an attacker to enumerate valid user emails through timing attacks.
**Learning:** Early returns on database misses (`sql.ErrNoRows`) in authentication flows cause significant timing discrepancies.
**Prevention:** Always execute the password hashing function against a structurally valid constant dummy hash (with correct bcrypt cost) to ensure constant response times, even for non-existent users.
