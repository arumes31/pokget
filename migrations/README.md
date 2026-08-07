# Migration recovery policy

Every migration version must be contiguous and must either provide both
`up.sql` and `down.sql`, or be listed in `irreversible.txt` with recovery handled
through PostgreSQL backup and restore.

The historical entries in `irreversible.txt` predate this policy. Adding a fake
or lossy down migration after those migrations have shipped would make rollback
look safe while potentially deleting user data. The container smoke test instead
applies the complete migration chain to a fresh PostgreSQL database, creates a
custom-format dump, restores it into a second database, and compares the schema
and migration version. Schema integrity is checked by comparing normalized
`pg_dump --schema-only --no-owner --no-privileges` output. Each dump is replayed
into an empty scratch database to canonicalize PostgreSQL expressions, then the
per-dump `\restrict` guards are removed before comparison.

New migrations should be reversible. If a new migration genuinely cannot be
reversed, its pull request must add it to `irreversible.txt` and explain the
backup/restore recovery procedure.
