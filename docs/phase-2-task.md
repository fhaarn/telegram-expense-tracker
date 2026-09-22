# Phase 2 Tasks — Admin API and User Access Control

Goal: let the owner securely list users, blacklist users, and restore access through an API-key-protected HTTP API.

Repository implementation and automated checks are complete. External production rollout items remain unchecked. Follow the [HLD](HLD.md).

## Technical baseline and scope

Extend the existing Go service, PostgreSQL migrations, transactional intake, and notification outbox. Keep onboarding status separate from access status. Registration stays open by default; whitelist means restoring access for an existing user.

No admin dashboard is required. CSV export belongs to phase 3, GoFood OCR to phase 4, and remaining convenience features to phase 5. Google Sheets remains an undecided idea.

## 1. Configuration and authentication

- [x] Add a separate `ADMIN_API_KEY` configuration value and empty placeholder in `.env.example`; never reuse Telegram credentials.
- [x] Disable admin operations with 503 when no server key is configured, while keeping the bot and health endpoints operational.
- [x] Require `X-API-Key` on every `/admin/*` endpoint; return 401 for missing or invalid credentials.
- [x] Compare credentials in constant time and keep keys out of logs, errors, URLs, and response bodies.
- [x] Require HTTPS for hosted access and document local development access.
- [x] Add bounded request bodies, request timeouts, and admin rate limits; document selected limits and return 429 when exceeded.

## 2. Database and access state

- [x] Add the next versioned migration without modifying already-applied migrations.
- [x] Add `users.access_status` with allowed values `allowed` and `blocked`, defaulting to `allowed`; backfill existing users.
- [x] Add `users.access_updated_at` and define its initial value during migration/registration.
- [x] Keep `users.status` (`pending`/`active`) independent of access status.
- [x] Create `admin_access_events` with ID, user_id FK, previous_status, new_status, optional reason, and created_at.
- [x] Persist actual transitions and audit events atomically; repeated requests must not create duplicate transition events.
- [x] Add indexes appropriate for stable user pagination and access filtering.

## 3. List users endpoint

- [x] Implement `GET /admin/users?limit=50&cursor=...&access=all`.
- [x] Use stable cursor pagination with a default limit of 50 and supported limits of 1–100.
- [x] Validate cursors, limits, and access filters (`all`, `allowed`, `blocked`); reject invalid input with 400.
- [x] Include pending and active users; return `users` and `next_cursor`.
- [x] Return only internal ID, Telegram user ID, Telegram username, display name, onboarding status, access status, created_at, and access_updated_at.
- [x] Exclude expense data, smoking preference, secrets, and notification contents.

## 4. Blacklist and whitelist endpoints

- [x] Implement `PUT /admin/users/{user_id}/blacklist` using internal user IDs.
- [x] Implement `PUT /admin/users/{user_id}/whitelist` to restore access.
- [x] Accept an optional JSON reason of at most 500 characters; validate IDs and request bodies.
- [x] Return 200 with user ID and access status on success, 400 for invalid input, and 404 for unknown users.
- [x] Make repeated blacklist/whitelist requests idempotent.
- [x] Serialize access changes with existing expense and comparison transactions using the established lock order.
- [x] Preserve profiles, categories, drafts, mappings, expenses, and reports when access changes.

## 5. Enforce blocked access

- [x] Check access centrally before commands or callbacks perform application transitions or expose reports.
- [x] Prevent `/start`, old buttons, smoking preference callbacks, expense edits/deletes, and comparison commands from bypassing a block.
- [x] Acknowledge valid blocked-user Telegram updates normally and preserve update deduplication to avoid retries.
- [x] Use silent acknowledgment for blocked users; no extra blocked-access notification is queued.
- [x] On blacklist, atomically revoke the user's outstanding invites, clear their pending invite, and end their active comparison connection.
- [x] Prevent blocked users from creating or redeeming invitations or becoming a partner through another user's action.
- [x] Suppress queued pair notifications after connection termination and queued bot replies addressed to blocked users.
- [x] Recheck delivery eligibility before claiming messages; document that already in-flight deliveries may finish.
- [x] Restore normal bot use after whitelisting without resurrecting ended connections, revoked invites, or expired drafts.

## 6. Tests and verification

- [x] Test authenticated access, missing/invalid keys, and disabled configuration for all admin routes.
- [x] Test pagination, filters, empty results, malformed cursors, request validation, size limits, and rate limits.
- [x] Test migration/backfill against users with existing expense and comparison data.
- [x] Test allowed → blocked → allowed transitions, repeated requests, and audit-event consistency.
- [x] Verify listing responses and logs exclude sensitive fields and credentials.
- [x] Test blocked users across onboarding, expense flows, reports, callbacks, and comparison invitations.
- [x] Test concurrent blacklist/save and blacklist/invite acceptance with atomic rollback and no post-block mutations.
- [x] Test queued reply suppression, partner notification suppression, and worker restart/retry behavior.
- [x] Verify expense history remains unchanged and whitelisted users can use their saved data again.
- [x] Run `make test` (unit, integration, and operation tests), `make check-fmt vet build`, and the Docker build check.

## 7. Documentation and deployment

- [x] Document endpoint contracts, example requests, response codes, and the meaning of blacklist/whitelist.
- [x] Document secure key generation, Render configuration, and rotation without putting actual keys in examples or source control.
- [x] Update README, HLD, and operations documentation to match implemented behavior.
- [ ] Configure `ADMIN_API_KEY` in Render and deploy through the existing GitHub Actions workflow.
- [ ] Confirm migration success and unchanged public health checks after deployment.
- [ ] With a dedicated test user, verify list → blacklist → denied bot action → whitelist → restored access in production.
- [x] Mark tasks complete only after implementation and the relevant verification succeed.
