# Phase 6 — Implementation and interaction report

Implemented bulk expense entry and admin announcements. Uses the existing `ADMIN_API_KEY`; no new environment variables are required. Migration `009_bulk_and_broadcasts.sql` runs through the existing startup migration runner. Changes are local until committed, pushed and deployed.

## Bulk expense conversation

User sends one message:

```text
Tahu Telor 20k
Nice 8 Ball Cafe 100k
Tahu Telor 25k
```

If Tahu Telor is already remembered as Food & drinks, the bot skips both matching lines and asks only:

```text
🧾 Item 2/3
🏷️ What category is “Nice 8 Ball Cafe”?

[🍽️ Food & drinks]
[☕ Coffee & drinks]
[other eligible categories…]
[Next ➡️]
[✨ New category] [❌ Cancel]
```

Select Coffee & drinks. Once all items have a category:

```text
📅 What date are these expenses for?
Choose below or send YYYY-MM-DD. This date applies to every item.

[Today] [Yesterday]
[❌ Cancel]
```

User sends `2026-10-07`:

```text
🧾 Review 3 expenses · page 1/1
📅 2026-10-07

1. Tahu Telor · Rp20,000
   Food & drinks
2. Nice 8 Ball Cafe · Rp100,000
   Coffee & drinks
3. Tahu Telor · Rp25,000
   Food & drinks

💰 Batch total: Rp145,000

[✅ Save all] [✏️ Edit] [❌ Cancel]
```

After Save all:

```text
✅ Saved 3 expenses for 2026-10-07!
💰 Total: Rp145,000
```

Next time, both descriptions use their remembered categories. If both were initially unknown, choosing Tahu Telor's category once categorizes both matching lines.

## Rules and recovery

- Accept 2–20 non-empty lines. Parse the last token as the amount, preserving numbers inside descriptions. Blank lines are ignored. Malformed input identifies its original line number; correct and resend the whole message before any draft is created.
- One date applies to the entire batch. Today/Yesterday follow the user's stored timezone; custom dates use the existing year-1900-or-later validation.
- Category choices follow existing ordering, five-per-page pagination, custom creation and smoker-only visibility. New mappings are learned only when saved. Existing remembered mappings are not overwritten by a batch category edit.
- Review has five items per page, full descriptions and a whole-batch total. Edit lets the user choose an item, then Description, Amount or Category; it also offers Change date. Category changes apply to all matching descriptions in the batch.
- `/bulk` resumes the persisted stage. `/cancel` discards the entire batch. Pending batches expire after 24 hours. A new single entry cannot replace an unfinished batch; a pending single draft must be finished/cancelled before starting a batch.
- Save all commits every expense and mapping together. A failed transaction, repeated Telegram update, stale button, or repeated confirmation cannot create a partial/duplicate batch. Cross-user callbacks are rejected.
- Confirmed expenses appear in ordinary reports, `/recent`, exports and comparisons. Weekly record messages combine the winning items per category into one partner reply. First records remain quiet; historical/future backfills do not announce current-week records.
- Blocking a user cancels pending drafts/batches and skips pending announcements; whitelisting does not revive them.

## Admin announcements

Full request examples and status fields: [admin API guide](admin-api.md#announcements-phase-6).

1. `POST /admin/broadcasts` with `message` and `dry_run: true` previews the eligible count.
2. Send the same request with `dry_run: false` and a unique `Idempotency-Key` to queue delivery.
3. `GET /admin/broadcasts/{id}` shows pending, sent, failed and skipped counts.

All use `X-API-Key` with the existing `ADMIN_API_KEY`. Recipients see the plain text message you supply, including your emoji. No extra prefix is injected, and no announcement runs automatically on deploy.

Announcements use a persistent audience snapshot, at most one send per second, priority for ordinary bot replies, Telegram retry delays, five-attempt retry limits and restart recovery. Reusing an idempotency key does not create a new job. An uncertain network result can still duplicate an external send on retry. Render free-tier sleep may delay delivery.

## Database additions

| Table | Stored components |
| --- | --- |
| `expense_batches` | ID, owner, pending/confirmed/cancelled/expired status, conversation stage, selected item, version, shared date, creation/expiry timestamps |
| `expense_batch_items` | Batch ID, owner, line position and existing draft ID; unique batch/position and draft ID |
| `admin_broadcasts` | ID, unique idempotency key, plain text message, creation timestamp |
| `admin_broadcast_recipients` | ID, broadcast, user, delivery state, attempt count, available time, lease, generic failure code; unique broadcast/user |
| `broadcast_delivery_state` | Singleton row with global next announcement time/cooldown |

Batch items reuse `expense_drafts`; confirmed records continue to use `expenses`. Broadcasts have their own queue because announcements do not originate from incoming Telegram update IDs. Job progress is calculated from recipient state.

## Verification

Passed on 2026-10-09: `make test` (race-enabled unit and isolated PostgreSQL integration tests plus 11 Python operation tests), `make check-fmt`, `make vet`, `make build`, and `git diff --check`.

Automated coverage includes multiline validation, category memory, repeated descriptions, custom categories, owner isolation, category visibility, date validation, edit/cancel/expiry/restart, pagination, duplicate saves, transaction rollback/retry, weekly cache/notification behavior, existing-key HTTP authentication, preview with no sends, API validation, idempotency conflicts, audience snapshots, bot-reply priority, blocked-user suppression, expired delivery leases, Telegram retry classification, terminal failures and API-to-worker delivery using a fake sender.

Production interaction and real announcement delivery remain pending deployment; no real users were messaged during development.
