#!/usr/bin/env bash
# Run this from a trusted machine with PostgreSQL client tools installed.
# Keep the resulting backup private; it contains account and conversation data.
set -euo pipefail
: "${DATABASE_URL:?Set DATABASE_URL before running this script}"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$BACKUP_DIR/connectai-hub-$STAMP.dump"
pg_dump "$DATABASE_URL" --format=custom --no-owner --no-acl --file="$OUT"
chmod 600 "$OUT"
echo "Backup created: $OUT"
