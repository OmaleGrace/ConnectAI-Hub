-- Remembers which Plus expiry date the "ends soon" reminder was already sent for,
-- so renewing (a new expiry date) re-arms the reminder.
ALTER TABLE users ADD COLUMN IF NOT EXISTS plus_reminder_for TIMESTAMPTZ;
-- Speeds up the hourly reconciliation of unfinished payments.
CREATE INDEX IF NOT EXISTS plan_payments_pending_idx ON plan_payments(created_at) WHERE status='pending';
