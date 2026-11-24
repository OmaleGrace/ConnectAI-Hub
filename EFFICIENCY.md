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
