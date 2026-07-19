## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2026-07-19 - Prevent User Enumeration via Timing Attacks in Login
**Vulnerability:** The login endpoint returned immediately when a user was not found in the database (`sql.ErrNoRows`), but took significantly longer (due to bcrypt) when the user existed but provided an incorrect password.
**Learning:** Early returns on database misses during authentication flows enable attackers to enumerate registered users by measuring response times.
**Prevention:** Always ensure the expensive password hashing function is executed even if the user is not found, using a constant dummy hash of the same bcrypt cost factor.
