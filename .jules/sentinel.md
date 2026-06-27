## 2025-01-20 - User Enumeration via Timing Attack in Login

**Vulnerability:** The `Login` handler returned early if a user was not found in the database, skipping the expensive password hashing process. This allowed an attacker to enumerate valid email addresses based on response times, as valid users trigger an expensive `bcrypt` comparison.
**Learning:** Early returning on `sql.ErrNoRows` in authentication endpoints inadvertently leaks the existence of users via timing variations. The application must perform an equivalent amount of work in both success and failure cases to maintain constant-time responses.
**Prevention:** For authentication endpoints, ensure a dummy `bcrypt` check is performed (using a valid structural dummy hash matching the cost factor) when a user does not exist. This normalizes response times and mitigates user enumeration timing attacks.
