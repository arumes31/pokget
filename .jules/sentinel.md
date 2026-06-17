## 2026-06-17 - Fix User Enumeration Timing Attack
**Vulnerability:** The `Login` handler returned early when a user was not found (`sql.ErrNoRows`), bypassing the expensive password hashing step (`bcrypt`). This discrepancy in response time allowed attackers to easily determine if an email address exists in the database.
**Learning:** This is a classic timing attack for user enumeration. When implementing authentication flows, it's crucial to ensure the computational cost of processing a login request is consistent regardless of whether the user exists or not.
**Prevention:** Always execute the password hashing function (e.g., bcrypt) against a constant dummy hash with the same computational cost when a user is not found to ensure constant response times.
