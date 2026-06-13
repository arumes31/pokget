## 2024-05-18 - Fix timing attack in user login
**Vulnerability:** Timing attack for user enumeration during login.
**Learning:** The application exited early and avoided the costly `auth.CheckPasswordHash` (bcrypt) call if a user did not exist, making the login process significantly faster for non-existent users (0s) than for existing ones (1.3s-2.6s). This allowed attackers to guess valid emails.
**Prevention:** Always perform the `auth.CheckPasswordHash` even if a user is not found, using a dummy hash with the same bcrypt cost, so the response time remains constant regardless of whether the user exists or not.
