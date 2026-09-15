# PostgreSQL migrations

`001_onboarding.sql` adds users, per-user categories, pending name interactions,
processed update IDs, and a durable reply outbox. Expense and comparison tables
will arrive in subsequent migrations.

Run `make migrate-up` with `DATABASE_URL` exported. Application startup also applies
pending migrations before serving HTTP. SQL is embedded into the executable.
The runner uses a PostgreSQL transaction-scoped advisory lock, SHA-256 checksums,
and an atomic transaction. Re-running is safe; editing an applied migration fails.
Add a new numbered migration instead. Do not reset a production database to resolve
a checksum failure. There is no automatic down migration: use forward fixes and
keep future migrations compatible with the previous release.
