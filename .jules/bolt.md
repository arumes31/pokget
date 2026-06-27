## 2024-05-30 - Overlapping Rate Limit Delays with Processing
**Learning:** In worker loops (`internal/worker/price_sync.go`), putting a synchronous `time.Sleep()` at the end of the loop adds the sleep duration strictly sequentially to the processing time. For large batches, this compounds significantly.
**Action:** Use a `time.Ticker` initialized before the loop and wait on `<-ticker.C` inside the loop. This allows network, database, or compute times to overlap with the rate limit wait, improving overall throughput while remaining respectful to rate limits.
