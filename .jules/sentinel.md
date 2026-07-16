## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.
## 2024-05-18 - User Enumeration via Timing Attack in Login
**Vulnerability:** Login endpoint returned instantly when the email did not exist in the database, allowing attackers to enumerate valid users via timing attacks.
**Learning:** `sql.ErrNoRows` handlers that return immediately bypass the costly `bcrypt` hash comparison which is otherwise performed on valid users.
**Prevention:** Always perform a dummy `bcrypt` check against a constant hash with the appropriate cost factor (e.g., cost 14) in the `sql.ErrNoRows` case to ensure constant response times.
