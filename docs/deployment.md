# Deployment setup (not enabled yet)

This repository can test and deploy a specific main commit through GitHub Actions.
No deployment is enabled by default and none is performed by writing these files.

## One-time account setup

1. Create Neon PostgreSQL and a Render Docker web service linked to this repository.
2. Set Render's branch to `main`, health path to `/healthz`, and Auto-Deploy to **Off**.
3. Configure database URL (`sslmode=verify-full`), production bot token, webhook secret,
   timezone/currency, and any bot username configuration documented in the README.
4. Keep the development bot/database separate. Never commit `.env` or tokens.
5. Add GitHub secret `RENDER_API_KEY`; repository variables `RENDER_SERVICE_ID`
   and `RENDER_HEALTH_URL` (for example `https://your-service.onrender.com/healthz`).
6. Only when ready to deploy automatically, set repository variable
   `RENDER_DEPLOY_ENABLED=true`. Leave it unset while development is local only.
7. Configure required checks/branch protection in GitHub where the repo plan permits.
8. Register the production Telegram webhook once the service is healthy; this is a
   separate explicit action, not part of CI. Live account setup is still pending.

## What a merge does

CI checks formatting, vet, tests, an isolated PostgreSQL integration suite, operation
script tests, and the Docker image. The deploy job runs only on a successful push to
main and only when enabled. Main workflow runs are serialized without interrupting
an in-progress deploy. Before deployment it checks that the tested SHA is still main's
current head; an obsolete queued run skips deploying.

The script sends `commitId=GITHUB_SHA` to Render's API, polls that specific deployment,
requires `live` with the matching commit, and checks `/healthz`. This uses the API rather
than a bare deploy hook so the tested revision and completed release can be verified.
A network error while triggering a deploy is not automatically retried, because the
request may already have succeeded. Inspect Render before retrying. The script has
mock-based tests; no live Render API call has been made during repo development.

Disable Render's independent auto-deploy setting to prevent races with this workflow.
GitHub serialization cannot coordinate manual deploys or other deployment systems.
Migration startup must finish before HTTP becomes available. Future schema changes
must work with the preceding version during rollover. A code rollback cannot undo a
schema migration; prefer compatible forward fixes and verified backups.

## Limits and operational checks

Render Free may sleep, lose local files, or exhaust quotas. Durable data belongs in
Neon. A sleeping service may delay the first bot response. Verify current provider
limits and a live cold-start session before relying on the app. OCR capacity must be
measured separately in phase 2. Back up PostgreSQL and rehearse restoration using
[operations.md](operations.md).

References: [Render deploy API](https://api-docs.render.com/reference/create-deploy),
[deployment status](https://api-docs.render.com/reference/retrieve-deploy),
[Render deployment settings](https://render.com/docs/deploys).
