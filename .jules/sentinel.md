## 2024-05-24 - Timing Attack in Login Flow
**Vulnerability:** Timing attack possible during login flow when checking for user existence. The application returns early when a user is not found (`sql.ErrNoRows`), making it faster than when hashing a password for a valid user.
**Learning:** Returning early on database misses (like `sql.ErrNoRows`) allows attackers to enumerate registered emails via timing differences.
**Prevention:** Avoid early returns on database misses. Instead of returning early, always perform a dummy password hash computation using the application's actual hashing function (e.g., `_, _ = auth.HashPassword(password)`) to ensure the timing perfectly matches the current configuration and prevents user enumeration.
