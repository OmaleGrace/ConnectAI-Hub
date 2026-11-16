# ConnectAI Hub — Batch 3

Batch 3 connects the seven authenticated workspaces to Google Gemini.

## Configuration

Keep your existing `.env` and Neon `DATABASE_URL`. Make sure it contains:

```env
GEMINI_API_KEY=your_gemini_api_key
GEMINI_MODEL=gemini-3.5-flash
```

Never commit `.env` or share the API key. The key is used only by the Go server; it is not sent to the browser.

## Run

From the project folder:

```bash
go mod tidy
go run .
```

The app runs at `http://localhost:8080`. Sign in, open any of the seven workspaces, and send a message.

## What this batch adds

- Shared Gemini REST client using Go's standard library (no new Go dependency).
- Separate role instructions for all seven workspaces.
- Authenticated chat endpoints protected by the existing CSRF cookie.
- Per-user, per-module conversation storage in Neon PostgreSQL.
- Recent conversation context sent to Gemini.
- Responsive chat UI, loading state, and error handling.
- Automatic `ai_messages` table/index creation during the existing database migration.

## Notes

- Existing account, session, and user tables are preserved.
- The server stores the user's prompt and Gemini response in `ai_messages`.
- Each request sends up to the latest 12 stored messages as conversational context.
- The model name is configurable with `GEMINI_MODEL`. If Gemini reports that the model is unavailable for your API key or API version, use a model ID enabled for your Google AI Studio project.
- This is a development foundation. Before public launch, add request rate limits, usage quotas, monitoring, privacy controls, and a review of retention policies.
