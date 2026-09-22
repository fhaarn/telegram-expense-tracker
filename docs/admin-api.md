# Admin API

The owner can list users and block/restore bot access. Configure `ADMIN_API_KEY` separately from Telegram credentials. Generate a key with `openssl rand -hex 32`, store it in Render's Environment settings, and deploy. Never commit the value. Local development may set it in the ignored `.env` file and restart the app.

An unset/blank key disables admin routes (503) without disabling the bot. All admin requests require `X-API-Key`. Invalid/missing keys return 401. Hosted requests use HTTPS through Render; HTTP localhost is suitable for local development. No application redirect trusts forwarded headers or exposes credentials.

Authenticated requests share a 60-request/minute process-local limit (429 with Retry-After). It resets on process restart; this is designed for one owner and one service instance. Admin operations have a five-second deadline and bodies are limited to 4096 bytes. Never use verbose HTTP logging with credentials.

## Requests

Use your HTTP client's secret/environment storage for the key. The examples below are HTTP request templates, not literal credentials.

```http
GET /admin/users?limit=50&access=all
X-API-Key: <your admin key>
```

Returns `{"users":[...],"next_cursor":null}`. User fields: `id`, `telegram_user_id`, `telegram_username`, `display_name`, `status`, `access_status`, `created_at`, `access_updated_at`. Nullable profile fields remain null. IDs refer to internal user IDs for mutation endpoints. No expense data, smoking preference, or credentials are returned.

Use `limit` 1–100 (default 50), `access` all/allowed/blocked, and the returned opaque `cursor` for the next page. Keep the same filter while paging. Ordering is by internal ID; concurrent status changes may change membership between pages, so this is not a snapshot export.

```http
PUT /admin/users/123/blacklist
X-API-Key: <your admin key>
Content-Type: application/json

{"reason":"Manual access restriction"}
```

```http
PUT /admin/users/123/whitelist
X-API-Key: <your admin key>
Content-Type: application/json

{"reason":"Access restored"}
```

Reason is optional (empty body or `{}` is accepted), up to 500 characters. Success returns `{"id":123,"access_status":"blocked"}` or `allowed`. Repeating the same state succeeds without another audit event. Unknown users return 404, malformed IDs/query/body return 400, unavailable storage returns 503, unsupported methods return 405. Error bodies do not expose database details or keys.

## Access behavior

Users default to allowed. Blacklisting is separate from pending/active onboarding status. Blocked Telegram updates are acknowledged and deduplicated silently; no blocked-access reply is sent. All user command/callback processing is stopped before profile metadata changes. Existing expenses, drafts, mappings, categories, and preferences remain saved.

Blocking revokes pending invites created by the user, clears their pending invitation, ends their active pair, and suppresses queued replies to them and queued messages for the ended connection. Messages claimed before the block may already be in flight. Delivery claims and admin/intake transitions use the same transaction lock. A sending worker's later completion cannot resurrect a suppressed row.

Whitelisting restores use of the bot; it does not revive ended connections, revoked invites, or expired drafts. Users can connect again normally. Actual changes are recorded in `admin_access_events`; reasons should not contain credentials.

## Rollout and rotation

Migration 007 backfills access as allowed, adds access timestamps and the audit table. It runs at app startup. Set the Render key and push/merge through the configured deployment workflow. Check `/healthz`, then use a dedicated test user for the list/block/restore flow. Do not test on an unsuspecting real user.

Rotate by generating a new key, updating Render, deploying, and replacing the key in your HTTP client. The prior key stops working after the previous instance shuts down; during a rolling deployment each instance uses its own configured key. Disabling the key prevents admin access but does not unblock users. Avoid rolling back to pre-access-control application code because it would not enforce existing blocks.
