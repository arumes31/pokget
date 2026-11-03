# Agent guide

Pokget is a Go (net/http + gorilla/mux) web app with server-rendered
`html/template` views, vanilla JS assets in `static/`, and Alpine/HTMX on the
client. CI runs Go tests, Playwright-less chromedp UI tests, Node asset tests,
gosec/govulncheck, container smoke tests and publishes the image to GHCR. The
production host pulls that image (`dr-dnode01`).

## Local toolchain

- Go is not always on `PATH`; on the Windows build machine it lives in
  `D:\BUILD\_gotool\go\bin\go.exe`. Use `CGO_ENABLED=0` for handler/UI tests.
- Node tooling (`npm run build:static`, `npm run test:static`) is used for
  minification and the JavaScript specs; Docker `node:26-alpine` works too.
  On the Windows build machine a portable Node lives in `D:\BUILD\_nodetool`
  (`node.exe`, `npm.cmd`).
- chromedp UI tests need a browser: set `POKGET_CHROME_PATH` (Edge/Chrome).
- `graphify hook install` pins the absolute interpreter path in `.git/hooks/*`.
  Re-run it on a new machine/venv, otherwise commits print
  "could not locate a Python with graphify installed".

## Pull request workflow

1. Work on `test`, run the relevant suites locally, then push and open a PR
   against `main`. `test` is deleted on merge — restore it afterwards
   (`git push origin test`).
2. Wait until CI is green (ignore `Strix (advisory)` and CodeRabbit for the
   merge gate, but see the next point).
3. **CodeRabbit review**: this repository gets no automatic CodeRabbit reviews
   (the OSS plan needs >= 10 stars). Request one explicitly with
   `node scripts/coderabbit-review.mjs --pr <number> --wait` while the hourly
   free quota is free (currently 3 reviews/hour). The script skips drafts,
   closed PRs, already reviewed heads, duplicate requests, and PRs whose newest
   CodeRabbit comment reports an exhausted quota; `--wait` blocks until
   CodeRabbit answered for the current head commit (exit 3 on timeout, 1 on
   requested changes). CodeRabbit refuses to review a closed PR, so the review
   must be requested **before** merging, and its blocking findings must be fixed
   before the merge.
4. Merge with `gh pr merge <pr> --merge`, then let the `main` pipeline build
   and publish the image before deploying it.
5. Verify the deploy: `/health/ready` returns 204, container is `healthy` with
   `0 restarts`, `docker logs` shows no `level=ERROR` and no `status=5xx`.

## Market prices

Price coverage is measured, not assumed: `go run ./cmd/pricegaps` prints it per
game/language/rarity and `-fail-under-usd 80 -fail-under-eur 80` turns the report
into a gate (exit 2). `-sources` additionally probes every TCGCSV category.

Sources per game: Scryfall (Magic, USD+EUR incl. foil/etched), YGOPRODeck
(Yu-Gi-Oh), TCGdex (Pokémon per card, Cardmarket EUR + TCGplayer USD) and TCGCSV
(the TCGplayer categories listed in `internal/service/price_bulk.go`). EUR for
TCGplayer-only rows is derived from the daily ECB rate and flagged with
`cards.price_eur_estimated`. `altered` and `duel_masters` have no marketplace with
public prices.

Matching rules in `internal/worker/price_match.go` (both sides run the same
normalizer, so never key raw strings):

- set code + collector number, normalized (leading zeros, set code inside the
  number, rarity suffix, print run, separators),
- set name + number for games whose set codes disagree,
- card name inside the set for products without a collector number (WoW, Sorcery),
- composite number alone for numbers that carry the set code (Weiss uses `NIK/`
  while the number says `NIK/S135-E063`),
- language-bound categories (`ja`, `ko`, `zh-cn`, `zh-tw` share the Japanese
  Pokémon set codes) run before the language-agnostic pass; the generic pass skips
  those languages.

Bulk rules: a source never wipes a currency it does not know, the fallback only
fills cards without a price, and an upsert batch retries after `40P01`/`40001`/
`55P03` because the price worker writes the same rows.

Refresh cadence (`internal/worker/price_sync.go`): the bulk exports run at startup
and every `PRICE_BULK_INTERVAL_HOURS` (default 24 h), the tracked-card sync runs
every `PRICE_SYNC_INTERVAL_MINUTES` (default 60 min) with `PRICE_TRACKED_LIMIT`
cards per cycle split across the collectors that track cards, and the per-card
backfill runs with the price cycle. `cards.last_updated` means "the price changed
at"; `cards.price_checked_at` means "a source verified it at" — show the second one
to users, otherwise a stable price looks stale.

## Conventions

- Static assets referenced from templates carry `?v={{ .BuildVersion }}`;
  `TestTemplatesVersionEveryStaticAsset` fails otherwise.
- Static, updatable files must stay cache-safe; the service worker cache name
  embeds the build version and is registered as `/sw.js?v=<build>`.
- Scanner styles live in `static/css/styles.css`; the HUD accent is driven by
  `--tcg-accent` (one `[data-game]` palette per TCG).
- Keep user-visible strings translatable: `data-i18n` in templates, German
  entries in `static/js/i18n.js`; `npm run test:static` enforces coverage.
