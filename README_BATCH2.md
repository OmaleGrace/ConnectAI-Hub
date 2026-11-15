# ConnectAI Hub — Batch 2: PostgreSQL and Accounts

This batch adds PostgreSQL-backed registration, login, logout and sessions.

## Requirements
- Go 1.22+
- PostgreSQL running locally
- A database named `connectai_hub`

## Setup
1. Create a PostgreSQL database named `connectai_hub`.
2. Copy `.env.example` to `.env`.
3. Put your local PostgreSQL username/password in `DATABASE_URL`.
4. Run `go mod tidy`.
5. Run `go run .`.

The app runs migrations automatically at startup.

## Routes
- `/register` — create an account
- `/login` — sign in
- `POST /logout` — sign out
- `/dashboard` and `/modules/{slug}` — require sign-in
- `/health` — process health check

## Security notes
Passwords are stored as PBKDF2-HMAC-SHA256 hashes with a per-user random salt. Session tokens are random, stored only as SHA-256 hashes in the database, and sent in HttpOnly cookies. Forms use a CSRF token. Set `APP_ENV=production` behind HTTPS to enable Secure cookies.

Before public launch, add rate limiting, email verification, password reset, session cleanup, HTTPS enforcement, security headers and a managed secrets strategy. The built-in schema is intentionally small and is not a substitute for a production security review.
