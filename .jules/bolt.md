# Bolt Performance Journal

## 2026-06-04: Mail Template Pre-parsing
- **Problem**: Confirmation email templates were being parsed on every `SendConfirmationEmail` call.
- **Optimization**: Pre-parsed the template during `MailService` initialization and stored it in the struct.
- **Impact**: Reduced CPU cycles and memory allocations per email sent. While the impact per call is small (template parsing is relatively fast for small templates), it eliminates redundant work in a hot path for user registration/verification.
- **Pattern**: Always pre-parse static templates during service initialization.
