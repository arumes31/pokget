## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2024-06-08 - Fix User Enumeration Timing Attack
**Vulnerability:** The login endpoint returned early when a user was not found in the database (sql.ErrNoRows). Since password hashing (bcrypt) takes significantly longer than a database lookup, an attacker could observe response times to determine if an email address exists in the system (user enumeration).
**Learning:** Returning early on database misses in authentication endpoints leaks information through timing side-channels. Password verification functions must always be executed to maintain constant response times.
**Prevention:** Always execute the password hashing function against a structurally valid dummy hash (with the correct cost factor) when a user is not found, ensuring the authentication flow takes roughly the same amount of time regardless of whether the user exists.
