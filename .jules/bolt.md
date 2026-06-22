## 2026-06-22 - Use time.Ticker instead of time.Sleep in Loops
**Optimization:** Replaced time.Sleep with time.Ticker inside a batch processing loop.
**Why it existed:** The script used `time.Sleep(200 * time.Millisecond)` inside a loop to rate-limit requests to a third-party API. However, this caused the wait time to be sequentially added on top of the processing time, blocking the goroutine and slowing down the script significantly.
**Prevention:** When rate limiting a loop where processing takes time, instantiate a `time.Ticker` before the loop and use `<-ticker.C` inside the loop. This ensures a consistent request rate without unnecessarily delaying the next request if the previous iteration took time to process.
