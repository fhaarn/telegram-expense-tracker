# Phase 1 Tasks — Telegram Expense Tracker

Goal: deploy a multi-user Telegram bot that records text expenses, remembers categories, supports corrections, and provides daily/monthly reports.

All checkboxes represent implementation work; creating this checklist does not mark any feature complete.

## Technical baseline

Use Go, PostgreSQL (local Docker for development, Neon for deployment), Render web hosting, and GitHub Actions. Use Telegram webhooks in deployment; local long polling may use a separate development bot. The [HLD](HLD.md) reflects this deployment baseline. The repository implements infrastructure, onboarding, text expenses, categories, reports, and pairing. External deployment configuration and live verification remain pending.

Defaults: IDR and Asia/Jakarta. Phase 2 OCR, GoFood screenshot parsing, CSV export, budgets, and reminders are outside this checklist.

## 1. Project setup

- [x] Reconcile the HLD with PostgreSQL, Neon, Render webhooks, and phase 1 deployment; remove obsolete SQLite-specific operations.
- [x] Initialize the Go module and application entry point with Telegram, expense, parsing, storage, and reporting packages.
- [x] Add Telegram and PostgreSQL libraries; keep parsing and label helpers independent. Transactional application rules currently live in internal/postgres; Telegram remains a transport adapter.
- [x] Add `.gitignore`, `.env.example`, and validated configuration for bot token, database URL, timezone, currency, HTTP port, and webhook secret.
- [x] Add local PostgreSQL Compose configuration and document database startup, application startup, and test commands.
- [x] Implement and document the migration runner (`make migrate-up`) with locking, checksums, and startup application.
- [x] Verify PostgreSQL startup/connectivity in an isolated Docker container and build the deployment image.
- [x] Add a Dockerfile for deployment and an HTTP health endpoint; listen on Render's configured port.
- [x] Development bot creation/token entry reported complete by the user; multi-user onboarding uses sender identity rather than a configured owner ID.
- [ ] Configure a separate production bot token when deploying.

## 2. Database and storage

- [x] Add versioned PostgreSQL migrations for categories, category mappings, drafts, expenses, inbound updates, and active user interactions.
- [x] Add owner-consistent references and uniqueness constraints for update IDs, originating draft IDs, normalized category names, and description mappings.
- [x] Seed Food & drinks, Transport, Groceries, Shopping, Bills, Entertainment, Health, and Other without duplicating them on restart.
- [x] Store amounts with checked integer arithmetic and one documented IDR scale; never use floating-point money calculations.
- [x] Store expense dates as local calendar dates and operational timestamps in UTC.
- [x] Implement transactional confirmation, revision/version checks, deletion, and remembered-category updates.
- [x] Configure a small connection pool, query timeouts, and TLS for the hosted database.

## 3. Telegram intake and reliability

- [x] Implement `/start` and `/help` with text examples and available commands.
- [x] Require private chats and completed registration for expense operations; authorize each message/callback by its Telegram sender and record owner.
- [x] Implement the deployment webhook endpoint with Telegram secret-header verification and request-size limits.
- [x] Persist authorized updates before acknowledging webhook delivery; deduplicate repeated deliveries.
- [x] Atomically commit each synchronous input transition and reply; return 503 on rollback for Telegram retries. Recover queued outbound replies after restart (no separate inbound worker is needed).
- [x] Send actionable errors for invalid input and temporary failures; keep secrets and expense content out of logs.
- [x] Handle shutdown cleanly and preserve pending work. Avoid aggressive idle database polling that prevents Neon from sleeping.
- [x] Explain that screenshot support is coming in phase 2 when an image is submitted.

## 4. Text reader

- [x] Trim outer whitespace and treat only the final whitespace-delimited token as the amount candidate.
- [x] Accept digits with optional `k`/`K`; preserve the entire preceding description, including numbers.
- [x] Reject missing descriptions, invalid suffixes, zero/negative amounts, unsupported punctuation, and integer overflow.
- [x] Ask for correction if the final token is invalid; never search earlier numbers for a fallback amount.
- [x] Preserve the display description and separately normalize its category key by lowercasing and collapsing whitespace.
- [x] Default the expense date to today in Asia/Jakarta and show the date in the preview.

## 5. Category selection and learning

- [x] Show a paginated category picker for an unknown description, including `+ New category`.
- [x] Prefill the remembered category for a known normalized description, regardless of amount.
- [x] Stage a new description/category mapping and persist it only when the expense is saved.
- [x] Let category corrections apply to `This expense only` or `Remember for <description>`.
- [x] Re-run lookup after a description edit and discard the previous description's staged learning action.
- [x] Keep historical expenses and already-visible pending drafts unchanged when a mapping changes.
- [x] Detect stale mapping changes so one pending draft cannot silently overwrite a newer remembered choice.

## 6. Custom categories

- [x] Add `+ New category` to every category picker, including expense-edit flows.
- [x] Persist a category-name interaction so replies are treated as names instead of new expenses, including after restart.
- [x] Validate a nonempty single-line name of at most 40 Unicode characters; normalize whitespace and reject control characters.
- [x] Reuse an existing category for case/whitespace-equivalent names rather than creating duplicates.
- [x] Create the category and select it on the originating draft, then return to the expense preview.
- [x] Make `/cancel` during name entry return to the picker without creating a category.
- [x] Retain an already-created category if its expense is cancelled, but save no expense or remembered mapping.
- [x] Include custom categories in reports; omit categories without confirmed expenses for the selected period.

## 7. Draft review and expense management

- [x] Show description, formatted amount, category, and date with Save, Edit, and Cancel buttons.
- [x] Require a valid amount, description, date, and category before Save.
- [x] Support editing description, amount, category, and date with explicit prompts.
- [x] Keep only one active free-text interaction per user and identify its originating draft.
- [x] Save an expense exactly once and return the existing result for repeated Save callbacks.
- [x] Persist pending/confirmed/cancelled/expired draft states and enforce the initial 24-hour expiry.
- [x] Validate callback ownership, draft state, and version before mutation.
- [x] Implement `/recent` with paginated records and record-specific Edit/Delete buttons.
- [x] Confirm saved-expense revisions before applying them; prevent stale revisions overwriting newer changes.
- [x] Require confirmation before deleting a saved expense and exclude deleted records from reports.

## 8. Reports

- [x] Implement `/today`: confirmed expense list, total, and category breakdown for the current local date.
- [x] Implement `/month`: total, expense count, and category breakdown for the current local calendar month.
- [x] Format IDR amounts consistently and order report entries predictably.
- [x] Handle empty periods with a friendly message and zero total.
- [x] Paginate or split long responses to fit Telegram limits.
- [x] Reflect confirmed edits/deletions and include custom categories; exclude drafts, cancelled entries, and deleted expenses.

## 9. Focused verification

- [x] Test `Tahu Telor 20k` → description `Tahu Telor`, amount Rp20,000.
- [x] Test `Nice 8 Ball Cafe 100k` → description `Nice 8 Ball Cafe`, amount Rp100,000.
- [x] Test uppercase suffixes, extra whitespace, invalid final tokens, missing fields, and overflow.
- [x] Test saving `bensin 100k` as Transport, then prefilling Transport for `bensin 25k`, including after restart.
- [x] Test custom Fitness creation for `gym 150k`, reuse for `gym 200k`, and inclusion in reports.
- [x] Test duplicate category names, cancelled category entry, cancelled expenses, and one-off category overrides.
- [x] Test duplicate webhook delivery and Save callbacks, stale revisions/mappings, and transaction rollback against PostgreSQL.
- [x] Test per-user ownership isolation and webhook-secret rejection.
- [x] Test report totals across local midnight/month boundaries, custom categories, edits, deletions, and empty periods.
- [x] Test restart recovery of drafts, category-name interactions, and pending inbound work.

## 10. GitHub Actions and deployment

- [x] Verify the local GitHub repository/remote and document the feature-branch → pull-request → main workflow.
- [ ] Commit and push the completed implementation when requested.
- [x] Add GitHub Actions checks for pull requests and pushes to `main`: formatting check, `go vet`, tests, and build validation.
- [x] Use standard Linux runners and a temporary PostgreSQL service for integration tests; never use the production database for CI tests.
- [ ] Require successful checks before merging where repository settings support it, and prevent routine direct pushes to `main`.
- [ ] Disable Render auto-deploy and configure the opt-in deployment settings; verify one real release.
- [ ] Create the Neon database and Render web service connected to this repository.
- [ ] Store production secrets in Render and the Render API key in GitHub Actions secrets; configure service ID/health URL and opt-in deployment variable.
- [x] Implement opt-in deployment after successful main checks; deploy exact tested SHA through the Render API.
- [x] Implement tested-SHA deployment and serialized requests, with mocked regression tests.
- [x] Run migrations safely with locking before serving the new version; fail deployment on migration errors and keep changes compatible with the prior version during rollout.
- [x] Implement release status and health verification; a successful API request alone is not a successful release.
- [ ] Register Telegram's production webhook and verify delivery, retries, and duplicate protection.
- [ ] Check free-tier allowances at setup and monitor GitHub minutes, Render builds/usage, and Neon storage/compute.
- [ ] Exercise Render sleep/wake behavior and document the delayed first reply.
- [ ] Set up a consistent database backup outside the application filesystem and verify a restore.
- [x] Document deployment, secrets setup, troubleshooting, backup/restore, and rollback; note that code rollback does not undo database migrations.

## 11. Multi-user onboarding and one-to-one comparison (new scope)

Registration, pairing, expense authorization, and comparison tests are implemented; shared-hosting usage measurement remains pending.

- [x] Replace required `TELEGRAM_ALLOWED_USER_ID` configuration with registered-user identity derived from Telegram sender IDs; update configuration tests, environment example, and README.
- [x] Add users with unique Telegram numeric ID, private chat ID, optional username, display name, status, timezone, and currency.
- [x] Implement idempotent `/start` → “Who are you?” onboarding and validated name entry; seed categories per user.
- [x] Preserve invitation context through onboarding/restart and validate it again before acceptance.
- [x] Scope all existing/future queries, category mappings, interactions, and callback mutations by authenticated internal user ID.
- [x] Add invite, pair, active-membership, and notification-outbox migrations with ownership and uniqueness constraints.
- [x] Generate 16 secure random bytes as 32 hex characters; store a digest and emit `https://t.me/<bot_username>?start=compare_<token>`.
- [x] Add `/compare` invitation creation/rotation, 24-hour expiry, cancellation, and Connect/Cancel acceptance explaining the shared totals/counts.
- [x] Atomically pair two distinct unpaired registered users, consume the invite, revoke obsolete invitations, and enqueue both requested notifications.
- [x] Enforce one partner per user under concurrent acceptance; handle self-invites, invalid/expired/reused tokens, and already-paired users.
- [x] Deliver “You're Battling {A} Now!” to the joiner and “{B} is your partner in crime now!” to the inviter with durable retries.
- [x] Add paired `/compare` daily/monthly aggregate totals and counts; apply the documented connection-local-date boundary, including earlier expenses on that calendar date.
- [x] Add confirmed `/disconnect`, revoke both users' comparison access, retain private records, and handle obsolete buttons/notifications.
- [x] Test two-user data isolation, independent category learning, onboarding recovery, concurrent pairing, repeat callbacks, blocked notification recipients, and disconnect authorization.
- [x] Add persisted request limits (60 updates/minute/user) and invite limits (five/hour/user).
- [ ] Measure shared hosting/database usage before expanding beyond a small group.

## 12. Phase 1 completion

- [ ] Complete an end-to-end Telegram session: create a category, save expenses, reuse category memory, edit/delete a record, and verify daily/monthly reports.
- [ ] Confirm data and category memory survive a redeploy and a sleeping/restarted application.
- [ ] Merge a small tested change into `main` and verify that the correct revision deploys automatically.
- [x] Update the README and HLD to match the implemented behavior and record any remaining limitations.

## Skeleton verification — 2026-09-14

- Passed locally: formatting check, `go vet ./...`, `go test -race ./...`, Go binary build, and `docker compose config --quiet`.
- Config tests cover invalid settings, remote TLS requirements, defaults, and credential-safe validation errors. HTTP tests verify liveness without a database query, readiness success/failure, and absence of an unfinished webhook route.
- Docker daemon was unavailable: image build and real PostgreSQL integration execution remain unverified locally. CI defines both checks but has not run on GitHub.
- Checked configuration/CI tasks indicate files are implemented, not that Render, Neon, GitHub, or Telegram accounts are configured. Business packages currently contain documentation placeholders.

## Onboarding milestone verification — 2026-09-14

- [x] Add initial PostgreSQL tables: users, per-user categories, onboarding interactions, processed inbound IDs, and durable reply outbox. Expense and pairing migrations remain unchecked above.
- [x] Implement onboarding `/start`, `/help`, `/cancel`, invalid-name recovery, and returning-user handling. Full expense command help remains pending.
- [x] Verify two-user registration/isolation, repeated migration application, concurrent redelivery, rollback, cancelled onboarding, and reply lease recovery against temporary PostgreSQL.
- [x] Verify webhook authentication, body limit, private-chat filtering, and HTTP 503 on persistence failure.
- [x] Verify webhook → database → reply flow with two users and a fake Telegram sender; no real messages sent by tests.
- [x] Provide explicit `make webhook-register` setup; registration is not performed on startup.
- [ ] Configure a real development token and public HTTPS endpoint, then verify registration using two real Telegram accounts.

The initial skeleton verification above is historical. PostgreSQL is now available for integration tests. Remaining category-mapping/expense/pairing tasks are not implied complete by successful onboarding tests.

- [x] Build the Docker deployment image and smoke-test executable startup, automatic migrations, health/readiness, and clean SIGTERM shutdown against PostgreSQL. Temporary test container removed afterward.

- [x] Automatically load optional local `.env` for the app, migration runner, and webhook setup; preserve existing environment values and test precedence, missing files, and sanitized parse errors.

## Repository completion verification — 2026-09-14

- Implemented text entry through category selection, confirmation, edits/deletion and reports; comparison invitations and disconnect; persisted rate limits; authenticated callbacks and durable inline keyboards.
- Focused tests use a temporary PostgreSQL database and isolated schemas, plus a local fake Telegram API. They cover restart-persisted state, stale writes, duplicate delivery/save, owner isolation, aggregate privacy, concurrent pairing, rollback, and report length with Unicode.
- Full live Telegram acceptance, tunnel/webhook registration, hosting configuration, deployed sleep/wake, provider quota checks and production backup storage remain unchecked. Phase 2 OCR awaits representative GoFood screenshots.
- Earlier verification sections are historical, not current feature limitations. No code was committed, pushed, or deployed by this implementation task.

- [x] Add backup/restore scripts and rehearse with synthetic data in an isolated database; refuse overwrite/nonempty restore targets. Actual off-host production backup scheduling remains pending.
- [x] Validate CI/deployment workflow with actionlint and 11 Python unit/mock tests (root verification).

## Final repository verification — 2026-09-15

- [x] Formatting, vet, race-enabled unit tests, full PostgreSQL integration suite, and Go build passed on the integrated Phase 1 code.
- [x] Eleven Python operation-script tests and GitHub Actions workflow lint passed; no live Render API calls were made.
- [x] Backup/restore rehearsal restored synthetic data into an empty database and refused overwrites/nonempty targets.
- [x] Final Docker image built; container smoke test applied all four migrations, passed health/readiness checks, and exited cleanly on SIGTERM.
- [x] Removed the temporary application and PostgreSQL test containers after verification.

The implementation is ready for a live development-bot session. External account configuration, public HTTPS tunnel, production deployment, hosted backup scheduling, and Phase 2 OCR fixtures remain outside this completed repository work. Nothing was committed, pushed, or deployed by the agents.

- [x] Make `make test` run all unit and integration tests, manage temporary PostgreSQL automatically, and reuse an explicit test database in CI; retain `make test-unit` for quick checks.


### Weekly category records

- [x] Alert both connected partners when a newly saved expense strictly exceeds the current week's largest single expense in the same normalized category across both partners.
- [x] Use Monday–Sunday in the user's configured timezone (default Asia/Jakarta), limited to expense dates on or after the connection date and no later than today.
- [x] Establish the first expense quietly; equal/lower amounts do not trigger alerts. Previous-week and future-dated entries do not trigger live alerts.
- [x] Append the owner's emoji record message to the Save confirmation; enqueue the partner's name/category/amount alert in the same transaction. Never include the purchase description in the partner alert.
- [x] Match user-owned categories by normalized name; differently named categories remain separate.
- [x] Store weekly category baselines in weekly_category_records. Edits/deletions rebuild cached baselines from nondeleted expenses without alerts. No weekly reset job is needed.
- [x] Preserve intake serialization, duplicate-update protection, and pair-scoped outbox suppression after disconnection.
- [x] Explain category/amount sharing in invitation and connection consent messages. Existing active connections also receive alerts after this update.

Monthly comparison totals remain unchanged; there are no monthly record alerts.

- [x] Add migration 005 for keyed weekly record caching with lazy initialization and daily refresh.
- [x] Keep cached winners synchronized on edits/deletions; test tie handling and runner-up promotion.

- [x] Add nullable smoking preference, Yes/No onboarding question, and unanswered-user /start prompt.
- [x] Add Coffee & drinks for everyone and conditionally visible Smoking & vaping; backfill active users.
- [x] Enforce category visibility for picker, mappings, callbacks, and Save; preserve reports and invitation flow.
- [x] Test preferences, stale callbacks, visibility, and comparison invitation onboarding.
