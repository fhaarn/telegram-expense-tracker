# Database migrations

Reserved for versioned PostgreSQL SQL migrations. No application schema exists yet.
`make migrate-up` intentionally fails until the migration runner and schema are implemented.
The health-only skeleton requires a reachable empty PostgreSQL database, not tables.

Next: add a migration runner with a database lock, then category, draft, expense,
interaction, and inbound-update tables. Production migrations must complete before
new application code serves traffic and remain compatible with the preceding release.
