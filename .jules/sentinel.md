## 2025-06-07 - Insecure Defaults: CSRF & Reverse Proxy
**Vulnerability:**
1. CSRF cookie `Secure` flag was hardcoded to `false`, allowing CSRF tokens to be transmitted over unencrypted HTTP.
2. IP spoofing via `X-Forwarded-For` and `CF-Connecting-IP` headers because reverse proxy trust was enabled by default.

**Learning:**
The application suffered from insecure defaults that prioritized local development ease (`csrf.Secure(false)`, implicit trust of `X-Forwarded-For`) over production security. This led to explicit vulnerabilities where rate-limiting could be completely bypassed via IP spoofing out-of-the-box, and CSRF cookies could be intercepted.

**Prevention:**
Always apply the principle of "fail-secure". Production security measures (like secure cookies and untrusted proxies) should be the default, requiring explicit configuration flags (e.g. `DEBUG=true` or `TRUST_PROXY=true`) to weaken security constraints.
