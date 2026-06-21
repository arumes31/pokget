## 2024-05-24 - User Enumeration via Timing Attack
**Vulnerability:** A timing attack in the `Login` handler allowed user enumeration. If an email did not exist, `sql.ErrNoRows` caused an early return. Valid emails required an expensive `bcrypt` hash comparison (cost 14, taking ~2.6 seconds), making it trivial to determine valid emails based on response time.
**Learning:** Returning early on database misses in authentication endpoints bypasses the heavy hashing function, creating a significant timing difference that attackers can measure.
**Prevention:** Always ensure constant response times for authentication endpoints regardless of whether the user exists or not. This is achieved by running the `bcrypt` verification against a structurally valid dummy hash when the user is not found.
