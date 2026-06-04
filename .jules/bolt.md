
## Performance Optimization: Worker Pool & Rate Limiting in Price Scraper
- **Date**: 2026-06-04
- **Pattern**: Synchronous sleep in loops blocks overall throughput.
- **Solution**: Replaced `time.Sleep` with a worker pool and `rate.Limiter`.
- **Learning**: Using `limiter.Wait(ctx)` in workers ensures we respect external constraints (scrapers) while allowing the main loop to proceed and other workers to handle non-blocked tasks (like DB updates).
