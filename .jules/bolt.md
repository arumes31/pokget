
## Performance Learnings - 2026-06-04

- **Slice Pre-allocation**: Always pre-allocate slices when the capacity is known beforehand (e.g., when transforming one slice into another of the same size). This reduces GC pressure and avoids expensive slice growth reallocations. In , pre-allocating  improved efficiency during LLM prompt construction.
- **LLM API Abstraction**: Consolidating HTTP logic for LLM queries into a single  method improves maintainability and ensures consistent error handling (like checking HTTP status codes) across all LLM-reliant features.

## Performance Learnings - 2026-06-04

- **Slice Pre-allocation**: Always pre-allocate slices when the capacity is known beforehand (e.g., when transforming one slice into another of the same size). This reduces GC pressure and avoids expensive slice growth reallocations. In `internal/service/llm.go`, pre-allocating `cardNames` improved efficiency during LLM prompt construction.
- **LLM API Abstraction**: Consolidating HTTP logic for LLM queries into a single `queryLLM` method improves maintainability and ensures consistent error handling (like checking HTTP status codes) across all LLM-reliant features.
