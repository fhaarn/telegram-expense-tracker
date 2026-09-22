# Telegram Expense Tracker

Go + PostgreSQL Telegram expense tracker. [HLD](docs/HLD.md) · [Phase 1 checklist](docs/phase-1-task.md).

## Implemented Phase 1

Private-chat registration, text expenses, category memory and custom categories,
editable drafts, saved-expense edits/deletions, daily/monthly reports, and one-to-one
comparison invites are implemented. Every expense requires Save before it affects reports.

```text
/start → choose your display name
bensin 100k → choose Transport → Save
bensin 25k → Transport is remembered → Save
Nice 8 Ball Cafe 100k → preserves the full café name
```

Only the final token is the amount: digits with optional `k`/`K`, without punctuation
or decimals. IDR values use integer minor units (Rp1 = 100 units). Descriptions are
limited to 200 characters. Category names are 1–40 characters on one line; equivalent
case/whitespace names reuse the existing category. Category learning happens on Save.

Commands: `/start`, `/help`, `/today`, `/month`, `/recent`, `/compare`, `/disconnect`,
and `/cancel`. The recent view offers Edit/Delete; deletion requires confirmation.
Drafts expire after 24 hours. A category correction can apply once or be remembered.
Reports contain confirmed, non-deleted expenses and use the user's stored timezone.

`/compare` creates a 24-hour invite when unpaired; set `TELEGRAM_BOT_USERNAME` (without
`@`) to enable links. New users complete registration before confirming Connect.
Each person can have one partner. Paired `/compare` shares only totals/counts from
the connection's local calendar date onward, including earlier entries on that date.
`/disconnect` requires confirmation and removes access for both people. Already sent
Telegram messages cannot be recalled. No merchant/category details are shared.

Screenshot OCR remains Phase 2: actual representative GoFood screenshots are needed
before implementing a verified template. Images currently receive a coming-later reply.

## Local setup

Requirements: Go 1.26.4+, Docker with a running daemon, and Make.

```sh
cp .env.example .env
# Edit .env: set your development bot token and a random webhook secret.
make db-up
make migrate-up
make run
```

The app, migration command, and webhook-registration command automatically load `.env` from the current working directory. Run them from the project root. Existing environment variables take precedence, even when explicitly empty. A missing `.env` is fine on Render; malformed or unreadable files stop startup with a sanitized error. Restart the command after editing `.env`.
The old `TELEGRAM_ALLOWED_USER_ID` setting is removed: every private-chat sender can
register. Use a separate development bot from production.

Startup checks PostgreSQL, applies migrations under a lock, then starts HTTP and
reply delivery. Ctrl-C/SIGTERM drains HTTP and stops the worker before closing the
pool. No webhook is registered automatically.

- `GET /healthz`: process health, no database query (suitable for frequent hosting probes).
- `GET /readyz`: database readiness with a timeout.
- `POST /telegram/webhook`: secret-header-protected Telegram updates, maximum 1 MiB.

To connect a real bot, expose the application at a public HTTPS address (Render or
a development tunnel), then explicitly register that endpoint:

```sh
export TELEGRAM_WEBHOOK_URL=https://your-host.example/telegram/webhook
make webhook-register
```

The command configures the shared secret, message and callback updates, and one webhook connection
to minimize out-of-order onboarding messages; it preserves pending Telegram updates.
Open the bot in Telegram, tap Start, and enter your name. Repeat with a second account.
Never register a local test endpoint against your production bot.

## Reliability and privacy

Each accepted update commits its deduplication ID, user transition, and queued reply
in one PostgreSQL transaction before HTTP 200. Failures return 503 for Telegram to
retry. The synchronous Phase 1 flow has no separate unfinished input job: it either
commits fully or rolls back. Raw Telegram payloads are not retained.

Replies use a durable outbox, per-user ordering, 120-second leases, and up to five
attempts with backoff. Startup resumes unsent replies. The worker sleeps without
querying the database when idle and wakes on incoming updates. If multiple app
instances are later introduced, add cross-instance wakeup/coordination. Hosting sleep
may delay retries until the next wakeup. A network failure after Telegram accepted a
reply may duplicate the reply, but cannot duplicate confirmed expenses, registration, categories, or pairing.

All intake transactions currently share one advisory lock to keep cross-user pairing
safe; this intentionally targets a small group, not high throughput. Each user has a
60-update/minute limit (one wait reply, then excess updates are acknowledged without
processing), and at most five invites can be created per hour. Limits persist in PostgreSQL.

Successful/failed outbox bodies and keyboards are cleared. Queued pair messages are
suppressed when the relationship is no longer active before claim; an already in-flight
send can still finish after disconnect. Failed rows are available for operational
inspection (`notification_outbox.status = 'failed'`); no automatic infinite retries.
The app logs operation IDs and generic errors, not token values or message bodies.
Remote application database connections require `sslmode=verify-full`.

## Verification

```sh
make test             # All Go unit + integration tests and Python script tests
make test-unit        # Unit tests only; no PostgreSQL/Docker needed
make integration      # Database/Telegram integration packages
make check-fmt vet build
```

`make test` starts its own PostgreSQL 17 container on a random localhost port, waits
for readiness, runs the suite without caching, and removes the container afterward,
including after failure. Start Docker first; `make db-up` is not required for tests.
The test runner does not read `.env` and never uses the application's `DATABASE_URL`.
Python 3 is required for the operation-script unit tests.

If CI or your shell already provides a dedicated database, reuse it without starting
or removing a container:

```sh
TEST_DATABASE_URL='postgres://expense:expense@localhost:5432/expense_tracker_test?sslmode=disable' make test
```

Only supply an isolated test database: integration tests create and drop schemas.
A supplied database is not removed by the runner. CI supplies its PostgreSQL service
to the same `make test` command, so tests are not run twice.

Tests cover final-token parsing and integer limits, onboarding, callback authentication,
owner isolation, category learning/custom categories, stale drafts/mappings/revisions,
expense edits/deletions, report boundaries, invite consent/concurrency/disconnection,
transaction rollback, durable reply recovery, and a fake Telegram delivery path.
No tests send messages to real Telegram accounts.

## Deployment status

Target: Render + Neon. CI runs formatting, vet, race tests, Go build, isolated
PostgreSQL integration tests, and Docker build. The opt-in deployment job uses the
Render API to deploy the tested commit, serializes requests, and checks release
status and application health. See [deployment setup](docs/deployment.md) for the
required GitHub variables/secrets and Render settings.

[Operations](docs/operations.md) covers backup/restore and rollback. Scripts have
been rehearsed with synthetic data in a temporary database; actual off-host backup
storage, scheduling, hosting accounts, production secrets, and live Telegram tests
remain pending. Render Free sleeps; durable data stays in PostgreSQL.

Use feature branch → PR → passing CI → merge to main. No deployment occurs until
explicitly enabled in repository settings. The module name remains
`telegram-expense-tracker`.


Weekly records: connected partners receive emoji alerts when a newly saved expense beats the week's largest single expense in the same category across the pair. Weeks run Monday–Sunday (default Asia/Jakarta), counting from the connection date. The first entry is quiet; ties, edits, and deletions do not send alerts. Partner alerts share the category and amount, never the description. Categories match by normalized name. Historical weeks and future-dated expenses do not trigger alerts.

Weekly record baselines are cached in `weekly_category_records` (migration 005, applied at startup). Ordinary saves use a keyed lookup; missing/date-stale baselines and corrections are rebuilt from expenses. No reset job is required.


Admin API (phase 2): configure a separate `ADMIN_API_KEY` in Render to enable user listing and blacklist/whitelist. Unset disables admin routes while the bot stays available. See [admin API setup and operations](docs/admin-api.md) for requests, access behavior, key rotation, and rollout checks.


### Excel export

Send `/export` for this month's report, or `/export 2026-09-01 2026-09-30` for an inclusive date range. The bot sends an XLSX with date-grouped expenses, total spending, and a category pie chart. There is no expense ID or income section. Category totals are on the supporting Categories sheet.

Limits: 366 days, 2,000 expenses, 5 MB, one request every five minutes, one queued report per user, and 20 queued reports globally. Each report is a snapshot at request time. Edits in Excel do not sync back; request a new export for updated bot data. Data is isolated to the requester; blocked users cannot export. The existing delivery worker handles XLSX attachments and retries. No new environment variables or external services are required; migration 008 runs at startup.
