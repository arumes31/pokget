## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2026-08-01 - Prevent User Enumeration via Timing Attacks in Login
**Vulnerability:** The `Login` handler returned early when a user was not found (`sql.ErrNoRows`), skipping the expensive bcrypt password hashing. This timing difference allows attackers to enumerate registered users by measuring the response time.
**Learning:** Returning early on database misses in authentication flows introduces timing side channels. While comparing against a hardcoded dummy hash is a common mitigation, it risks mismatched cost factors and reverse timing/DoS attacks if the application's actual hashing cost changes.
**Prevention:** To prevent timing attacks for user enumeration, use the application's actual hashing function (e.g., `_, _ = auth.HashPassword(password)`) on database misses to ensure the timing perfectly matches the current configuration.
