# ConnectAI Hub — Batch 5: Document and Data File Analysis

## What this adds
- File analysis in Document AI and Data AI workspaces.
- Supports UTF-8 `.txt`, `.md`, `.csv`, `.json`, and `.log` files.
- Maximum file size: 5 MB; request size capped at 6 MB.
- User must be signed in; CSRF token is checked.
- Uploaded file content is processed in memory and is not written to the server filesystem.
- Analysis and a short file/request record are saved in the existing `ai_messages` table.
- No new Go dependencies and no database migration are required.

## Install
From the project directory:
```bash
unzip -o ~/Downloads/ConnectAI-Hub-Batch5.zip -d .
gofmt -w internal/web/server.go
go run .
```

If your browser has an old script cached, use Ctrl+Shift+R.

## Test
1. Sign in and open Document AI.
2. Upload a small `.txt`, `.md`, `.csv`, or `.json` file.
3. Enter a question such as "Summarise the main points".
4. Confirm the answer appears and remains in chat after refreshing.
5. Repeat in Data AI with a small CSV file.

## Current limitations
- PDF, DOCX, XLSX and image files are not supported in this batch.
- File content is sent to Gemini for analysis, so do not upload confidential, sensitive, or private information unless you have permission and are comfortable sharing it with the AI provider.
- The upload is not retained as a downloadable file on the server. The chat stores the filename/request and AI response, not the original file content. A later follow-up cannot inspect the original file again unless you upload it again.
