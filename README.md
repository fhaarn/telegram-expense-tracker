# Telegram Expense Tracker

Personal expense tracker in Go. Design: [HLD](docs/HLD.md). Implementation checklist: [Phase 1 tasks](docs/phase-1-task.md).

## Current status

Runnable infrastructure skeleton: validated configuration, PostgreSQL connection pool,
HTTP health/readiness endpoints, graceful shutdown, Docker packaging, and GitHub CI.
Telegram SDK construction is available but not connected to the server. Expense parsing,
categories, reports, database schema, webhook intake, and deployment are not implemented.
No Telegram webhook is registered or acknowledged by this skeleton.

## Local development

Requirements: Go 1.26.4+, Docker with its daemon running, and Make.
The module name is locally `telegram-expense-tracker`; replace it and internal import
prefixes with the actual GitHub module path once the repository owner is chosen.

```sh
cp .env.example .env
make db-up
set -a
. ./.env
set +a
make run
```

The app reads exported environment variables; it does not automatically load `.env`.
The example token/owner/secret are placeholders. They allow health-only local startup;
replace them with a separate development bot's settings before implementing intake.

```sh
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

`/healthz` checks the process without querying PostgreSQL. `/readyz` verifies a database
connection with a timeout. Use `/healthz` for frequent hosting health checks to avoid
continually waking a sleeping Neon database. Startup checks database connectivity.
Ctrl-C or SIGTERM shuts down HTTP before closing the pool.

```sh
make check-fmt vet test build
TEST_DATABASE_URL="$DATABASE_URL" make integration
```

Integration tests currently perform a connection/SELECT check only. Use a dedicated
test database as tests expand. `make db-down` stops PostgreSQL while retaining its
named volume. `make migrate-up` intentionally reports that migrations are pending;
see [migrations](migrations/README.md).

## Deployment preparation

Target: Render web service + Neon PostgreSQL, using Telegram webhooks. Remote database
URLs must use `sslmode=verify-full`. Set secrets through Render environment settings;
never commit a real token, database password, webhook secret, or `.env` file.

```sh
docker build -t telegram-expense-tracker .
```

Run the image with environment variables and a reachable database. `localhost` inside
a container is that container, so the local host database URL must be adjusted.

CI runs on pull requests targeting main and pushes to main. It checks formatting,
vet, race-enabled tests, Go build, a temporary PostgreSQL integration check, and Docker
build. Actual deploy hooks, migration automation, webhook registration, and hosting
accounts are still pending; no automatic deployment is enabled yet.

Use feature branch → pull request → passing CI → merge to main. The future deployment
job will deploy the tested revision after main checks pass. Render Free can sleep and
lose local files; all durable state belongs in PostgreSQL. Database backups and a
restore procedure are required before using this for real expense history.
