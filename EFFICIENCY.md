# AI tool efficiency changes

All seven workspaces (and file analysis) share one request path in `internal/web/aitools.go`.

- **Context budget**: conversation history sent to the model is capped at ~12,000 characters (newest first); long old answers are shortened. Previously up to 12 full messages were resent every turn.
- **File analysis cap**: extracted text is limited to 120,000 characters (previously up to 5 MB) and the answer is told when a file was cut.
- **Parallel loading**: user settings and history load at the same time.
- **One-statement save**: question, answer and usage record are saved in a single atomic query that returns the answer id (replaces a transaction plus a follow-up SELECT).
- **Shared helpers**: `loadHistory`, `prepareChat`, `saveExchange` replace three copies of the same code in chat, regenerate and upload.
- **Prompt cache**: each workspace's system prompt is built once.

Tune the limits via the constants at the top of `aitools.go`. Run `go test ./...`.

## Assistant awareness (chat quality)
- The assistant now knows the signed-in user's name (loaded in the same query as workspace preferences, so no extra database call).
- The system prompt explains the app: conversations are saved per workspace, and which real buttons exist (Clear chat, Regenerate, Export, Search, Workspace preferences). It no longer gives generic "New Chat" advice or claims it has no memory.
- For permanent facts the assistant points users to Workspace preferences > Custom instructions.

## Copy and edit sent prompts
- Every message you send now has **Copy** and **Edit** (Edit is hidden on file-upload requests).
- **Edit** opens the prompt in a box. "Save & resend" (or Ctrl/Cmd+Enter) replaces the old reply with a new answer. If there are later messages, a confirmation is shown first because they are removed.
- Backend: `POST /api/modules/{slug}/edit` (`editMessage` in `server.go`); one atomic query updates the prompt, deletes later messages, saves the new answer and records usage. `ai_usage` accepts the new `edit` request type (see `database.go`).
- Copy has a fallback for sites served over plain http.

## Assistant memory (`internal/web/memory.go`)
- When you tell the assistant something lasting about yourself ("I run a TikTok page", "I'm learning Go"), a short note is saved and given to **every workspace**.
- Extraction runs in the background after the reply is sent, and only for messages that look like you talking about yourself, so it costs very little and never slows the chat. Passwords, emails, phone/ID/card numbers, links and similar details are never stored. Max 30 notes of 160 characters.
- **Settings > What the assistant remembers**: see every note, "Forget" one, "Forget everything", or switch memory off. Notes are deleted with "Delete AI history" and with the account, and are included in the data export.
- New table `user_memories` and column `users.memory_enabled` are created automatically at startup.
- Optional: set a cheaper model for it with the same per-workspace model setting, using the name `memory`.

## Site-wide speed
- CSS/JS URLs are fingerprinted automatically (`{{asset "css/app.css"}}`), cached for a year, and refreshed instantly after a deploy. No more manual `?v=` numbers.
- Removed the Google Fonts request (headings use a built-in serif font). Deleted four unused stylesheets (`auth.css`, `chat.css`, `redesign.css`, `studio.css`).
- Pages are sent with `Cache-Control: private, no-cache` and an exact Content-Length; SVG files are compressed; `/static/` no longer shows directory listings.
- Service worker no longer hard-codes asset versions; fingerprinted files load straight from its cache.
- Hourly cleanup of expired sessions, password-reset tokens, phone codes and stale rate-limit entries.
- Graceful shutdown: in-progress requests finish on Ctrl+C or a deploy.
- Payment-provider calls now time out after 20 seconds instead of waiting forever.
- Bug fix: the database setup no longer fails on restart once `edit` usage rows exist.
