## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.
## 2026-07-18 - Timing Attack in Login User Enumeration
**Vulnerability:** The Login handler returns early (using `sql.ErrNoRows`) without performing a password hash check if the user is not found, leading to timing differences that allow for user enumeration.
**Learning:** Early returns on DB misses before expensive crypto operations (like bcrypt) can leak whether an account exists.
**Prevention:** Always execute a dummy password comparison against a structural dummy hash of the same cost factor when the user isn't found to ensure constant-time responses.
