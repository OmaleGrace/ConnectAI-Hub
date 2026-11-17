ALTER TABLE users ADD COLUMN IF NOT EXISTS plan TEXT NOT NULL DEFAULT 'free';
ALTER TABLE users ADD COLUMN IF NOT EXISTS daily_limit BIGINT NOT NULL DEFAULT 100;
UPDATE users SET plan='free' WHERE plan IS NULL OR plan='';
UPDATE users SET daily_limit=100 WHERE daily_limit IS NULL OR daily_limit < 1;
