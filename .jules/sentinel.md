## 2024-06-19 - Timing Attack in Login Handler
**Vulnerability:** User enumeration timing attack where a missing user returned early before the expensive bcrypt password check.
**Learning:** Returning early on database misses (`sql.ErrNoRows`) allows attackers to determine if an email exists by measuring response times.
**Prevention:** Always execute the password hashing function (e.g., bcrypt) against a constant dummy hash with the same computational cost when a user is not found to ensure constant response times.
