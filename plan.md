1. **Fix timing attack in `Login` handler**:
   Modify `internal/handlers/auth_logic.go` to compute a dummy hash using `auth.HashPassword(password)` when a user is not found (`sql.ErrNoRows`). This ensures the time taken to process the login request is roughly the same whether the user exists or not, preventing user enumeration via timing attack.
2. **Update Sentinel Journal**:
   Append a critical learning entry to `.jules/sentinel.md` documenting the timing attack vulnerability, learning, and prevention strategy.
3. **Run tests**:
   Execute `SESSION_KEY=12345678901234567890123456789012 go test ./...` and `pnpm lint`, `pnpm test` to verify changes.
4. **Complete pre-commit steps to ensure proper testing, verification, review, and reflection are done.**
5. **Submit the PR**:
   Use `submit` to create the pull request titled "🛡️ Sentinel: [MEDIUM] Prevent user enumeration via timing attack in Login" with the required description sections.
