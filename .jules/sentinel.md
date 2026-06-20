## 2026-06-20 - User Enumeration Timing Attack
**Vulnerability:** The Login handler returned early on database misses, creating a timing difference between existing and non-existing users because password hashing was skipped.
**Learning:** Always execute computationally expensive operations (like bcrypt) even on failure paths to ensure constant response times and prevent user enumeration.
**Prevention:** Use a dummy bcrypt hash with the same cost factor when `sql.ErrNoRows` occurs.
