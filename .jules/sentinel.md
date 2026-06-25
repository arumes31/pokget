## 2025-02-14 - Prevent User Enumeration Timing Attack in Login
**Vulnerability:** The login endpoint returned immediately when querying an email that did not exist in the database (`sql.ErrNoRows`).
**Learning:** This early return created a measurable timing discrepancy because valid emails proceed to the computationally expensive `bcrypt.CompareHashAndPassword` check, allowing an attacker to deduce which emails are registered by timing the responses.
**Prevention:** Always perform the password hashing function against a constant, validly-formatted dummy hash (e.g., `$2a$14$Uiy1zBUY3xEMNXLSy8MbZe.8JYnd3DTlZIg6dK/F/5uPiZUHg4VgO`) even when the user is not found to normalize response times.
