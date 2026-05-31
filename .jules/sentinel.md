## 2026-05-31 - [Secure Session Cookies]
**Vulnerability:** Session cookies in `internal/handlers/auth_logic.go` were missing the `Secure` flag.
**Learning:** `Secure` flag was omitted for ease of local development, but it's a critical missing configuration for production deployment, rendering session cookies vulnerable to interception over unencrypted HTTP.
**Prevention:** Always ensure `Secure = true` for session cookies, even in local development (as modern browsers treat localhost as secure context anyway) or use environment variables to enforce HTTPS in production.
