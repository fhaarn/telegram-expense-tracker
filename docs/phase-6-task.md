# Phase 6 tasks — Bulk expenses and admin announcements

- [x] Add durable batch and announcement migrations.
- [x] Parse up to 20 expense lines and report invalid line numbers.
- [x] Reuse remembered categories; prompt for unknowns and support custom categories.
- [x] Ask for a shared date after categorization; support review, edit, cancel, expiry and restart.
- [x] Save expenses and mappings atomically; reject stale/cross-user callbacks and duplicate saves.
- [x] Maintain weekly records and coalesce batch alerts.
- [x] Add broadcast preview, submission and status endpoints using the existing admin API key.
- [x] Persist recipient snapshots, idempotency, retry/lease recovery and delivery progress.
- [x] Pace announcements, honor Telegram retry delays and prioritize normal replies.
- [x] Suppress blocked users and cancel their unfinished batches.
- [x] Add unit/integration coverage and run the complete test suite, formatting, vet and build.
- [x] Update HLD, admin API documentation and interaction report.
- [ ] Verify on the deployed bot after deployment (no real announcements during development).
