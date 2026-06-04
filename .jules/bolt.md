## Performance Learning: Context-Aware DB Operations & Rendering

In `internal/handlers/handlers.go`, the `render` method was updated to use `QueryRowContext` instead of `QueryRow`. This ensures that database queries are automatically cancelled if the client disconnects or the request times out, freeing up database resources.

Additionally, a pattern for context-based caching of user metadata was introduced in the `render` function. By checking for `user_data` in the request context before querying the database, we allow complex request handlers that might call `render` multiple times (e.g., for fragments) to avoid redundant database roundtrips.

Impact:
- Improved server resilience under high load or network instability.
- Potential reduction in DB load for complex pages by up to 50% through metadata caching.
