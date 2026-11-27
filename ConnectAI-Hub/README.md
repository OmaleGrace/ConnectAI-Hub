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

## Install on a phone (PWA)

ConnectAI Hub is an installable web app, so there is no app store step. It needs an **https://** address (or localhost).

- **Android (Chrome/Edge/Samsung Internet):** tap *Install* (home page, Settings, mobile menu or the banner) or ⋮ → Install app.
- **iPhone / iPad:** open in Safari → Share → Add to Home Screen. The Install button shows these steps automatically.
- **Inside Facebook / Instagram / WhatsApp browsers:** the Install button tells the person to open the page in Safari or Chrome first.

`static/js/install.js` handles all of this; `static/manifest.webmanifest` defines the icons and the long-press shortcuts (Study, Career, Website builder, Prompt library).
For a Play Store listing, wrap the same site with PWABuilder or Bubblewrap (Trusted Web Activity); no code changes are needed.

## Guided tour

`static/js/tour.js`: signed-in members get a spotlight tour of the dashboard once per account (replay from **Take a tour** or Settings). Visitors on the home page get a small dismissible invitation, never a blocking pop-up. Edit the `memberSteps` and `visitorSteps` lists to change the content.

## Links and screenshots in the AI assistants

Every assistant can read web pages the user pastes, and can take a screenshot of them.

- **Reading links:** paste up to 3 `https://` (or `www.`) links in a message. The server opens each page, extracts the readable text and gives it to the AI as clearly marked, untrusted reference material. The answer lists the pages it read; if a page cannot be opened the assistant says so instead of guessing.
- **Screenshots:** tick **📷 Screenshot links** under the message box, or just ask ("what does this site look like?", "screenshot it", "check it on mobile", "full page"). The picture appears in the answer, can be opened or downloaded, and is also shown to the AI so it can review the design. Up to 2 per message, 8 per 10 minutes, newest 60 kept per user, deleted after 30 days.
- **Not supported:** pages behind a login, PDFs (use the Documents assistant), and keyword web search (only links the user pastes are opened).

**Security.** Opening user-supplied addresses from a server is risky (SSRF), so `netguard.go` only allows public internet addresses on ports 80/443, checks the real IP at connect time (covers DNS tricks and redirects) and refuses localhost, private ranges and the cloud metadata address. Chrome is forced through a local filtering proxy (`egressproxy.go`) that enforces the same rules for every request a page makes, runs with a throw-away profile and none of your secrets, and is killed on timeout. Screenshots are re-encoded as JPEG.

**Setup.** The Docker image installs Chromium and sets `CHROME_BIN` and `CHROME_NO_SANDBOX=true` (Chrome cannot use its own sandbox as a non-root user inside Docker; the proxy and process limits are the protection, so for a public site consider running the app in a container with no access to your private network). Without Chrome, link reading works and screenshots are switched off. `WEB_ACCESS=off` disables both.

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

### If screenshots fail on your host

1. Set `WEB_DEBUG=on` and read the logs: you will see each request Chrome makes through the proxy, plus Chrome's own error output (`Chrome diagnostics:`).
2. **No diagnostics and "took too long"**: the container is probably too small. Chromium needs roughly 500 MB of RAM and a 512 MB instance will be killed. Use a bigger instance, or set `CHROME_EXTRA_ARGS=--no-zygote --single-process`.
3. **`No usable sandbox` / crashes on start**: set `CHROME_NO_SANDBOX=true` (the Docker image already does).
4. **Proxy lines show `403`**: the address was refused by the safety rules (private IP or non-web port). `502` means it was allowed but could not be reached.

## URL Search & Screenshot page

Signed-in users can open `/web-tools` to inspect a public website URL: title, description, headings, readable text and outgoing links, plus an optional desktop/mobile screenshot they can download. It uses the same safety rules as the assistants (see above). Set `WEB_DEBUG=on` temporarily to log every request Chrome makes through the filtering proxy while diagnosing screenshot problems.


## Website builder

A build now runs in the background: submitting the form opens a progress page and the result appears when it is ready (you can leave the page and come back). The model writes plain sections instead of JSON, and an answer cut off by the length limit is continued automatically (up to 5 times), so large sites finish. Every finished site downloads as a ZIP that is ready to deploy: `index.html`, `css/style.css`, `js/script.js`, favicon, 404 page, robots.txt and a README with Netlify / Cloudflare Pages / GitHub Pages / Vercel / cPanel steps. Paste a link in the brief and the builder reads it and screenshots it as a design reference. AI edits run the same way and keep the previous version.

Setting: `GEMINI_MAX_OUTPUT_TOKENS` (default 8192; raise only if your model supports longer answers). Builds time out after 10 minutes.
