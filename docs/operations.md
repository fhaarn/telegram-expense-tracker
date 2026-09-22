# Database backup and restore

Use Python 3 and PostgreSQL 17 or newer client tools compatible with the database server. The
scripts are manual operations; no scheduler or production backup location is configured.
Do not run them with real account credentials until the destination is checked.

## Backup

Export `DATABASE_URL` through your secret manager or shell, then run:

```sh
scripts/backup.sh /secure/off-host-location/expense-tracker.dump
```

The script creates a consistent custom-format `pg_dump` archive, validates that it is
readable, restricts file permissions, and refuses to overwrite an existing backup.
Store backups encrypted with access limited to the owner; they contain personal data.
A backup on Render's ephemeral filesystem is not durable. Set an off-host schedule
and retention policy before production use; this remains an external setup task.

## Restore rehearsal

Create a separate empty database. Never use the current production database as target.
Export its URL as `RESTORE_DATABASE_URL`, then run:

```sh
CONFIRM_RESTORE=empty-database scripts/restore.sh /secure/path/expense-tracker.dump
```

The script refuses a target containing tables/views/sequences and restores in a
single transaction. Keep the target disconnected from application writers during the
check/restore operation. Verify schema migration versions, user/category/expense counts,
relationship memberships, and report totals before considering any production cutover.
A full backup includes pending replies; disable real Telegram delivery during a restore
rehearsal to avoid sending old notifications. Use fake credentials or the integration
suite's fake sender rather than starting a production-token bot against the restored DB.

Restore verification against a temporary test database can validate the mechanics;
actual off-host backup storage, scheduling, and production recovery drills remain pending.

## Recovery

- Deployment failure: inspect CI/Render status; no automatic destructive schema rollback.
- Database outage: webhook persistence returns an error for Telegram retries.
- Unsent replies: outbox recovers on restart; inspect terminal failures without printing bodies.
- Secret rotation: update the host environment; re-register the webhook for a webhook-secret change.
- Never paste production database URLs or bot tokens into issues or logs.

## Repository verification

The utilities were rehearsed against disposable PostgreSQL 17 databases with a synthetic record. The archive was restored into a separate empty database and the restored value was verified. This does not configure production storage or a scheduled backup.


Admin API (phase 2): configure a separate `ADMIN_API_KEY` in Render to enable user listing and blacklist/whitelist. Unset disables admin routes while the bot stays available. See [admin API setup and operations](admin-api.md) for requests, access behavior, key rotation, and rollout checks.


### Excel exports

Export snapshots are temporarily persisted in `notification_outbox.export_payload`, then cleared on success, terminal failure, or blacklist suppression. Treat pending payloads/backups as private expense data. Existing worker startup recovery handles queued exports. Generation is serial, with no permanent local files. Document sends have a 60-second context and claims have a 120-second lease. An uncertain Telegram response can cause a duplicate file on retry. If delivery repeatedly fails, inspect sanitized delivery IDs/attempt counts, not payload contents. No new deployment secret is needed.
