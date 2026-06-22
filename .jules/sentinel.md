## 2026-06-22 - Unsafe Template Execution Vulnerability
**Vulnerability:** In `internal/handlers/handlers.go`, the `ExecuteTemplate` method received its `name` parameter directly without validation or allowlisting.
**Learning:** Even though most usages in the codebase were safe static strings, passing unverified template names can open the door for template injection or path traversal if user input is ever used for rendering template dynamically.
**Prevention:** Always validate or allowlist template names against a known safe list or predefined parsed template registry, for instance checking `h.Templates.Lookup(name) != nil` before executing it.
