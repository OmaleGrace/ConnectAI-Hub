# ConnectAI Hub — Batch 1

First working Go web application foundation.

## Run
```bash
go run .
```
Open http://localhost:8080

## Routes
- `/` — homepage
- `/dashboard` — workspace dashboard
- `/modules` — module directory
- `/modules/{slug}` — module preview
- `/health` — health check

Login and registration are placeholders. Authentication, PostgreSQL, AI provider calls, file uploads and billing are not implemented yet. Do not collect real passwords through the placeholder pages.

Set `APP_ADDR` to change the listen address. Default: `:8080`.
