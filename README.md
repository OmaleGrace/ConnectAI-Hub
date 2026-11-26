# ConnectAI Hub

Seven focused AI assistants (Study, Career, Business, Documents, Website, Support, Data) plus a website builder, prompt library, notifications and a Paystack-powered **ConnectAI Plus** plan, in one Go web app.

- Backend: Go 1.22+, standard library `net/http`, PostgreSQL (`lib/pq`)
- Frontend: server-rendered templates in `templates/`, one stylesheet (`static/css/app.css`), one script (`static/js/app.js`), installable PWA
- AI: Gemini by default, or OpenRouter when `OPENROUTER_API_KEY` is set

## Run locally

```bash
cp .env.example .env        # fill in DATABASE_URL and an AI key
go run .                    # http://localhost:8080
```

Tables are created and updated automatically on startup (`internal/database/database.go`). The files in `migrations/` are the same changes as readable SQL history.

## Configuration

Everything is an environment variable; `.env.example` lists them all with comments. The important ones:

| Variable | Purpose |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string |
| `GEMINI_API_KEY` / `OPENROUTER_API_KEY` | AI provider |
| `APP_BASE_URL` | Public https URL; used in emails and the Paystack callback |
| `APP_ENV=production` | Secure cookies and HSTS |
| `SMTP_*` | Password reset, welcome, payment and reminder emails |
| `ADMIN_EMAIL` | Account that gets admin access |
| `PAYSTACK_SECRET_KEY`, `PAYSTACK_PLUS_AMOUNT_KOBO` | ConnectAI Plus checkout |

## Payments (ConnectAI Plus)

A payment can reach the app three ways. All of them go through the same two functions in `internal/web/billing.go`, which ask Paystack to confirm the payment and then grant 30 days of Plus exactly once:

1. **Browser callback** `GET /billing/callback`: the customer returns after paying.
2. **Webhook** `POST /billing/webhook`: Paystack calls the server directly, so Plus is granted even if the customer closes the tab. Requests are accepted only with a valid `x-paystack-signature` (HMAC-SHA512 of the body using your secret key).
3. **Hourly reconciliation**: payments still pending after 15 minutes are re-checked with Paystack; abandoned ones are marked failed.

**One-time Paystack setup** (Dashboard > Settings > API Keys & Webhooks):

- Callback URL: `https://YOUR-DOMAIN/billing/callback`
- Webhook URL: `https://YOUR-DOMAIN/billing/webhook`

Renewals add 30 days on top of any time left. Three days before a pass ends the member gets an in-app notification and (if they allow email notifications) an email, once per expiry date. When a pass ends, the account returns to the Free allowance and gets a notification. Plus granted by an admin with no expiry date is never downgraded automatically.

## Deploy

With Docker:

```bash
docker build -t connectai-hub .
docker run --env-file .env -p 8080:8080 connectai-hub
```

Put it behind HTTPS (Caddy, nginx or your host's proxy). `GET /health` returns `ok` when the database is reachable. The server finishes in-flight requests on shutdown (SIGTERM / Ctrl+C).

Backups: `scripts/backup-db.sh` writes a private `pg_dump` file.

## Tests

```bash
go test ./...                                   # unit tests
TEST_DATABASE_URL='postgres://...' go test ./... # also runs the database tests
```

Use an empty throwaway database for `TEST_DATABASE_URL`; the tests create and delete their own users.

## Project layout

```
main.go                    start-up and graceful shutdown
internal/web/server.go     routes and handlers
internal/web/billing.go    Paystack verification, webhook, reconciliation, Plus reminders
internal/web/aitools.go    shared AI request path for all workspaces
internal/web/memory.go     assistant memory
internal/web/housekeeping.go  asset fingerprinting, hourly cleanup job
internal/ai/               Gemini / OpenRouter client
internal/database/         schema setup
templates/ static/         pages, CSS, JS, icons, service worker
```

See `EFFICIENCY.md` for the performance and quality changes already made.
