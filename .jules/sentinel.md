## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2024-10-27 - Fix Timing Attack in Login Handler
**Vulnerability:** The `Login` handler returned early on a database miss (`sql.ErrNoRows`), allowing attackers to enumerate users by measuring response times since the expensive `bcrypt.CompareHashAndPassword` was not executed.
**Learning:** Early returns in authentication flows, specifically around database lookups before cryptographic operations, introduce timing side channels.
**Prevention:** Always execute the cryptographic operations (like password hashing comparison) with a dummy value when the user is not found to ensure constant response times. The dummy hash must be a structurally valid bcrypt hash with a matching cost factor (e.g., `$2a$14$...`).
