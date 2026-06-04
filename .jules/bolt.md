# Bolt Performance Journal

## [2026-06-04] Context-aware Database Operations in Handlers

### Learning
Using `QueryContext` and `ExecContext` with the incoming request's context (`r.Context()`) is critical for performance and resource management in Go web services.

### Architectural Bottleneck
Without context-aware DB calls, if a client cancels a request or disconnects, the database query continues to run until completion on the database server. In high-traffic scenarios or with long-running queries, this leads to:
1.  **Resource Leakage**: CPU and memory on the DB server are wasted on results that will never be consumed.
2.  **Connection Pool Exhaustion**: Connections remain busy longer than necessary.

### Pattern
Always prefer `*Context` variants of database methods in HTTP handlers. This ensures that the `database/sql` driver can send a cancellation signal to the database engine as soon as the HTTP request is closed.

### Impact in Pokget Vault
Implemented in `internal/handlers/errors.go`. While these specific queries are likely fast, establishing this pattern early prevents "zombie queries" from accumulating during high load.
