## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2024-06-12 - Fix User Enumeration Timing Attack
**Vulnerability:** The Login endpoint returned early when a user was not found, resulting in faster response times for non-existent users compared to existing users. This allowed user enumeration via timing attacks.
**Learning:** To mitigate timing attacks, the password hashing function (`bcrypt.CompareHashAndPassword`) must be executed against a structurally valid dummy hash with a matching cost factor even if the user does not exist in the database.
**Prevention:** Ensure constant execution time for authentication flows by avoiding early returns on database misses, and always executing the expensive cryptographic operations against a dummy target.
