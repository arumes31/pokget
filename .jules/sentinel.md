## 2024-06-08 - Fix IP Spoofing Default in ProxyMiddleware
**Vulnerability:** The `ProxyMiddleware` incorrectly trusted reverse proxy headers (`X-Forwarded-For`, `CF-Connecting-IP`) by default if the `TRUST_PROXY` and `TRUST_CLOUDFLARE` environment variables were completely missing. This allowed unauthenticated attackers to supply a fake `X-Forwarded-For` header to spoof their IP address, bypassing rate limits and other IP-based security measures.
**Learning:** Checking for `!= "false"` when parsing boolean environment variables inadvertently creates a fail-open, insecure default. When configuring security-sensitive mechanisms (like trusting external IP headers), defaults must always be fail-secure.
**Prevention:** Always use explicit opt-in logic (e.g., `== "true"`) for security features controlled by environment variables. Ensure that when an env var is empty/unset, the application falls back to its safest state.

## 2024-07-12 - Fix User Enumeration Timing Attack in Login
**Vulnerability:** The `Login` handler in `auth_logic.go` returned early when a user was not found (`sql.ErrNoRows`), bypassing the expensive bcrypt password hashing function. This allowed attackers to enumerate valid email addresses by measuring the response time of the login endpoint.
**Learning:** Returning early on database misses in authentication flows creates a timing discrepancy that leaks user existence. For Go's bcrypt, mitigating this requires running the hash check against a constant dummy hash that is structurally valid and has the same cost factor.
**Prevention:** To prevent timing attacks for user enumeration, always execute the password hashing function against a constant dummy hash when the user is not found, ensuring constant response times regardless of user existence.
