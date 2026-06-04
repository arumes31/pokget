# Bolt Performance Journal

## Critical Performance Learnings

### [2026-06-04] Scraper Client Optimization
- **Observation:** Creating a new user agent slice on every `FetchPrice` call in `ScraperPriceClient` led to unnecessary allocations.
- **Optimization:** Moved the `userAgents` slice to a package-level variable `scraperUserAgents`.
- **Impact:** Reduced per-call memory pressure and allocation overhead.
- **Codebase Pattern:** Centralize static configuration and rotation pools in service-level globals when they don't depend on instance state.
