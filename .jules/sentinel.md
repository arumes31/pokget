## 2025-02-14 - Fix CSRF protection disabled in production
**Vulnerability:** The CSRF middleware had the `Secure(false)` flag hardcoded, disabling the Secure flag on CSRF cookies. This flag is necessary to ensure CSRF cookies are only sent over HTTPS.
**Learning:** Hardcoding configurations that are only appropriate for local development (like disabling the secure flag) into production code can lead to vulnerabilities when deployed.
**Prevention:** Use configuration variables (like `cfg.App.Debug`) to conditionally enable or disable security features so that they are secure by default in production and only disabled in specific development environments.
