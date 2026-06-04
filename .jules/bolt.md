# Bolt Performance Journal

## Audit Logging Performance

### Initial Observation
The `AuditService.Log` function was using `db.Exec` directly for every log entry. While simple, it involves repetitive JSON marshaling even when metadata is nil.

### Optimization Strategy
1. **Early Exit for Nil Metadata**: By checking if metadata is nil before calling `json.Marshal`, we avoid the overhead of the marshaler for simple log entries.
2. **Single Responsibility**: Ensuring `Log` handles both marshaling and insertion efficiently within one function call.

### Impact
- Reduced CPU usage for logs without metadata.
- Improved readability and testability of the audit logging path.

### Code Pattern
```go
func (s *AuditService) Log(userID, action string, metadata map[string]interface{}) {
    if metadata == nil {
        s.db.Exec(..., []byte("{}"))
        return
    }
    // ... marshal and exec ...
}
```
