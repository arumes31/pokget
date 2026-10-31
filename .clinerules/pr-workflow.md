# PR workflow rules (Cline memory)

These rules mirror `AGENTS.md`; keep both in sync if the workflow changes.

- Branch `test` → PR against `main`; restore `test` after the merge.
- Merge only when CI is green (`Strix (advisory)` is informational).
- **Before merging, request a CodeRabbit review** while the hourly free quota is
  available (3 reviews/hour on the OSS plan):
  `node scripts/coderabbit-review.mjs --pr <number>`
  The script skips drafts/closed/merged PRs, heads that were already reviewed,
  duplicate requests and quota-exhausted PRs. Wait for the review, then fix
  blocking findings.
- Never merge with pending `fail` checks and never force-push `main`.
- After the main pipeline publishes the image, deploy it and verify
  `/health/ready` (204), `0 restarts`, `healthy`, no `level=ERROR`/`status=5xx`.
