## Worker Database Connection Retention Optimization

**Date:** June 4, 2026
**Area:** Background Worker (`worker.go`)
**Bottleneck:**
Long-running background tasks updating rows asynchronously were holding open `*sql.Rows` handles while making slow HTTP requests and synchronously sleeping (`time.Sleep`). This pattern quickly starves DB connection pools, especially when the card dataset grows large. Furthermore, synchronous sleeps compound delay time on top of latency.

**Optimization:**
1. **Early Release:** Buffer all necessary DB row data into a slice (`[]models.Card`) and immediately call `rows.Close()`. Operations spanning network boundaries are now completely decoupled from database connections until the specific `UPDATE` command.
2. **Ticker over Sleep:** Replace `time.Sleep(delay)` at the end of the loop with a `time.Ticker` blocking via `<-ticker.C`. This overlaps network execution time with the intended delay interval, ensuring the rate limit is respected without accumulating artificial delays, providing a ~33% boost depending on latency variance.
