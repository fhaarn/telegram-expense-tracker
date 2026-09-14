# Phase 1 Tasks — Telegram Expense Tracker

Goal: deploy a multi-user Telegram bot that records text expenses, remembers categories, supports corrections, and provides daily/monthly reports.

All checkboxes represent implementation work; creating this checklist does not mark any feature complete.

## Technical baseline

Use Go, PostgreSQL (local Docker for development, Neon for deployment), Render web hosting, and GitHub Actions. Use Telegram webhooks in deployment; local long polling may use a separate development bot. The [HLD](HLD.md) reflects this deployment baseline. The current code is an infrastructure skeleton; expense features and external deployment setup remain pending.

Defaults: IDR and Asia/Jakarta. Phase 2 OCR, GoFood screenshot parsing, CSV export, budgets, and reminders are outside this checklist.

## 1. Project setup

- [x] Reconcile the HLD with PostgreSQL, Neon, Render webhooks, and phase 1 deployment; remove obsolete SQLite-specific operations.
- [x] Initialize the Go module and application entry point with Telegram, expense, parsing, storage, and reporting packages.
- [x] Add the Telegram library and PostgreSQL driver; keep business rules independent of transport and database code.
- [x] Add `.gitignore`, `.env.example`, and validated configuration for bot token, allowed user ID, database URL, timezone, currency, HTTP port, and webhook secret.
- [x] Add local PostgreSQL Compose configuration and document database startup, application startup, and test commands.
- [ ] Implement and document the working migration runner (`make migrate-up` currently reports that it is pending).
- [ ] Verify local PostgreSQL startup and the Docker image once the Docker daemon is running.
- [x] Add a Dockerfile for deployment and an HTTP health endpoint; listen on Render's configured port.
- [ ] Create a development bot through BotFather and configure its numeric owner ID. Use a separate bot token for production.

## 2. Database and storage

- [ ] Add versioned PostgreSQL migrations for categories, category mappings, drafts, expenses, inbound updates, and active user interactions.
- [ ] Add owner-consistent references and uniqueness constraints for update IDs, originating draft IDs, normalized category names, and description mappings.
- [ ] Seed Food & drinks, Transport, Groceries, Shopping, Bills, Entertainment, Health, and Other without duplicating them on restart.
- [ ] Store amounts with checked integer arithmetic and one documented IDR scale; never use floating-point money calculations.
- [ ] Store expense dates as local calendar dates and operational timestamps in UTC.
- [ ] Implement transactional confirmation, revision/version checks, deletion, and remembered-category updates.
- [x] Configure a small connection pool, query timeouts, and TLS for the hosted database.

## 3. Telegram intake and reliability

- [ ] Implement `/start` and `/help` with text examples and available commands.
- [ ] Require private chats and completed registration for expense operations; authorize each message/callback by its Telegram sender and record owner.
- [ ] Implement the deployment webhook endpoint with Telegram secret-header verification and request-size limits.
- [ ] Persist authorized updates before acknowledging webhook delivery; deduplicate repeated deliveries.
- [ ] Process durable pending updates with bounded retries and recover unfinished work after restart.
- [ ] Send actionable errors for invalid input and temporary failures; keep secrets and expense content out of logs.
- [ ] Handle shutdown cleanly and preserve pending work. Avoid aggressive idle database polling that prevents Neon from sleeping.
- [ ] Explain that screenshot support is coming in phase 2 when an image is submitted.

## 4. Text reader

- [ ] Trim outer whitespace and treat only the final whitespace-delimited token as the amount candidate.
- [ ] Accept digits with optional `k`/`K`; preserve the entire preceding description, including numbers.
- [ ] Reject missing descriptions, invalid suffixes, zero/negative amounts, unsupported punctuation, and integer overflow.
- [ ] Ask for correction if the final token is invalid; never search earlier numbers for a fallback amount.
- [ ] Preserve the display description and separately normalize its category key by lowercasing and collapsing whitespace.
- [ ] Default the expense date to today in Asia/Jakarta and show the date in the preview.

## 5. Category selection and learning

- [ ] Show a paginated category picker for an unknown description, including `+ New category`.
- [ ] Prefill the remembered category for a known normalized description, regardless of amount.
- [ ] Stage a new description/category mapping and persist it only when the expense is saved.
- [ ] Let category corrections apply to `This expense only` or `Remember for <description>`.
- [ ] Re-run lookup after a description edit and discard the previous description's staged learning action.
- [ ] Keep historical expenses and already-visible pending drafts unchanged when a mapping changes.
- [ ] Detect stale mapping changes so one pending draft cannot silently overwrite a newer remembered choice.

## 6. Custom categories

- [ ] Add `+ New category` to every category picker, including expense-edit flows.
- [ ] Persist a category-name interaction so replies are treated as names instead of new expenses, including after restart.
- [ ] Validate a nonempty single-line name of at most 40 Unicode characters; normalize whitespace and reject control characters.
- [ ] Reuse an existing category for case/whitespace-equivalent names rather than creating duplicates.
- [ ] Create the category and select it on the originating draft, then return to the expense preview.
- [ ] Make `/cancel` during name entry return to the picker without creating a category.
- [ ] Retain an already-created category if its expense is cancelled, but save no expense or remembered mapping.
- [ ] Include custom categories in reports; omit categories without confirmed expenses for the selected period.

## 7. Draft review and expense management

- [ ] Show description, formatted amount, category, and date with Save, Edit, and Cancel buttons.
- [ ] Require a valid amount, description, date, and category before Save.
- [ ] Support editing description, amount, category, and date with explicit prompts.
- [ ] Keep only one active free-text interaction per user and identify its originating draft.
- [ ] Save an expense exactly once and return the existing result for repeated Save callbacks.
- [ ] Persist pending/confirmed/cancelled/expired draft states and enforce the initial 24-hour expiry.
- [ ] Validate callback ownership, draft state, and version before mutation.
- [ ] Implement `/recent` with paginated records and record-specific Edit/Delete buttons.
- [ ] Confirm saved-expense revisions before applying them; prevent stale revisions overwriting newer changes.
- [ ] Require confirmation before deleting a saved expense and exclude deleted records from reports.

## 8. Reports

- [ ] Implement `/today`: confirmed expense list, total, and category breakdown for the current local date.
- [ ] Implement `/month`: total, expense count, and category breakdown for the current local calendar month.
- [ ] Format IDR amounts consistently and order report entries predictably.
- [ ] Handle empty periods with a friendly message and zero total.
- [ ] Paginate or split long responses to fit Telegram limits.
- [ ] Reflect confirmed edits/deletions and include custom categories; exclude drafts, cancelled entries, and deleted expenses.

## 9. Focused verification

- [ ] Test `Tahu Telor 20k` → description `Tahu Telor`, amount Rp20,000.
- [ ] Test `Nice 8 Ball Cafe 100k` → description `Nice 8 Ball Cafe`, amount Rp100,000.
- [ ] Test uppercase suffixes, extra whitespace, invalid final tokens, missing fields, and overflow.
- [ ] Test saving `bensin 100k` as Transport, then prefilling Transport for `bensin 25k`, including after restart.
- [ ] Test custom Fitness creation for `gym 150k`, reuse for `gym 200k`, and inclusion in reports.
- [ ] Test duplicate category names, cancelled category entry, cancelled expenses, and one-off category overrides.
- [ ] Test duplicate webhook delivery and Save callbacks, stale revisions/mappings, and transaction rollback against PostgreSQL.
- [ ] Test per-user ownership isolation and webhook-secret rejection.
- [ ] Test report totals across local midnight/month boundaries, custom categories, edits, deletions, and empty periods.
- [ ] Test restart recovery of drafts, category-name interactions, and pending inbound work.

## 10. GitHub Actions and deployment

- [ ] Initialize/publish the GitHub repository and document the feature-branch → pull-request → main workflow.
- [x] Add GitHub Actions checks for pull requests and pushes to `main`: formatting check, `go vet`, tests, and build validation.
- [x] Use standard Linux runners and a temporary PostgreSQL service for integration tests; never use the production database for CI tests.
- [ ] Require successful checks before merging where repository settings support it, and prevent routine direct pushes to `main`.
- [ ] Create the Neon database and Render web service connected to this repository.
- [ ] Store production secrets in Render; store the Render deploy-hook URL in GitHub Actions secrets.
- [ ] Trigger deployment only after successful checks on `main`; disable the separate Render auto-deploy trigger to avoid duplicate deploys.
- [ ] Ensure the deployed revision is the tested revision and serialize deployment requests to avoid stale releases winning a race.
- [ ] Run migrations safely with locking before serving the new version; fail deployment on migration errors and keep changes compatible with the prior version during rollout.
- [ ] Verify deployment completion and application health; a successful deploy-hook request alone is not a successful release.
- [ ] Register Telegram's production webhook and verify delivery, retries, and duplicate protection.
- [ ] Check free-tier allowances at setup and monitor GitHub minutes, Render builds/usage, and Neon storage/compute.
- [ ] Exercise Render sleep/wake behavior and document the delayed first reply.
- [ ] Set up a consistent database backup outside the application filesystem and verify a restore.
- [ ] Document deployment, secrets setup, troubleshooting, backup/restore, and rollback; note that code rollback does not undo database migrations.

## 11. Multi-user onboarding and one-to-one comparison (new scope)

These tasks extend the original single-owner skeleton; previous checked infrastructure tasks do not mean multi-user behavior exists.

- [ ] Replace required `TELEGRAM_ALLOWED_USER_ID` configuration with registered-user identity derived from Telegram sender IDs; update configuration tests, environment example, and README.
- [ ] Add users with unique Telegram numeric ID, private chat ID, optional username, display name, status, timezone, and currency.
- [ ] Implement idempotent `/start` → “Who are you?” onboarding and validated name entry; seed categories per user.
- [ ] Preserve invitation context through onboarding/restart and validate it again before acceptance.
- [ ] Scope all existing/future queries, category mappings, interactions, and callback mutations by authenticated internal user ID.
- [ ] Add invite, pair, active-membership, and notification-outbox migrations with ownership and uniqueness constraints.
- [ ] Generate 16 secure random bytes as 32 hex characters; store a digest and emit `https://t.me/<bot_username>?start=compare_<token>`.
- [ ] Add `/compare` invitation creation/rotation, 24-hour expiry, cancellation, and Connect/Cancel acceptance explaining the shared totals/counts.
- [ ] Atomically pair two distinct unpaired registered users, consume the invite, revoke obsolete invitations, and enqueue both requested notifications.
- [ ] Enforce one partner per user under concurrent acceptance; handle self-invites, invalid/expired/reused tokens, and already-paired users.
- [ ] Deliver “You're Battling {A} Now!” to the joiner and “{B} is your partner in crime now!” to the inviter with durable retries.
- [ ] Add paired `/compare` daily/monthly aggregate totals and counts; confirm proposed connection-date sharing boundary before implementation.
- [ ] Add confirmed `/disconnect`, revoke both users' comparison access, retain private records, and handle obsolete buttons/notifications.
- [ ] Test two-user data isolation, independent category learning, onboarding recovery, concurrent pairing, repeat callbacks, blocked notification recipients, and disconnect authorization.
- [ ] Add bounded per-user request/invite limits and measure shared hosting/database usage before expanding beyond a small group.

## 12. Phase 1 completion

- [ ] Complete an end-to-end Telegram session: create a category, save expenses, reuse category memory, edit/delete a record, and verify daily/monthly reports.
- [ ] Confirm data and category memory survive a redeploy and a sleeping/restarted application.
- [ ] Merge a small tested change into `main` and verify that the correct revision deploys automatically.
- [ ] Update the README and HLD to match the implemented behavior and record any remaining limitations.

## Skeleton verification — 2026-09-14

- Passed locally: formatting check, `go vet ./...`, `go test -race ./...`, Go binary build, and `docker compose config --quiet`.
- Config tests cover invalid settings, remote TLS requirements, defaults, and credential-safe validation errors. HTTP tests verify liveness without a database query, readiness success/failure, and absence of an unfinished webhook route.
- Docker daemon was unavailable: image build and real PostgreSQL integration execution remain unverified locally. CI defines both checks but has not run on GitHub.
- Checked configuration/CI tasks indicate files are implemented, not that Render, Neon, GitHub, or Telegram accounts are configured. Business packages currently contain documentation placeholders.
