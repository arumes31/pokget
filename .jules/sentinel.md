
## 2024-05-18 - Hardcoded Default Session Key Fallback
**Vulnerability:** A hardcoded default string ("temporary-insecure-dev-key-32-chars-long") was used as the session secret if `SESSION_KEY` wasn't set in environment variables.
**Learning:** Hardcoded default secrets are a significant risk. If the application is deployed without `SESSION_KEY` being configured properly, anyone inspecting the source code can forge session cookies to authenticate as any user. The fallback existed to simplify local development, but sacrificed production security.
**Prevention:** Instead of using a static string as a fallback, generate a cryptographically secure random string at startup (using `crypto/rand`). This maintains developer convenience (the app starts without config) but ensures that every instance has a unique, unguessable key, preventing session forgery even if the configuration step is missed.
